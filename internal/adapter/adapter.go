// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type Config struct {
	NativeActionSemantics bool              `json:"native_action_semantics,omitempty"`
	BackendURL            string            `json:"backend_url"`
	WorkerBinary          string            `json:"worker_binary"`
	WorkerSHA256          string            `json:"worker_sha256"`
	UpstreamVersion       string            `json:"upstream_version"`
	WorkRoot              string            `json:"work_root"`
	ServerDataRoots       map[string]string `json:"server_data_roots,omitempty"`
	StateDBPath           string            `json:"state_db_path,omitempty"`
	CapabilityFile        string            `json:"capability_file,omitempty"`
	MaxWorkers            int               `json:"max_workers,omitempty"`
	TimeoutSeconds        int               `json:"timeout_seconds,omitempty"`
	WorkerMemoryMiB       int               `json:"worker_memory_mib,omitempty"`
}
type Adapter struct {
	cfg      Config
	client   *http.Client
	proxy    *httputil.ReverseProxy
	slots    chan struct{}
	sequence atomic.Uint64
	instance string
	logger   *slog.Logger
	Version  string
}

func New(cfg Config, version string) (*Adapter, error) {
	u, e := url.Parse(cfg.BackendURL)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !net.ParseIP(u.Hostname()).IsLoopback() {
		return nil, errors.New("backend must be a fixed loopback HTTP URL without credentials/query/fragment")
	}
	if !filepath.IsAbs(cfg.WorkerBinary) || !filepath.IsAbs(cfg.WorkRoot) {
		return nil, errors.New("worker binary and work root must be absolute")
	}
	if (cfg.StateDBPath != "" && !filepath.IsAbs(cfg.StateDBPath)) || (cfg.CapabilityFile != "" && !filepath.IsAbs(cfg.CapabilityFile)) {
		return nil, errors.New("state_db_path and capability_file must be absolute when configured")
	}
	if cfg.UpstreamVersion == "" {
		return nil, errors.New("upstream_version must pin the expected engine version")
	}
	if cfg.MaxWorkers == 0 {
		cfg.MaxWorkers = 2
	}
	if cfg.MaxWorkers < 1 || cfg.MaxWorkers > 8 {
		return nil, errors.New("max_workers must be between 1 and 8")
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 75
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 300 {
		return nil, errors.New("timeout_seconds must be between 1 and 300")
	}
	if cfg.WorkerMemoryMiB != 0 && (cfg.WorkerMemoryMiB < 128 || cfg.WorkerMemoryMiB > 1024) {
		return nil, errors.New("worker_memory_mib must be zero or between 128 and 1024")
	}
	for backend, local := range cfg.ServerDataRoots {
		if !filepath.IsAbs(backend) || !filepath.IsAbs(local) {
			return nil, errors.New("server data roots must be absolute paths")
		}
	}
	if e = os.MkdirAll(cfg.WorkRoot, 0o700); e != nil {
		return nil, e
	}
	if e = checkWorkerHash(cfg.WorkerBinary, cfg.WorkerSHA256); e != nil {
		return nil, e
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p := httputil.NewSingleHostReverseProxy(u)
	direct := p.Director
	p.Director = func(r *http.Request) { direct(r); r.Host = u.Host }
	p.Transport = http.DefaultTransport.(*http.Transport).Clone()
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, e
	}
	a := &Adapter{cfg: cfg, client: client, proxy: p, slots: make(chan struct{}, cfg.MaxWorkers), instance: hex.EncodeToString(nonce[:]), Version: version}
	p.ModifyResponse = a.modifyResponse
	p.ErrorHandler = a.proxyFailure
	return a, nil
}

func (a *Adapter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(marshal(map[string]any{"ready": true, "adapter_version": a.Version, "worker_sha256": a.cfg.WorkerSHA256, "max_workers": cap(a.slots), "active_workers": len(a.slots)}))
	})
	mux.HandleFunc("/mcp", a.serveMCP)
	return http.NewCrossOriginProtection().Handler(mux)
}
func (a *Adapter) serveMCP(w http.ResponseWriter, r *http.Request) {
	refs := r.Header.Values(callRefHeader)
	r.Header.Del(callRefHeader)
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	if len(refs) == 1 && net.ParseIP(peer).IsLoopback() && callRefPattern.MatchString(refs[0]) {
		r = r.WithContext(context.WithValue(r.Context(), callRefKey{}, refs[0]))
	}
	if r.Method != "POST" {
		a.proxy.ServeHTTP(w, r)
		return
	}
	raw, e := readBounded(http.MaxBytesReader(w, r.Body, maxResponse), maxResponse)
	if e != nil {
		http.Error(w, "request exceeds size limit", 413)
		return
	}
	r.Body = ioBody(raw)
	var m message
	if e = json.Unmarshal(raw, &m); e != nil || m.Version != "2.0" {
		a.proxy.ServeHTTP(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(a.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	switch m.Method {
	case "tools/list":
		status, err := a.nativeTool(ctx, r.Header, "onto_status", object{})
		if err == nil {
			var info object
			info, err = payload(status)
			if err == nil && str(info, "version") != a.cfg.UpstreamVersion {
				slog.Error("upstream version mismatch", "expected", a.cfg.UpstreamVersion, "observed", str(info, "version"))
				err = errors.New("upstream version mismatch; update and verify backend/worker together")
			}
		}
		var result json.RawMessage
		if err == nil {
			result, err = a.native(ctx, r.Header, "tools/list", json.RawMessage(m.Params))
		}
		if err == nil {
			result, err = overlayCatalog(result)
		}
		if err != nil {
			http.Error(w, "cannot read/adapt upstream tool catalog", 502)
			return
		}
		a.respond(w, m.ID, result)
	case "tools/call":
		var params struct {
			Name      string `json:"name"`
			Arguments object `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &params) != nil {
			a.proxy.ServeHTTP(w, r)
			return
		}
		args := params.Arguments
		if args == nil {
			args = object{}
		}
		started := time.Now()
		if !needsAdaptation(params.Name, args) {
			r = r.WithContext(context.WithValue(r.Context(), callKey{}, callInfo{name: params.Name, args: args, id: m.ID, started: started}))
			a.proxy.ServeHTTP(w, r)
			return
		}
		result, err := a.adapt(ctx, r.Header, params.Name, args)
		if err != nil {
			result = errorTool(err)
		}
		original := result
		result = a.decorate(params.Name, args, result)
		a.logAdaptedCall(ctx, params.Name, args, started, result, original, err)
		a.respond(w, m.ID, result)
	default:
		a.proxy.ServeHTTP(w, r)
	}
}
func ioBody(raw []byte) *readCloser { return &readCloser{Reader: bytes.NewReader(raw)} }

type readCloser struct{ *bytes.Reader }

func (*readCloser) Close() error { return nil }
func (a *Adapter) respond(w http.ResponseWriter, id, result json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(marshal(message{Version: "2.0", ID: id, Result: result}))
}
func needsAdaptation(name string, args object) bool {
	if name == "onto_defects" || name == "onto_classify_el" || name == "onto_align" {
		return true
	}
	return (name == "onto_convert" && (supplied(args, "inline_content") || supplied(args, "from"))) || auditedTools[name] || name == "onto_dl_check" || name == "onto_dl_explain" || (scopedTools[name] && supplied(args, "ontology_turtle")) || (name == "onto_ingest" && (supplied(args, "inline_data") || boolean(args, "dry_run"))) || (name == "onto_diff" && (supplied(args, "old_content") || supplied(args, "new_content")))
}

func (a *Adapter) withWorker(ctx context.Context, fn func(*worker) (json.RawMessage, error)) (json.RawMessage, error) {
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	case <-ctx.Done():
		return nil, observe(codeDeadline, stageCapacity, errors.New("private worker capacity/deadline exceeded; shared store was not touched"))
	}
	w, e := startWorkerLimited(ctx, a.cfg.WorkerBinary, a.cfg.WorkRoot, a.Version, a.cfg.WorkerMemoryMiB)
	if e != nil {
		return nil, e
	}
	defer w.close()
	status, e := w.checked("onto_status", object{})
	if e != nil {
		return nil, observe(codeIdentity, stageIdentity, e)
	}
	if str(status, "version") != a.cfg.UpstreamVersion {
		return nil, observe(codeVersion, stageIdentity, errors.New("private worker engine version mismatch"))
	}
	return fn(w)
}
func (a *Adapter) adapt(ctx context.Context, h http.Header, name string, args object) (json.RawMessage, error) {
	args = clone(args)
	if name == "onto_defects" {
		return a.defects(ctx, h, args)
	}
	if name == "onto_classify_el" {
		return a.boundedAnalysis(ctx, h, name, args)
	}
	if name == "onto_align" {
		return a.align(ctx, h, args)
	}
	if name == "onto_convert" {
		return a.convert(ctx, args)
	}
	if auditedTools[name] {
		return a.auditCall(ctx, h, name, args)
	}
	if name == "onto_dl_check" || name == "onto_dl_explain" {
		keys := []string{"class_iri"}
		if name == "onto_dl_check" {
			keys = []string{"sub_class", "super_class"}
		}
		for _, key := range keys {
			value, e := normalizeIRI(str(args, key))
			if e != nil {
				return nil, e
			}
			args[key] = marshal(value)
		}
	}
	if scopedTools[name] && supplied(args, "ontology_turtle") {
		turtle := str(args, "ontology_turtle")
		if len(turtle) > maxInline {
			return nil, errors.New("ontology_turtle exceeds 1 MiB")
		}
		delete(args, "ontology_turtle")
		if supplied(args, "certificate_dir") || supplied(args, "rules_file") {
			return nil, errors.New("request-local evaluation rejects certificate_dir and rules_file")
		}
		if name == "onto_shacl" && !boolean(args, "inline") {
			text, e := a.readServerFile(str(args, "shapes"))
			if e != nil {
				return nil, e
			}
			args["shapes"] = marshal(string(text))
			args["inline"] = marshal(true)
		}
		return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
			if _, e := w.checked("onto_load", object{"turtle": marshal(turtle)}); e != nil {
				return nil, e
			}
			result, e := w.call(name, args)
			if e == nil {
				result = scopeResult(result)
			}
			return result, e
		})
	}
	if name == "onto_ingest" {
		return a.ingest(ctx, h, args)
	}
	if name == "onto_diff" {
		return a.diff(ctx, h, args)
	}
	return a.nativeTool(ctx, h, name, args)
}
