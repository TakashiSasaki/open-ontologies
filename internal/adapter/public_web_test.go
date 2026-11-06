// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func publicFixture(t *testing.T) (*PublicWeb, *[]string, *sync.Mutex) {
	t.Helper()
	queries := []string{}
	mu := &sync.Mutex{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","version":"fixture"}`))
			return
		}
		if r.URL.Path != "/api/query" || r.Method != "POST" {
			t.Errorf("unexpected native route %s %s", r.Method, r.URL.Path)
			http.Error(w, "forbidden", 500)
			return
		}
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		queries = append(queries, req["query"])
		mu.Unlock()
		if strings.Contains(req["query"], "COUNT(*)") {
			_, _ = w.Write([]byte(`{"results":[{"count":"\"42\"^^<http://www.w3.org/2001/XMLSchema#integer>"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"s":"<urn:example:A>","label":"\"test label\""}]}`))
	}))
	t.Cleanup(backend.Close)
	a := &Adapter{cfg: Config{BackendURL: backend.URL, UpstreamVersion: "fixture", WorkRoot: t.TempDir(), WorkerBinary: "/bin/false", WorkerMemoryMiB: 512}, slots: make(chan struct{}, 2), client: backend.Client(), Version: "test"}
	return &PublicWeb{adapter: a, graphs: []string{"default", "urn:public"}, reads: make(chan struct{}, 4), jobs: make(chan struct{}, 2)}, &queries, mu
}
func webRequest(p *PublicWeb, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	return w
}
func TestPublicRoutesNeverForwardMutationsOrRawMCP(t *testing.T) {
	p, q, _ := publicFixture(t)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/mcp", `{"method":"tools/call","params":{"name":"onto_clear"}}`},
		{"POST", "/api/update", `{"query":"CLEAR ALL"}`}, {"POST", "/web-api/v1/update", `{}`},
		{"POST", "/web-api/v1/resources", `{}`}, {"GET", "/web-api/v1/calculate", ""},
		{"POST", "/web-api/v1/calculate", `{"operation":"convert","content":"x","format":"turtle","to":"turtle","path":"/etc/passwd"}`},
		{"POST", "/web-api/v1/calculate", `{"operation":"onto_clear","content":"x","format":"turtle"}`},
		{"GET", "/web-api/v1/resources?query=SELECT%20*", ""},
		{"GET", "/web-api/v1/resources?graph=urn:private", ""},
		{"GET", "/web-api/v1/resource?id=urn:a%3E%20%7D%20UNION%20%7B%20%3Curn:b", ""},
		{"GET", "/web-api/v1/resources?q=a&q=b", ""}, {"GET", "/web-api/v1/resources?q=%XX", ""},
	} {
		w := webRequest(p, tc.method, tc.path, tc.body)
		if w.Code < 400 {
			t.Fatalf("accepted %s: %s", tc.path, w.Body.String())
		}
		if !json.Valid(w.Body.Bytes()) {
			t.Fatal("error response was not JSON")
		}
	}
	if len(*q) != 0 {
		t.Fatalf("rejected requests reached native: %v", *q)
	}
}
func TestPublicReadQueriesBindDataAndBoundResults(t *testing.T) {
	p, queries, _ := publicFixture(t)
	injection := `" } UNION { SERVICE <http://127.0.0.1:1/> { ?s ?p ?o } } #`
	w := webRequest(p, "GET", "/web-api/v1/resources?q="+url.QueryEscape(injection), "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	q := (*queries)[0]
	ts, e := tokens(q)
	if e != nil {
		t.Fatal(e)
	}
	for _, tok := range ts {
		if tok.kind == "word" && strings.EqualFold(tok.text, "SERVICE") {
			t.Fatal("query injection escaped literal")
		}
	}
	if !strings.Contains(q, "LIMIT 26 OFFSET 0") || !strings.Contains(q, "SUBSTR(STR(?label),1,160)") {
		t.Fatal(q)
	}
	w = webRequest(p, "GET", "/web-api/v1/resource?id=urn:A&graph=urn:public", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	q = (*queries)[1]
	if !strings.Contains(q, "GRAPH <urn:public>") || !strings.Contains(q, "LIMIT 101") || !strings.Contains(q, "SUBSTR(STR(?o),1,2000)") {
		t.Fatal(q)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("live data cacheable")
	}
	w = webRequest(p, "GET", "/web-api/v1/status", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"triples":42`) {
		t.Fatal(w.Body.String())
	}
}
func TestPublicBlankNodeDoesNotPretendToBeStableIRI(t *testing.T) {
	for _, id := range []string{"_:abc123", "_:a }", "urn:a> <urn:b>", "relative"} {
		if _, e := subjectPattern(id); e == nil {
			t.Fatal(id)
		}
	}
}
func TestPublicJobInputAllowlists(t *testing.T) {
	base := publicJob{Operation: "reason", Content: "<urn:a> <urn:p> <urn:b> .", Format: "turtle", Profile: "rdfs"}
	if validateJob(base) != nil {
		t.Fatal("valid fixture rejected")
	}
	for _, alter := range []func(*publicJob){func(j *publicJob) { j.Content = strings.Repeat("x", publicInputLimit+1) }, func(j *publicJob) { j.Format = "jsonld" }, func(j *publicJob) { j.Format = "rdfxml" }, func(j *publicJob) { j.Profile = "owl-dl" }, func(j *publicJob) { j.To = "/etc/passwd" }, func(j *publicJob) { j.Operation = "load" }} {
		j := base
		alter(&j)
		if validateJob(j) == nil {
			t.Fatal("unsafe input accepted", j.Operation, j.Format)
		}
	}
}

func TestPublicReadStopsAfterNativeVersionChange(t *testing.T) {
	p, queries, _ := publicFixture(t)
	p.adapter.cfg.UpstreamVersion = "different-pinned-version"
	w := webRequest(p, "GET", "/web-api/v1/resources", "")
	if w.Code != 502 || len(*queries) != 0 {
		t.Fatal("unverified engine was queried", w.Code, w.Body.String())
	}
}
func TestPublicCapacityAndRateAreBounded(t *testing.T) {
	p, queries, _ := publicFixture(t)
	for i := 0; i < cap(p.reads); i++ {
		p.reads <- struct{}{}
	}
	w := webRequest(p, "GET", "/web-api/v1/resources", "")
	if w.Code != 429 || len(*queries) != 0 {
		t.Fatal(w.Code, *queries)
	}
	<-p.reads
	p.window = time.Now()
	p.readCount = 120
	w = webRequest(p, "GET", "/web-api/v1/resources", "")
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
}
func TestPublicCancelledWorkerIsReaped(t *testing.T) {
	p, _, _ := publicFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.calculate(ctx, publicJob{Operation: "validate", Content: "<urn:a> <urn:p> <urn:b> .", Format: "turtle"})
	var f *webFault
	if err == nil {
		t.Fatal("cancelled call succeeded")
	}
	_ = f
	if len(p.adapter.slots) != 0 {
		t.Fatal("slot leaked")
	}
}
