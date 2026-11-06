// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This test starts a separate native HTTP process with an empty private data
// directory. Its fixture writes never use a configured/production endpoint.
func TestPublicWebPinnedEngineIntegration(t *testing.T) {
	binary := os.Getenv("OO_PUBLIC_TEST_WORKER")
	if binary == "" {
		t.Skip("set OO_PUBLIC_TEST_WORKER to a pinned native executable")
	}
	b, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(b)
	root := t.TempDir()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.Command(binary, "serve-http", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--storage-mode", "memory", "--data-dir", filepath.Join(root, "native"), "--config", filepath.Join(root, "absent.toml"))
	cmd.Env = []string{"HOME=" + root, "TMPDIR=" + root, "PATH=/usr/bin:/bin", "RUST_LOG=error"}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	client := &http.Client{Timeout: 5 * time.Second}
	var version string
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		r, err := client.Get(base + "/health")
		if err == nil {
			var h struct{ Version string }
			err = json.NewDecoder(r.Body).Decode(&h)
			r.Body.Close()
			if err == nil && h.Version != "" {
				version = h.Version
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if version == "" {
		t.Fatal("isolated native startup failed")
	}
	native := func(path string, value any) []byte {
		t.Helper()
		r, err := client.Post(base+path, "application/json", bytes.NewReader(marshal(value)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil || r.StatusCode != 200 || bytes.Contains(b, []byte(`"error"`)) {
			t.Fatalf("native fixture: %s %v", b, err)
		}
		return b
	}
	fixture := `@prefix ex: <https://example.org/> . @prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> . ex:Dog rdfs:subClassOf ex:Animal . ex:Fido a ex:Dog .`
	native("/api/load-turtle", map[string]string{"turtle": fixture + ` ex:Fido ex:detail [ ex:name "名前 <script>alert(1)</script>"@ja ] .`})
	native("/api/update", map[string]string{"query": `INSERT DATA { GRAPH <urn:private> { <urn:secret> <urn:p> "hidden" } }`})
	p, e := NewPublicWeb(PublicConfig{Engine: Config{BackendURL: base, WorkerBinary: binary, WorkerSHA256: hex.EncodeToString(sum[:]), UpstreamVersion: version, WorkRoot: filepath.Join(root, "requests")}, Graphs: []string{"default"}}, "integration")
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Ready(context.Background()); e != nil {
		t.Fatal(e)
	}
	get := func(path string) map[string]any {
		t.Helper()
		w := webRequest(p, "GET", "/web-api/v1/"+path, "")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var data map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
			t.Fatal(e)
		}
		return data
	}
	if get("status")["triples"] != float64(4) {
		t.Fatal("wrong public scope count")
	}
	if len(get("resources?q=secret")["items"].([]any)) != 0 {
		t.Fatal("private graph leak")
	}
	if len(get("resources?q=" + url.QueryEscape(`" } UNION { SERVICE <http://127.0.0.1:1/> { ?s ?p ?o } } #`))["items"].([]any)) != 0 {
		t.Fatal("query injection")
	}
	data := get("resource?id=" + url.QueryEscape("https://example.org/Fido"))
	var blank string
	for _, v := range data["properties"].([]any) {
		row := v.(map[string]any)
		if row["kind"] == "bnode" {
			blank = row["value"].(string)
		}
	}
	if blank == "" {
		t.Fatal("blank node not represented")
	}
	if !strings.HasPrefix(blank, "_:") {
		t.Fatal("blank node lost its RDF marker")
	}
	if webRequest(p, "GET", "/web-api/v1/resource?id="+url.QueryEscape(blank), "").Code != 400 {
		t.Fatal("blank node was treated as a stable reference")
	}

	// Changes made to this fixture become visible without an export or a cache reload.
	native("/api/load-turtle", map[string]string{"turtle": `<urn:new> <urn:p> "newly written fixture" .`})
	if get("status")["triples"] != float64(5) || len(get("resources?q=urn:new")["items"].([]any)) != 1 {
		t.Fatal("live view did not refresh")
	}
	snapshot := func() []byte {
		return native("/api/query", map[string]string{"query": `SELECT ?s ?p ?o ?g WHERE { { ?s ?p ?o } UNION { GRAPH ?g { ?s ?p ?o } } } ORDER BY ?g ?s ?p ?o`})
	}
	before := snapshot()
	jobs := []publicJob{
		{Operation: "validate", Content: fixture, Format: "turtle"},
		{Operation: "reason", Content: fixture, Format: "turtle", Profile: "rdfs"},
		{Operation: "reason", Content: fixture, Format: "turtle", Profile: "owl-rl"},
		{Operation: "classify", Content: fixture, Format: "turtle"},
		{Operation: "defects", Content: fixture, Format: "turtle"},
	}
	for _, format := range []string{"turtle", "ntriples", "rdfxml", "nquads", "trig", "jsonld", "ofn"} {
		jobs = append(jobs, publicJob{Operation: "convert", Content: fixture, Format: "turtle", To: format})
	}
	for _, job := range jobs {
		t.Run(job.Operation+job.Profile+job.To, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			v, e := p.calculate(ctx, job)
			if e != nil {
				t.Fatal(e)
			}
			var out map[string]any
			if e = json.Unmarshal(v.(json.RawMessage), &out); e != nil {
				t.Fatal(e)
			}
			if out["persistent_store_modified"] != false || out["input_triples"] != float64(2) {
				t.Fatal(out)
			}
			if job.Operation == "reason" && !strings.Contains(out["content"].(string), "<https://example.org/Animal>") {
				t.Fatal("missing inferred statement")
			}
		})
	}
	var oversized strings.Builder
	for i := 0; i < 2001; i++ {
		fmt.Fprintf(&oversized, "<urn:s%d> <urn:p> <urn:o> .\n", i)
	}
	for _, invalid := range []string{`this is not Turtle`, `<file:///data/private> <urn:p> <urn:o> .`, `<relative> <urn:p> <urn:o> .`, oversized.String()} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, e := p.calculate(ctx, publicJob{Operation: "validate", Format: "turtle", Content: invalid})
		cancel()
		if e == nil {
			t.Fatalf("accepted invalid/over-limit input (%d bytes)", len(invalid))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.calculate(ctx, jobs[0]); e == nil {
		t.Fatal("canceled calculation accepted")
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("private calculations altered shared fixture data")
	}
	entries, e := os.ReadDir(p.adapter.cfg.WorkRoot)
	if e != nil || len(entries) != 0 {
		t.Fatalf("scratch leak: %v %v", entries, e)
	}
	// Verify actual OS limits, not only the config value.
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w, e := startWorkerLimited(ctx, binary, p.adapter.cfg.WorkRoot, "test", 512)
	if e != nil {
		t.Fatal(e)
	}
	defer w.close()
	limits, e := os.ReadFile(fmt.Sprintf("/proc/%d/limits", w.cmd.Process.Pid))
	if e != nil || !strings.Contains(string(limits), "536870912") || !strings.Contains(string(limits), "8388608") {
		t.Fatalf("worker resource limits missing: %s %v", limits, e)
	}
	pid := w.cmd.Process.Pid
	cancel()
	w.close()
	if _, e := os.Stat(fmt.Sprintf("/proc/%d", pid)); !os.IsNotExist(e) {
		t.Fatal("canceled worker not reaped")
	}
	entries, e = os.ReadDir(p.adapter.cfg.WorkRoot)
	if e != nil || len(entries) != 0 {
		t.Fatal("canceled worker scratch leaked")
	}

}
