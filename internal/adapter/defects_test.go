// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func failurePayload(t *testing.T, raw json.RawMessage, err error) object {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var p object
	var r object
	_ = json.Unmarshal(raw, &r)
	var blocks []object
	_ = json.Unmarshal(r["content"], &blocks)
	if len(blocks) != 1 || json.Unmarshal([]byte(str(blocks[0], "text")), &p) != nil {
		t.Fatal(string(raw))
	}
	return p
}

func snapshotFixture(t *testing.T, rows any) (*Adapter, *atomic.Int32, *atomic.Value) {
	t.Helper()
	calls := &atomic.Int32{}
	query := &atomic.Value{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var m message
		_ = json.NewDecoder(r.Body).Decode(&m)
		var p struct {
			Name      string
			Arguments object
		}
		_ = json.Unmarshal(m.Params, &p)
		query.Store(str(p.Arguments, "query"))
		if p.Name != "onto_query" || !strings.HasSuffix(str(p.Arguments, "query"), "LIMIT 10001") {
			t.Error("unexpected shared-store operation", p.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message{Version: "2.0", ID: m.ID, Result: jsonTool(object{"results": marshal(rows)})})
	}))
	t.Cleanup(backend.Close)
	return &Adapter{cfg: Config{BackendURL: backend.URL}, client: backend.Client()}, calls, query
}

func TestDefectBoundsRefuseBeforeAnyWorkerOrMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		rows any
		code string
	}{
		{"rows", make([]map[string]string, maxDefectRows+1), "analysis_scope_required"},
		{"bytes", []map[string]string{{"s": "<urn:s>", "p": "<urn:p>", "o": strings.Repeat("x", maxDefectBytes)}}, "analysis_scope_required"},
		{"malformed", []map[string]string{{"s": "<urn:s>"}}, "analysis_snapshot_failed"},
		{"missing", nil, "analysis_snapshot_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, calls, _ := snapshotFixture(t, test.rows)
			r, e := a.defects(context.Background(), http.Header{}, object{})
			p := failurePayload(t, r, e)
			if str(p, "error_code") != test.code || boolean(p, "complete") || boolean(p, "partial_results_available") || calls.Load() != 1 {
				t.Fatal(string(r), calls.Load())
			}
		})
	}
}

