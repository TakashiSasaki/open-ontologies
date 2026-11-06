// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const publicInputLimit = 64 << 10
const publicOutputLimit = 2 << 20
const publicTripleLimit = 2000
const publicPageSize = 25

// Only explicitly selected graphs are public. No native MCP or REST router is
// reachable through this handler; every backend request is constructed here.
type PublicConfig struct {
	Engine Config   `json:"engine"`
	Graphs []string `json:"graphs"`
}
type PublicWeb struct {
	adapter             *Adapter
	graphs              []string
	reads               chan struct{}
	jobs                chan struct{}
	mu                  sync.Mutex
	window              time.Time
	readCount, jobCount int
}
type webFault struct {
	status        int
	code, message string
}

func (e *webFault) Error() string       { return e.message }
func bad(message string) error          { return &webFault{400, "invalid_input", message} }
func failed(code, message string) error { return &webFault{502, code, message} }

type publicJob struct {
	Operation string `json:"operation"`
	Content   string `json:"content"`
	Format    string `json:"format"`
	To        string `json:"to,omitempty"`
	Profile   string `json:"profile,omitempty"`
}

func NewPublicWeb(cfg PublicConfig, version string) (*PublicWeb, error) {
	if len(cfg.Graphs) == 0 || len(cfg.Graphs) > 16 {
		return nil, errors.New("configure one to sixteen public graphs")
	}
	seen := map[string]bool{}
	for _, g := range cfg.Graphs {
		if seen[g] {
			return nil, errors.New("duplicate public graph")
		}
		seen[g] = true
		if g != "default" {
			if _, e := normalizeIRI(g); e != nil {
				return nil, errors.New("public graph must be an absolute IRI")
			}
		}
	}
	// Public calculations cannot inherit mounts, state databases or arbitrary
	// operator defaults intended for trusted MCP clients.
	cfg.Engine.ServerDataRoots = nil
	cfg.Engine.StateDBPath = ""
	cfg.Engine.CapabilityFile = ""
	cfg.Engine.MaxWorkers = 2
	cfg.Engine.TimeoutSeconds = 15
	cfg.Engine.WorkerMemoryMiB = 512
	a, e := New(cfg.Engine, version)
	if e != nil {
		return nil, e
	}
	a.client = &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: nil, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &PublicWeb{adapter: a, graphs: append([]string(nil), cfg.Graphs...), reads: make(chan struct{}, 2), jobs: make(chan struct{}, 2)}, nil
}
func (p *PublicWeb) Ready(ctx context.Context) error {
	b, e := p.native(ctx, "/health", nil)
	if e != nil {
		return e
	}
	var h struct{ Status, Version string }
	if json.Unmarshal(b, &h) != nil || h.Status != "ok" || h.Version != p.adapter.cfg.UpstreamVersion {
		return errors.New("native version/readiness mismatch")
	}
	return nil
}
func (p *PublicWeb) native(ctx context.Context, path string, body any) ([]byte, error) {
	// This function is private and called only with the two constant read routes.
	if path != "/health" && path != "/api/query" {
		return nil, errors.New("non-read backend path refused")
	}
	method := "GET"
	var reader io.Reader
	if body != nil {
		method = "POST"
		reader = bytes.NewReader(marshal(body))
	}
	req, e := http.NewRequestWithContext(ctx, method, p.adapter.cfg.BackendURL+path, reader)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := p.adapter.client.Do(req)
	if e != nil {
		return nil, failed("backend_unavailable", "データを取得できませんでした。少し待って再実行してください。")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, failed("backend_unavailable", "読み取りサービスが応答していません。")
	}
	b, e := readBounded(r.Body, publicOutputLimit)
	if e != nil {
		return nil, failed("output_limit", "応答が大きすぎます。検索範囲を狭めてください。")
	}
	return b, nil
}
func (p *PublicWeb) query(ctx context.Context, q string) ([]map[string]string, error) {
	if e := p.Ready(ctx); e != nil {
		return nil, failed("backend_unavailable", "本体の状態・バージョンを確認できません。")
	}
	b, e := p.native(ctx, "/api/query", map[string]string{"query": q})
	if e != nil {
		return nil, e
	}
	var v struct {
		Results []map[string]string `json:"results"`
		Error   json.RawMessage     `json:"error"`
	}
	if json.Unmarshal(b, &v) != nil || len(v.Error) > 0 || v.Results == nil {
		return nil, failed("query_failed", "データの読み取りに失敗しました。")
	}
	return v.Results, nil
}
func rdfText(term string) string {
	if strings.HasPrefix(term, "<") && strings.HasSuffix(term, ">") {
		return term[1 : len(term)-1]
	}
	if strings.HasPrefix(term, `"`) {
		escaped := false
		for i := 1; i < len(term); i++ {
			if escaped {
				escaped = false
				continue
			}
			if term[i] == '\\' {
				escaped = true
				continue
			}
			if term[i] == '"' {
				v, e := strconv.Unquote(term[:i+1])
				if e == nil {
					return v
				}
				break
			}
		}
	}
	return term
}
func rdfString(value string) string { return string(marshal(value)) }
func (p *PublicWeb) graph(r *http.Request) (string, error) {
	g := r.URL.Query().Get("graph")
	if g == "" {
		g = p.graphs[0]
	}
	for _, allowed := range p.graphs {
		if g == allowed {
			return g, nil
		}
	}
	return "", bad("このグラフは一般公開の対象に含まれていません。")
}
func scoped(g, pattern string) string {
	if g == "default" {
		return pattern
	}
	iri, _ := normalizeIRI(g)
	return "GRAPH " + iri + " { " + pattern + " }"
}
func allowedQuery(r *http.Request, keys ...string) error {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return bad("クエリ文字列が不正です。")
	}
	for key, v := range values {
		if !allowed[key] || len(v) != 1 {
			return bad("未対応または重複した引数があります。")
		}
	}
	return nil
}
func (p *PublicWeb) status(ctx context.Context, r *http.Request) (any, error) {
	if e := allowedQuery(r, "graph"); e != nil {
		return nil, e
	}
	g, e := p.graph(r)
	if e != nil {
		return nil, e
	}
	rows, e := p.query(ctx, "SELECT (COUNT(*) AS ?count) WHERE { "+scoped(g, "?s ?p ?o")+" }")
	if e != nil {
		return nil, e
	}
	if len(rows) != 1 {
		return nil, failed("query_failed", "件数を確認できません。")
	}
	n, e := strconv.ParseUint(rdfText(rows[0]["count"]), 10, 64)
	if e != nil {
		return nil, failed("query_failed", "件数の応答が不正です。")
	}
	return map[string]any{"graph": g, "graphs": p.graphs, "triples": n, "engine_version": p.adapter.cfg.UpstreamVersion,
		"api_version": p.adapter.Version, "observed_at": time.Now().UTC().Format(time.RFC3339), "scope": "live_public_graph", "read_only": true}, nil
}
func (p *PublicWeb) resources(ctx context.Context, r *http.Request) (any, error) {
	if e := allowedQuery(r, "graph", "q", "kind", "offset"); e != nil {
		return nil, e
	}
	g, e := p.graph(r)
	if e != nil {
		return nil, e
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 128 {
		return nil, bad("検索語は128文字以内にしてください。")
	}
	offset := 0
	if s := r.URL.Query().Get("offset"); s != "" {
		offset, e = strconv.Atoi(s)
		if e != nil || offset < 0 || offset > 10000 {
			return nil, bad("ページ範囲が大きすぎます。検索語で絞り込んでください。")
		}
	}
	pattern := "?s ?p ?o . FILTER(isIRI(?s)) "
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "", "all":
	case "class":
		pattern = `?s a ?t . VALUES ?t { <http://www.w3.org/2002/07/owl#Class> <http://www.w3.org/2000/01/rdf-schema#Class> } FILTER(isIRI(?s)) `
	case "property":
		pattern = `?s a ?t . VALUES ?t { <http://www.w3.org/1999/02/22-rdf-syntax-ns#Property> <http://www.w3.org/2002/07/owl#ObjectProperty> <http://www.w3.org/2002/07/owl#DatatypeProperty> <http://www.w3.org/2002/07/owl#AnnotationProperty> } FILTER(isIRI(?s)) `
	default:
		return nil, bad("未対応の種類です。")
	}
	if q != "" {
		value := rdfString(strings.ToLower(q))
		pattern += "FILTER(CONTAINS(LCASE(STR(?s)), " + value + ") || EXISTS { ?s <http://www.w3.org/2000/01/rdf-schema#label> ?text . FILTER(CONTAINS(LCASE(STR(?text)), " + value + ")) }) "
	}
	// Group duplicate subjects before sorting (DISTINCT after sorting is much
	// slower for subjects with many statements), then limit before label expansion. Label text is clipped in the native
	// query, so a very large stored literal cannot inflate the public reply.
	query := fmt.Sprintf("SELECT ?s (SAMPLE(SUBSTR(STR(?label),1,160)) AS ?label) WHERE { { SELECT ?s WHERE { %s } GROUP BY ?s ORDER BY ?s LIMIT %d OFFSET %d } OPTIONAL { %s } } GROUP BY ?s ORDER BY ?s", scoped(g, pattern), publicPageSize+1, offset, scoped(g, "?s <http://www.w3.org/2000/01/rdf-schema#label> ?label"))
	rows, e := p.query(ctx, query)
	if e != nil {
		return nil, e
	}
	more := len(rows) > publicPageSize
	if more {
		rows = rows[:publicPageSize]
	}
	items := []map[string]string{}
	for _, row := range rows {
		items = append(items, map[string]string{"id": rdfText(row["s"]), "label": rdfText(row["label"])})
	}
	return map[string]any{"items": items, "graph": g, "offset": offset, "page_size": publicPageSize, "has_more": more, "observed_at": time.Now().UTC().Format(time.RFC3339), "read_only": true}, nil
}
func subjectPattern(value string) (string, error) {
	if len(value) > 4096 || value == "" {
		return "", bad("リソース識別子が不正です。")
	}
	if strings.HasPrefix(value, "_:") {
		// SPARQL blank-node labels in a query are existential, not stable
		// references to stored nodes; STR(blank) is not defined by SPARQL.
		return "", bad("匿名ノードIDはIRIとして検索できません。静的スナップショットの詳細ページを参照してください。")
	}
	iri, e := normalizeIRI(value)
	if e != nil {
		return "", bad("絶対IRIを指定してください。")
	}
	return iri + " ?predicate ?o", nil
}
func (p *PublicWeb) resource(ctx context.Context, r *http.Request) (any, error) {
	if e := allowedQuery(r, "graph", "id"); e != nil {
		return nil, e
	}
	g, e := p.graph(r)
	if e != nil {
		return nil, e
	}
	id := r.URL.Query().Get("id")
	pattern, e := subjectPattern(id)
	if e != nil {
		return nil, e
	}
	query := `SELECT (SUBSTR(STR(?predicate),1,2000) AS ?p) (IF(isBlank(?o),?o,SUBSTR(STR(?o),1,2000)) AS ?value) (IF(isBlank(?o),0,STRLEN(STR(?o))) AS ?length) (IF(isIRI(?o),"iri",IF(isBlank(?o),"bnode","literal")) AS ?kind) (LANG(?o) AS ?lang) (STR(DATATYPE(?o)) AS ?datatype) WHERE { ` + scoped(g, pattern) + ` } ORDER BY ?predicate ?o LIMIT 101`
	rows, e := p.query(ctx, query)
	if e != nil {
		return nil, e
	}
	more := len(rows) > 100
	if more {
		rows = rows[:100]
	}
	for _, row := range rows {
		for k, v := range row {
			row[k] = rdfText(v)
		}
	}
	return map[string]any{"id": id, "graph": g, "properties": rows, "truncated": more, "value_char_limit": 2000, "observed_at": time.Now().UTC().Format(time.RFC3339), "read_only": true}, nil
}
func validateJob(j publicJob) error {
	if strings.TrimSpace(j.Content) == "" || len(j.Content) > publicInputLimit || !utf8.ValidString(j.Content) {
		return bad("入力は空でないUTF-8のRDFテキスト、64 KiB以内にしてください。")
	}
	if j.Format != "turtle" && j.Format != "ntriples" {
		return bad("入力形式はTurtleまたはN-Triplesです。URLやファイルパスは指定できません。")
	}
	switch j.Operation {
	case "convert":
		switch j.To {
		case "turtle", "ntriples", "rdfxml", "nquads", "trig", "jsonld", "ofn":
		default:
			return bad("未対応の出力形式です。")
		}
		if j.Profile != "" {
			return bad("変換には推論プロファイルを指定しません。")
		}
	case "reason":
		if j.Profile != "rdfs" && j.Profile != "owl-rl" {
			return bad("推論プロファイルはrdfsまたはowl-rlです。")
		}
		if j.To != "" {
			return bad("推論の出力はN-Triplesです。")
		}
	case "validate", "classify", "defects":
		if j.To != "" || j.Profile != "" {
			return bad("この操作に出力形式・推論プロファイルは指定できません。")
		}
	default:
		return bad("未対応の操作です。")
	}
	return nil
}
func (p *PublicWeb) calculate(ctx context.Context, j publicJob) (any, error) {
	if e := validateJob(j); e != nil {
		return nil, e
	}
	// The same pinned engine is used, but only with new, private state. No request
	// field can select a native tool, server path, model, rule file or certificate.
	raw, e := p.adapter.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		path := w.dir + "/input.ttl"
		if j.Format == "ntriples" {
			path = w.dir + "/input.nt"
		}
		if e := writePrivateInput(path, j.Content); e != nil {
			return nil, e
		}
		converted, e := w.call("onto_convert", object{"path": marshal(path), "to": marshal("nquads")})
		if e != nil {
			return nil, e
		}
		if e = resultFailure(converted); e != nil {
			return nil, errors.New(strings.ReplaceAll(e.Error(), w.dir, "[input]"))
		}
		nq, e := textContent(converted)
		if e != nil {
			return nil, e
		}
		if e = checkInlineIRIs(nq); e != nil {
			return nil, e
		}
		count := strings.Count(strings.TrimSpace(nq), "\n")
		if strings.TrimSpace(nq) != "" {
			count++
		}
		if count > publicTripleLimit {
			return nil, bad("一度に処理できる入力は2,000トリプルまでです。")
		}
		out := object{"operation": marshal(j.Operation), "input_triples": marshal(count), "execution_scope": marshal("request"), "persistent_store_modified": marshal(false), "engine_version": marshal(p.adapter.cfg.UpstreamVersion)}
		if j.Operation == "validate" {
			out["valid"] = marshal(true)
			return marshal(out), nil
		}
		if j.Operation == "convert" {
			r, e := w.call("onto_convert", object{"path": marshal(path), "to": marshal(j.To)})
			if e != nil {
				return nil, e
			}
			if e = resultFailure(r); e != nil {
				return nil, e
			}
			text, e := textContent(r)
			if e != nil {
				return nil, e
			}
			report, e := conversionPayload(j.To, text)
			if e != nil {
				return nil, e
			}
			out["report"] = marshal(report)
			out["content"] = report["content"]
			out["format"] = marshal(j.To)
		} else {
			if _, e := w.checked("onto_load", object{"turtle": marshal(nq)}); e != nil {
				return nil, e
			}
			name := "onto_reason"
			args := object{"profile": marshal(j.Profile), "materialize": marshal(true)}
			if j.Operation == "classify" {
				name = "onto_classify_el"
				args = object{}
			}
			if j.Operation == "defects" {
				name = "onto_defects"
				args = object{"limit": marshal(100)}
			}
			report, e := w.checked(name, args)
			if e != nil {
				return nil, e
			}
			out["report"] = marshal(report)
			if j.Operation == "reason" {
				content, e := w.save("result.nt")
				if e != nil {
					return nil, e
				}
				out["content"] = marshal(content)
				out["format"] = marshal("ntriples")
			}
		}
		b := marshal(out)
		if len(b) > publicOutputLimit {
			return nil, &webFault{422, "output_limit", "結果が大きすぎます。入力を小さくしてください。"}
		}
		return b, nil
	})
	if e != nil {
		if ctx.Err() != nil {
			return nil, &webFault{422, "calculation_timeout", "制限時間に達しました。入力を小さくしてください。完全な結果は得られていません。"}
		}
		var f *webFault
		if errors.As(e, &f) {
			return nil, f
		}
		// Native error details can contain scratch paths or user content. The public
		// contract returns a fixed explanation rather than raw implementation errors.
		return nil, &webFault{422, "calculation_failed", "入力の解析または計算に失敗しました。構文、絶対IRI、入力サイズを確認してください。"}
	}
	return raw, nil
}
func (p *PublicWeb) reserve(job bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.window) >= time.Minute {
		p.window = time.Now()
		p.readCount = 0
		p.jobCount = 0
	}
	if job {
		if p.jobCount >= 12 {
			return false
		}
		p.jobCount++
	} else {
		if p.readCount >= 120 {
			return false
		}
		p.readCount++
	}
	return true
}
func (p *PublicWeb) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		status := 200
		operation := "unknown"
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		defer func() {
			slog.Info("public web request", "operation", operation, "status", status, "duration_ms", time.Since(started).Milliseconds())
		}()
		var result any
		var err error
		defer func() {
			if err != nil {
				f := &webFault{500, "internal_error", "処理に失敗しました。"}
				_ = errors.As(err, &f)
				status = f.status
				result = map[string]any{"error": map[string]string{"code": f.code, "message": f.message}, "complete": false, "persistent_store_modified": false}
				if status == 429 {
					w.Header().Set("Retry-After", "10")
				}
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(result)
		}()
		route := strings.TrimPrefix(r.URL.Path, "/web-api/v1/")
		if route != "status" && route != "resources" && route != "resource" && route != "calculate" {
			err = &webFault{404, "not_found", "公開されていない操作です。"}
			return
		}
		operation = route
		if r.Method == "OPTIONS" {
			result = map[string]bool{"ok": true}
			return
		}
		job := route == "calculate"
		expected := "GET"
		if job {
			expected = "POST"
		}
		if r.Method != expected {
			w.Header().Set("Allow", expected+", OPTIONS")
			err = &webFault{405, "method_not_allowed", "このHTTPメソッドは利用できません。"}
			return
		}
		slots := p.reads
		if job {
			slots = p.jobs
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			err = &webFault{429, "busy", "処理が混み合っています。少し待って再実行してください。"}
			return
		}
		if !p.reserve(job) {
			err = &webFault{429, "rate_limit", "短時間の利用上限に達しました。少し待って再実行してください。"}
			return
		}
		seconds := 6
		if job {
			seconds = 15
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(seconds)*time.Second)
		defer cancel()
		switch route {
		case "status":
			result, err = p.status(ctx, r)
		case "resources":
			result, err = p.resources(ctx, r)
		case "resource":
			result, err = p.resource(ctx, r)
		case "calculate":
			if r.URL.RawQuery != "" {
				err = bad("計算はJSON本文で指定してください。")
				return
			}
			if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
				err = &webFault{415, "content_type", "application/jsonで送信してください。"}
				return
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 384<<10))
			decoder.DisallowUnknownFields()
			var input publicJob
			if e := decoder.Decode(&input); e != nil {
				err = bad("JSONが不正、引数が未対応、または入力が大きすぎます。")
				return
			}
			if decoder.Decode(new(any)) != io.EOF {
				err = bad("JSONは一つのオブジェクトを指定してください。")
				return
			}
			result, err = p.calculate(ctx, input)
		}
	})
}
