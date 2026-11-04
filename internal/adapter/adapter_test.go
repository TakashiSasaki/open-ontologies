// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