func TestInvalidDefectInputsNeverReachBackend(t *testing.T) {
	a, calls, _ := snapshotFixture(t, []any{})
	for _, args := range []object{
		{"graph_iri": marshal("urn:a> } UNION { ?s ?p ?o")},
		{"graph_iri": marshal("urn:g"), "ontology_turtle": marshal("")},
		{"max_seconds": marshal(0)}, {"max_seconds": marshal(21)}, {"max_seconds": marshal(1.5)},
		{"limit": marshal(-1)}, {"ontology_turtle": marshal(42)},
		{"ontology_turtle": marshal(strings.Repeat("x", maxInline+1))},
	} {
		r, e := a.defects(context.Background(), http.Header{}, args)
		if p := failurePayload(t, r, e); !supplied(p, "error") {
			t.Fatal(string(r))
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input triggered backend", calls.Load())
	}
}

func installFakeDefectWorker(t *testing.T, a *Adapter) (string, string) {
	t.Helper()
	dir := t.TempDir()
	worker := filepath.Join(dir, "worker")
	capture := filepath.Join(dir, "captured.json")
	block := filepath.Join(dir, "block")
	code := fmt.Sprintf(`#!/usr/bin/python3
import json,sys,time,os
for line in sys.stdin:
 q=json.loads(line)
 if 'id' not in q: continue
 result={}
 if q['method']=='tools/call':
  p=q['params']; name=p['name']; args=p['arguments']
  if name=='onto_status': value={'version':'fake-native'}
  elif name=='onto_load':
   open(%q,'w').write(json.dumps(args)); value={'ok':True}
  elif name=='onto_defects':
   if os.path.exists(%q): time.sleep(30)
   value={'defect_count':0,'kinds_checked':[],'truncated':{}}
  else: raise Exception('unexpected tool')
  result={'content':[{'type':'text','text':json.dumps(value)}]}
 print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':result}),flush=True)
`, capture, block)
	if e := os.WriteFile(worker, []byte(code), 0o700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "requests")
	_ = os.Mkdir(root, 0o700)
	a.cfg.WorkerBinary = worker
	a.cfg.WorkRoot = root
	a.cfg.UpstreamVersion = "fake-native"
	a.slots = make(chan struct{}, 2)
	return capture, block
}

func TestDefectPrivateSnapshotPreservesBlankLinks(t *testing.T) {
	a, calls, query := snapshotFixture(t, []map[string]string{{"s": "<urn:s>", "p": "<urn:p>", "o": "_:b"}, {"s": "_:b", "p": "<urn:p>", "o": "\"line\\nvalue\""}})
	capture, _ := installFakeDefectWorker(t, a)
	r, e := a.defects(context.Background(), http.Header{}, object{"graph_iri": marshal("urn:graph")})
	p := failurePayload(t, r, e)
	if !boolean(p, "complete") || boolean(p, "persistent_store_modified") || str(p, "execution_scope") != "request" || calls.Load() != 1 {
		t.Fatal(string(r))
	}
	b, _ := os.ReadFile(capture)
	var loaded object
	_ = json.Unmarshal(b, &loaded)
	if !strings.Contains(str(loaded, "turtle"), "<urn:s> <urn:p> _:b .\n_:b <urn:p>") {
		t.Fatal(string(b))
	}
	var scope object
	_ = json.Unmarshal(p["analysis_scope"], &scope)
	if str(scope, "kind") != "named_graph_snapshot" || boolean(scope, "global_verdict") {
		t.Fatal(string(p["analysis_scope"]))
	}
	if !strings.Contains(query.Load().(string), "GRAPH <urn:graph> { ?s ?p ?o }") || strings.Contains(query.Load().(string), "UNION") {
		t.Fatal("named graph selection was not enforced", query.Load())
	}
}

func TestDefectDeadlineStopsWorkerAndNextCallSucceeds(t *testing.T) {
	a := &Adapter{}
	_, block := installFakeDefectWorker(t, a)
	_ = os.WriteFile(block, []byte("block"), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	r, e := a.defects(ctx, http.Header{}, object{"ontology_turtle": marshal("<urn:s> <urn:p> 1 .")})
	p := failurePayload(t, r, e)
	if str(p, "error_code") != "analysis_deadline_exceeded" || boolean(p, "complete") || time.Since(started) > 3*time.Second {
		t.Fatal(string(r))
	}
	entries, err := os.ReadDir(a.cfg.WorkRoot)
	if err != nil || len(entries) != 0 || len(a.slots) != 0 {
		t.Fatal("worker/scratch leak", entries, err)
	}
	_ = os.Remove(block)
	r, e = a.defects(context.Background(), http.Header{}, object{"ontology_turtle": marshal("<urn:s> <urn:p> 1 .")})
	if !boolean(failurePayload(t, r, e), "complete") {
		t.Fatal("subsequent request failed", string(r))
	}
}

func TestClinicalDependencyErrorsStayErrors(t *testing.T) {
	a := &Adapter{}
	for _, name := range []string{"onto_crosswalk", "onto_enrich", "onto_validate_clinical"} {
		r := a.decorate(name, object{}, jsonTool(object{"error": marshal("Crosswalks not loaded: unavailable file")}))
		var envelope object
		_ = json.Unmarshal(r, &envelope)
		p := failurePayload(t, r, nil)
		if !boolean(envelope, "isError") || str(p, "error_code") != "dependency_unavailable" || str(p, "dependency") != "clinical_crosswalk" {
			t.Fatal(string(r))
		}
		var application object
		_ = json.Unmarshal(p["application_error"], &application)
		if str(application, "code") != "dependency_unavailable" {
			t.Fatal("application category was not retained", string(r))
		}
	}
	r := a.decorate("onto_crosswalk", object{}, jsonTool(object{"mappings": marshal([]any{})}))
	var envelope object
	_ = json.Unmarshal(r, &envelope)
	if boolean(envelope, "isError") {
		t.Fatal("valid empty mappings became an error")
	}
}

func TestApplicationErrorAliasPreservesUpstreamExtensions(t *testing.T) {
	a := &Adapter{}
	want := object{"code": marshal("upstream_extension"), "detail": marshal(7)}
	r := a.decorate("onto_defects", object{}, jsonTool(object{"error": marshal("failed"), "error_code": marshal("analysis_failed"), "application_error": marshal(want)}))
	p := failurePayload(t, r, nil)
	if string(p["application_error"]) != string(marshal(want)) {
		t.Fatal("upstream error extension overwritten", string(r))
	}
}

func TestCancelledDefectIsNotReportedAsDeadlineOrCleanResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &Adapter{}
	raw, err := a.defects(ctx, http.Header{}, object{"ontology_turtle": marshal("<urn:s> <urn:p> 1 .")})
	p := failurePayload(t, raw, err)
	if str(p, "error_code") != "analysis_cancelled" || boolean(p, "complete") {
		t.Fatal(string(raw))
	}
}
