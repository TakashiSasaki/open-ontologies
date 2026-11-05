// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWholeTermBindingRejectsFragments(t *testing.T) {
	b := map[string]string{"target": "<https://example.org/Org>"}
	for _, q := range []string{"ASK { {target} a <urn:C> }", "PREFIX ex: <urn:> ASK { <{target}> a ex:C }"} {
		got, e := bindTerms(q, b)
		if e != nil || strings.Contains(got, "<<") || !strings.Contains(got, b["target"]) {
			t.Fatal(got, e)
		}
	}
	for _, q := range []string{`ASK { <urn:prefix/{target}> a <urn:C> }`, `ASK { ?s ?p "{target}" }`, `ASK { ex:{target} ?p ?o }`, `ASK { {target}suffix ?p ?o }`, `ASK { {missing} ?p ?o }`} {
		if got, e := bindTerms(q, b); e == nil {
			t.Fatal("fragment accepted", got)
		}
	}
	got, e := normalizePrecondition("ASK { {target} a <urn:Class> . }", map[string]string{"target": "<{target}>"})
	if e != nil || !strings.Contains(got, "<"+rdf+"type>") {
		t.Fatal(got, e)
	}
	if queryKind("# comment\nPREFIX ex: <urn:> ASK { ?s ?p ?o }") != "ASK" {
		t.Fatal("query prolog lost")
	}
}

func TestNativeErrorsAndNegativeFindingsRemainDistinct(t *testing.T) {
	a := &Adapter{}
	for _, sample := range []struct {
		body   string
		failed bool
	}{{`{"error":"bad syntax","count":9007199254740993}`, true}, {`{"conforms":false,"ok":false,"violations":[{"error":"domain data"}]}`, false}, {`{"error":null,"ok":true}`, false}} {
		raw := json.RawMessage(`{"isError":false,"content":[{"type":"text","text":` + string(marshal(sample.body)) + `}],"_meta":{"counter":9007199254740993}}`)
		out := a.decorate("onto_query", object{}, raw)
		var r object
		_ = json.Unmarshal(out, &r)
		if boolean(r, "isError") != sample.failed || !strings.Contains(string(out), "9007199254740993") {
			t.Fatal(string(out))
		}
	}
}

