// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	BackendURL      string            `json:"backend_url"`
	WorkerBinary    string            `json:"worker_binary"`
	WorkerSHA256    string            `json:"worker_sha256"`
	WorkRoot        string            `json:"work_root"`
	ServerDataRoots map[string]string `json:"server_data_roots,omitempty"`
	MaxWorkers      int               `json:"max_workers,omitempty"`
	TimeoutSeconds  int               `json:"timeout_seconds,omitempty"`
}
type Adapter struct {
	cfg      Config
	client   *http.Client
	proxy    *httputil.ReverseProxy
	slots    chan struct{}
	sequence atomic.Uint64
	instance string
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
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, e
	}
	return &Adapter{cfg: cfg, client: client, proxy: p, slots: make(chan struct{}, cfg.MaxWorkers), instance: hex.EncodeToString(nonce[:]), Version: version}, nil
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
		result, err := a.native(ctx, r.Header, "tools/list", json.RawMessage(m.Params))
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
		if !needsAdaptation(params.Name, args) {
			a.proxy.ServeHTTP(w, r)
			return
		}
		result, err := a.adapt(ctx, r.Header, params.Name, args)
		if err != nil {
			result = errorTool(err)
		}
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
	return name == "onto_dl_check" || name == "onto_dl_explain" || (scopedTools[name] && supplied(args, "ontology_turtle")) || (name == "onto_ingest" && (supplied(args, "inline_data") || boolean(args, "dry_run"))) || (name == "onto_diff" && (supplied(args, "old_content") || supplied(args, "new_content")))
}

func (a *Adapter) withWorker(ctx context.Context, fn func(*worker) (json.RawMessage, error)) (json.RawMessage, error) {
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	case <-ctx.Done():
		return nil, errors.New("private worker capacity/deadline exceeded; shared store was not touched")
	}
	w, e := startWorker(ctx, a.cfg.WorkerBinary, a.cfg.WorkRoot)
	if e != nil {
		return nil, e
	}
	defer w.close()
	return fn(w)
}
func (a *Adapter) adapt(ctx context.Context, h http.Header, name string, args object) (json.RawMessage, error) {
	args = clone(args)
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
