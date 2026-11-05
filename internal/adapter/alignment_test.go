// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func alignmentCandidate() object {
	return object{"source_iri": marshal("urn:source:A"), "target_iri": marshal("urn:target:B"),
		"relation": marshal("owl:equivalentClass"), "confidence": marshal(0.999), "requires_review": marshal(false),
		"applied": marshal(false), "evidence_coverage": marshal(0.6),
		"informative_signals": marshal([]string{"label_similarity", "parent_overlap", "property_overlap"})}
}

func TestAlignmentGateNeverPromotesNativeReviewOrUnknownEvidence(t *testing.T) {
	for _, change := range []object{
		{"evidence_coverage": marshal(0.2)},
		{"informative_signals": marshal([]string{"label_similarity"})},
		{"informative_signals": marshal([]string{"label_similarity", "label_similarity"})},
		{"requires_review": marshal(true)}, {"ambiguous": marshal(true)},
		{"evidence_coverage": marshal(nil)},
		{"informative_signals": marshal([]string{"label_similarity", "new_unknown_signal"})},
		{"source_iri": marshal("urn:bad> . <urn:inject>")}, {"relation": marshal("unknown")},
	} {
		row := alignmentCandidate()
		for key, value := range change {
			row[key] = value
		}
		triple, reasons := alignmentEligibility(row)
		if triple != "" || len(reasons) == 0 {
			t.Fatal("unsafe mapping approved", string(marshal(row)))
		}
	}
	if triple, reasons := alignmentEligibility(alignmentCandidate()); !strings.Contains(triple, "<urn:source:A>") || len(reasons) != 0 {
		t.Fatal(triple, reasons)
	}
}

func alignmentFixture(t *testing.T, candidate object, mode string) (*Adapter, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	evaluations, publishes := &atomic.Int32{}, &atomic.Int32{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m message
		_ = json.NewDecoder(r.Body).Decode(&m)
		var params struct {
			Name      string
			Arguments object
		}
		_ = json.Unmarshal(m.Params, &params)
		var result json.RawMessage
		switch params.Name {
		case "onto_align":
			evaluations.Add(1)
			if !boolean(params.Arguments, "dry_run") {
				t.Error("evaluation could auto-apply before gate")
			}
			count := 0
			if mode == "bad_dry_run" {
				count = 1
			}
			result = jsonTool(object{"candidates": marshal([]object{candidate}), "applied_count": marshal(count), "auto_applied": marshal([]object{candidate}), "borderline": marshal([]any{})})
		case "onto_load":
			publishes.Add(1)
			if supplied(params.Arguments, "path") || str(params.Arguments, "turtle") != "<urn:source:A> <http://www.w3.org/2002/07/owl#equivalentClass> <urn:target:B> .\n" {
				t.Error("wrong publication mode/terms", string(marshal(params.Arguments)))
			}
			if r.Header.Get("Idempotency-Key") != "" {
				t.Error("retry key forwarded")
			}
			if mode == "transport_failure" {
				http.Error(w, "not available", 503)
				return
			}
			if mode == "deny" {
				result = jsonTool(object{"error": marshal(`policy_denied: {"verdict":"deny"}`)})
			} else {
				result = jsonTool(object{"ok": marshal(true), "triples_loaded": marshal(1)})
			}
		default:
			t.Error("unexpected operation", params.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message{Version: "2.0", ID: m.ID, Result: result})
	}))
	t.Cleanup(backend.Close)
	return &Adapter{cfg: Config{BackendURL: backend.URL}, client: backend.Client()}, evaluations, publishes
}

func TestAlignmentEvidenceGatePrecedesWritesAndKeepsDryRunCandidates(t *testing.T) {
	for _, dry := range []bool{false, true} {
		low := alignmentCandidate()
		low["informative_signals"] = marshal([]string{"label_similarity"})
		low["evidence_coverage"] = marshal(0.2)
		a, evaluations, publishes := alignmentFixture(t, low, "")
		args := object{"source": marshal("source"), "dry_run": marshal(dry)}
		r, e := a.align(context.Background(), http.Header{}, args)
		p := failurePayload(t, a.decorate("onto_align", args, r), e)
		var borderline, eligible []object
		_ = json.Unmarshal(p["borderline"], &borderline)
		_ = json.Unmarshal(p["auto_apply_candidates"], &eligible)
		if evaluations.Load() != 1 || publishes.Load() != 0 || len(borderline) != 1 || len(eligible) != 0 || !boolean(borderline[0], "requires_review") || string(p["borderline_count"]) != "1" {
			t.Fatal(string(r), publishes.Load())
		}
		if string(borderline[0]["confidence"]) != string(low["confidence"]) {
			t.Fatal("native score changed")
		}
	}
	a, _, publishes := alignmentFixture(t, alignmentCandidate(), "")
	args := object{"source": marshal("source"), "dry_run": marshal(true)}
	r, e := a.align(context.Background(), http.Header{}, args)
	p := failurePayload(t, a.decorate("onto_align", args, r), e)
	var eligible []object
	_ = json.Unmarshal(p["auto_apply_candidates"], &eligible)
	if len(eligible) != 1 || publishes.Load() != 0 {
		t.Fatal("decorator erased dry-run eligibility", string(r))
	}
}

func TestAlignmentPublicationIsSingleGuardedCallWithHonestFailures(t *testing.T) {
	for _, mode := range []string{"success", "deny", "transport_failure", "bad_dry_run"} {
		t.Run(mode, func(t *testing.T) {
			a, evaluations, publishes := alignmentFixture(t, alignmentCandidate(), mode)
			args := object{"source": marshal("source"), "dry_run": marshal(false)}
			r, e := a.align(context.Background(), http.Header{"Idempotency-Key": []string{"do-not-replay"}}, args)
			r = a.decorate("onto_align", args, r)
			p := failurePayload(t, r, e)
			var envelope object
			_ = json.Unmarshal(r, &envelope)
			if evaluations.Load() != 1 || publishes.Load() > 1 || boolean(args, "dry_run") {
				t.Fatal("replay/caller mutation")
			}
			if mode == "success" {
				if publishes.Load() != 1 || boolean(envelope, "isError") || string(p["applied_count"]) != "1" {
					t.Fatal(string(r))
				}
			} else {
				if !boolean(envelope, "isError") {
					t.Fatal("failure reported success", string(r))
				}
				if mode == "transport_failure" && str(p, "execution_outcome") != "unknown" {
					t.Fatal(string(r))
				}
				if mode == "bad_dry_run" && publishes.Load() != 0 {
					t.Fatal("bad dry run was published")
				}
			}
		})
	}
}