func TestTransparentHTTPFailuresAndProtocolErrors(t *testing.T) {
	for _, status := range []int{400, 404, 429, 503} {
		r := &http.Response{StatusCode: status, Body: ioBody([]byte("original session failure")), Request: httptest.NewRequest("POST", "http://localhost/mcp", nil)}
		r.Request = r.Request.WithContext(context.WithValue(r.Request.Context(), callKey{}, callInfo{"onto_query", nil, marshal(1)}))
		a := &Adapter{}
		if e := a.modifyResponse(r); e != nil {
			t.Fatal(e)
		}
		b, _ := readBounded(r.Body, maxResponse)
		if string(b) != "original session failure" {
			t.Fatal(status, string(b))
		}
	}
	r := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: ioBody([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid params"}}`)), Request: httptest.NewRequest("POST", "http://localhost/mcp", nil)}
	r.Request = r.Request.WithContext(context.WithValue(r.Request.Context(), callKey{}, callInfo{"onto_query", nil, marshal(1)}))
	if e := (&Adapter{}).modifyResponse(r); e != nil {
		t.Fatal(e)
	}
	b, _ := readBounded(r.Body, maxResponse)
	if !strings.Contains(string(b), `"code":-32602`) {
		t.Fatal(string(b))
	}
}

func TestDryRunAndTemporalScopeDoNotInventSuccess(t *testing.T) {
	a := &Adapter{}
	out := a.decorate("onto_align", object{"dry_run": marshal(true)}, jsonTool(object{"auto_applied": marshal([]object{{"confidence": marshal(1.0), "coverage": marshal(0.32)}}), "borderline": marshal([]any{}), "summary_for_review": marshal("1 auto-applied"), "applied_count": marshal(0)}))
	p, e := payload(out)
	if e != nil {
		t.Fatal(e)
	}
	if string(p["auto_applied"]) != "[]" || !strings.Contains(str(p, "summary_for_review"), "0 applied") || !supplied(p, "auto_apply_candidates") {
		t.Fatal(string(out))
	}
	out = a.decorate("onto_temporal_query", object{}, jsonTool(object{"complete": marshal(true), "results": marshal([]any{})}))
	p, e = payload(out)
	if e != nil || !strings.Contains(string(p["scope"]), `"default_graph_included":false`) {
		t.Fatal(string(out), e)
	}
}

func TestActionNormalizationAndGoalsCannotInjectQueries(t *testing.T) {
	s := object{"name": marshal("assign"), "parameters": marshal([]object{{"name": marshal("target")}}), "preconditions": marshal([]string{"ASK { {target} a <urn:C> }"}), "effects": marshal([]object{{"kind": marshal("add_triple"), "subject": marshal("<{target}>"), "predicate": marshal("<urn:p>"), "object": marshal("urn:o")}})}
	n, e := normalizedAction(s)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(n["preconditions"]), rdf+"type") || strings.Contains(string(n["effects"]), "urn:p&gt;") {
		t.Fatal(string(marshal(n)))
	}
	for _, value := range []string{"urn:org", "<urn:org>"} {
		if _, e := actionBindings(s, marshal(map[string]string{"target": value})); e != nil {
			t.Fatal(e)
		}
	}
	for _, value := range []string{"not IRI", "<urn:x> } UNION { ?s ?p ?o"} {
		if _, e := actionBindings(s, marshal(map[string]string{"target": value})); e == nil {
			t.Fatal(value)
		}
	}
	for _, value := range []string{`<urn:o>`, `"hello"`, `"hello"@ja`, `"7"^^<http://www.w3.org/2001/XMLSchema#integer>`} {
		if _, e := goalSyntax(object{"goal_facts": marshal([][]string{{"<urn:s>", "<urn:p>", value}})}); e != nil {
			t.Fatal(value, e)
		}
	}
	for _, value := range []string{`<urn:o> } UNION { ?s ?p ?o`, `"x" . ?s ?p ?o`, `_:unbound`} {
		if _, e := goalSyntax(object{"goal_facts": marshal([][]string{{"<urn:s>", "<urn:p>", value}})}); e == nil {
			t.Fatal("goal injection accepted", value)
		}
	}
}

func TestIncompletePathsAreNotSilentlyChecked(t *testing.T) {
	idx := rdfIndex{"<urn:S>": {term(rdf + "type"): {term(sh + "NodeShape")}, term(sh + "targetNode"): {"<urn:node>"}, term(sh + "property"): {"_:p"}}, "_:p": {term(sh + "path"): {"_:cycle"}}, "_:cycle": {term(sh + "inversePath"): {"_:cycle"}}}
	_, shapes, issues := inspectShapes(idx)
	if len(shapes) != 2 || len(issues) == 0 {
		t.Fatal(shapes, issues)
	}
}

func TestNativeActionModePreservesEngineResults(t *testing.T) {
	for _, name := range []string{"onto_policy_check", "onto_action_applicable", "onto_plan_validate", "onto_plan_compile_pddl"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request message
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var params struct {
					Name      string `json:"name"`
					Arguments object `json:"arguments"`
				}
				_ = json.Unmarshal(request.Params, &params)
				if params.Name != name || str(params.Arguments, "value") != "\"SN-003\"" {
					t.Error(string(request.Params))
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(message{Version: "2.0", ID: request.ID, Result: jsonTool(object{"execution_ready": marshal(true), "native_marker": marshal(true)})})
			}))
			defer server.Close()
			a := &Adapter{cfg: Config{BackendURL: server.URL, NativeActionSemantics: true}, client: server.Client()}
			raw, err := a.auditCall(context.Background(), http.Header{}, name, object{"value": marshal("\"SN-003\"")})
			if err != nil {
				t.Fatal(err)
			}
			p, err := payload(raw)
			if err != nil || !boolean(p, "execution_ready") || !boolean(p, "native_marker") {
				t.Fatal(string(raw), err)
			}
		})
	}
}
