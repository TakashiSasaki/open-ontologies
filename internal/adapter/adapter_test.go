// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassIRINormalization(t *testing.T) {
	for _, s := range []string{"https://example.org/Cat", "<https://example.org/Cat>", " urn:example:Cat "} {
		got, e := normalizeIRI(s)
		if e != nil || !strings.HasPrefix(got, "<") || !strings.HasSuffix(got, ">") {
			t.Fatal(s, got, e)
		}
	}
	for _, s := range []string{"", "Cat", "<https://example.org/Cat", "https://example.org/Cat>", "https://example.org/a b", "https://example.org/a\nB"} {
		if _, e := normalizeIRI(s); e == nil {
			t.Fatal("invalid IRI accepted", s)
		}
	}
}
func TestSchemaOverlayDoesNotRoundOrChangeUnrelatedTools(t *testing.T) {
	original := marshal(map[string]any{"tools": []object{
		{"name": marshal("onto_ingest"), "inputSchema": json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"minimum":9007199254740993}},"required":["path"]}`)},
		{"name": marshal("unrelated"), "inputSchema": json.RawMessage(`{"type":"object","properties":{"counter":{"minimum":9007199254740993}}}`)},
	}})
	result, e := overlayCatalog(original)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(result), "9007199254740993") {
		t.Fatal("schema integer rounded")
	}
	var r struct {
		Tools []object `json:"tools"`
	}
	_ = json.Unmarshal(result, &r)
	var schema object
	_ = json.Unmarshal(r.Tools[0]["inputSchema"], &schema)
	if supplied(schema, "required") {
		t.Fatal("inline ingest still requires path")
	}
	var p object
	_ = json.Unmarshal(schema["properties"], &p)
	if !supplied(p, "inline_data") || !supplied(p, "dry_run") {
		t.Fatal("missing adapter inputs")
	}
}
func TestPrivatePathsCannotEscapeDeclaredRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600)
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600)
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	a := &Adapter{cfg: Config{ServerDataRoots: map[string]string{"/data": root}}}
	b, e := a.readServerFile("/data/ok.txt")
	if e != nil || string(b) != "ok" {
		t.Fatal(e)
	}
	for _, path := range []string{"/etc/passwd", "/data/../secret.txt", "/data/escape/secret.txt"} {
		if _, e := a.readServerFile(path); e == nil {
			t.Fatal("escaped root", path)
		}
	}
}
func TestUnsafePrivateOptionsAreRejectedBeforeWorkerOrBackend(t *testing.T) {
	a := &Adapter{}
	for _, args := range []object{
		{"ontology_turtle": marshal(""), "certificate_dir": marshal("/data/proof")},
		{"ontology_turtle": marshal(""), "rules_file": marshal("/etc/passwd")},
		{"ontology_turtle": marshal(strings.Repeat("x", maxInline+1))},
	} {
		if _, e := a.adapt(context.Background(), nil, "onto_reason", args); e == nil {
			t.Fatal("unsafe private option accepted")
		}
	}
	for _, args := range []object{{"inline_data": marshal("[]"), "path": marshal("/data/x")}, {"inline_data": marshal("[]")}, {"inline_data": marshal("binary"), "format": marshal("xlsx")}} {
		if _, e := a.ingest(context.Background(), nil, args); e == nil {
			t.Fatal("invalid ingest accepted")
		}
	}
}
func TestWorkerIdentityMustMatchPinnedExecutable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "worker")
	_ = os.WriteFile(file, []byte("different binary"), 0o700)
	if e := checkWorkerHash(file, strings.Repeat("0", 64)); e == nil {
		t.Fatal("worker hash mismatch accepted")
	}
}

func TestUpgradeCannotMixOldPrivateWorkerWithNewBackend(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request message
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message{Version: "2.0", ID: request.ID, Result: jsonTool(object{"version": marshal("2.2.0")})})
	}))
	defer backend.Close()
	dir := t.TempDir()
	binary := filepath.Join(dir, "worker")
	data := []byte("pinned but never executed")
	_ = os.WriteFile(binary, data, 0o700)
	hash := sha256.Sum256(data)
	a, e := New(Config{BackendURL: backend.URL + "/mcp", WorkerBinary: binary, WorkerSHA256: hex.EncodeToString(hash[:]), UpstreamVersion: "2.1.0", WorkRoot: filepath.Join(dir, "requests")}, "test")
	if e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, req)
	if response.Code != 502 || calls != 1 {
		t.Fatal("version mismatch was not rejected before catalog import", response.Code, calls)
	}
}

func TestWorkerDeadlineRemovesWorkspaceAndStopsProcess(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "blocked-worker")
	_ = os.WriteFile(binary, []byte("#!/bin/sh\nsleep 30\n"), 0o700)
	root := filepath.Join(dir, "requests")
	_ = os.Mkdir(root, 0o700)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	w, e := startWorker(ctx, binary, root, "test")
	if w != nil {
		w.close()
	}
	if e == nil {
		t.Fatal("blocked worker ignored deadline")
	}
	var buffer bytes.Buffer
	a := diagnosticAdapter(&buffer)
	a.logAdaptedCall(ctx, "onto_convert", object{}, started, errorTool(e), errorTool(e), e)
	if str(logObject(t, &buffer), "error_code") != string(codeDeadline) {
		t.Fatal("worker deadline was not classified", buffer.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || time.Since(started) > 5*time.Second {
		t.Fatal("private process/workspace was not cleaned", err, len(entries), time.Since(started))
	}
}
