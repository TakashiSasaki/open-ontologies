// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestInlineConversionValidationAndOrdinaryPathRouting(t *testing.T) {
	if needsAdaptation("onto_convert", object{"path": marshal("/data/example.ttl"), "to": marshal("turtle"), "output": marshal("/data/out.ttl")}) {
		t.Fatal("ordinary path calls must retain native behavior")
	}
	a := &Adapter{}
	for _, args := range []object{
		{"inline_content": marshal(""), "path": marshal("/data/a.ttl"), "from": marshal("turtle"), "to": marshal("turtle")},
		{"inline_content": marshal(""), "from": marshal("turtle"), "to": marshal("turtle"), "output": marshal("/data/out")},
		{"inline_content": marshal(strings.Repeat("x", maxInline+1)), "from": marshal("turtle"), "to": marshal("turtle")},
		{"inline_content": marshal(true), "from": marshal("turtle"), "to": marshal("turtle")},
		{"inline_content": marshal(""), "to": marshal("turtle")},
		{"inline_content": marshal(""), "from": marshal("../../etc/passwd"), "to": marshal("turtle")},
		{"inline_content": marshal(""), "from": marshal("turtle")},
		{"path": marshal("/data/a.ttl"), "from": marshal("turtle"), "to": marshal("turtle")},
	} {
		if !needsAdaptation("onto_convert", args) {
			t.Fatal("inline-only fields must be validated")
		}
		if _, err := a.convert(context.Background(), args); err == nil {
			t.Fatal("invalid inline options reached worker/backend", args)
		}
	}
}

func TestInlineIRIInspectionDoesNotConfuseLiteralsAndIRIs(t *testing.T) {
	for _, value := range []string{
		`<urn:s> <urn:p> "see file:///data/input.ttl" .`,
		`<urn:s> <urn:p> "escaped quote: \" and file:" .`,
		`<https://e/s> <urn:p> _:b <https://e/g> .`,
	} {
		if err := checkInlineIRIs(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{
		`<file:///run/private/input#s> <urn:p> "v" .`,
		`<urn:s> <urn:p> <file:///etc/example> .`,
		`<urn:s> <urn:p> "v"^^<file:///data/type> .`,
		`<urn:s> <urn:p> "v" <file:///data/graph> .`,
	} {
		if err := checkInlineIRIs(value); err == nil {
			t.Fatal("file IRI accepted", value)
		}
	}
}

func TestConversionPayloadKeepsJSONLDTextAndOFNDiagnostics(t *testing.T) {
	text := `{"@context":{},"content":"original"}`
	p, e := conversionPayload("jsonld", text)
	if e != nil || str(p, "content") != text {
		t.Fatal("JSON-LD was unpacked as a report", p, e)
	}
	report := `{"format":"ofn","content":"Ontology()","constructs_not_exported":[{"predicate":"urn:p","count":9007199254740993}]}`
	p, e = conversionPayload("functional", report)
	if e != nil || !strings.Contains(string(p["constructs_not_exported"]), "9007199254740993") {
		t.Fatal("OFN warnings lost/rounded", p, e)
	}
	if _, e = conversionPayload("ofn", `{"content":"invalid envelope"}`); e == nil {
		t.Fatal("incomplete OFN report accepted")
	}
}

func TestInlineConvertCatalogKeepsOutputAndSupportsDirectInput(t *testing.T) {
	raw, err := overlayCatalog(marshal(map[string]any{"tools": []object{{"name": marshal("onto_convert"), "inputSchema": marshal(map[string]any{
		"type": "object", "properties": object{"path": marshal(map[string]any{"type": "string"}), "to": marshal(map[string]any{"type": "string"}), "output": marshal(map[string]any{"type": []string{"string", "null"}})}, "required": []string{"path", "to"},
	})}}}))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Tools []object }
	_ = json.Unmarshal(raw, &catalog)
	var schema struct {
		Properties object
		Required   []string
	}
	_ = json.Unmarshal(catalog.Tools[0]["inputSchema"], &schema)
	if len(schema.Required) != 1 || schema.Required[0] != "to" || !supplied(schema.Properties, "inline_content") || !supplied(schema.Properties, "from") || !supplied(schema.Properties, "output") {
		t.Fatal(string(raw))
	}
}

func TestLargeNativePolicyDenialCountsAndMetadataArePreserved(t *testing.T) {
	rows := []map[string]any{}
	for i := range 2000 {
		for _, rule := range []string{"deny_org", "other"} {
			rows = append(rows, map[string]any{"rule_name": rule, "target": fmt.Sprintf("<https://example.org/target/%d>", i), "effect": "deny", "fired": i == 7})
		}
	}
	original := "policy_denied: " + string(marshal(map[string]any{"verdict": "deny", "rule_results": rows}))
	p := object{"error": marshal(original), "counter": json.RawMessage(`9007199254740993`)}
	a := &Adapter{}
	raw := marshal(object{"content": marshal([]object{{"type": marshal("text"), "text": marshal(string(marshal(p))), "annotations": marshal(map[string]any{"audience": []string{"user"}})}}), "structuredContent": marshal(p), "_meta": marshal(map[string]any{"retained": true})})
	result := a.decorate("onto_clear", object{}, raw)
	if len(result) > 8192 || !strings.Contains(string(result), "9007199254740993") {
		t.Fatal("summary too large or rounded metadata", len(result))
	}
	var envelope object
	_ = json.Unmarshal(result, &envelope)
	if !boolean(envelope, "isError") || !supplied(envelope, "_meta") {
		t.Fatal("error/metadata lost")
	}
	var blocks []object
	_ = json.Unmarshal(envelope["content"], &blocks)
	if !supplied(blocks[0], "annotations") {
		t.Fatal("content annotations lost")
	}
	var payload object
	_ = json.Unmarshal([]byte(str(blocks[0], "text")), &payload)
	var summary object
	_ = json.Unmarshal(payload["policy_summary"], &summary)
	if string(summary["evaluated_targets"]) != "2000" || string(summary["blocked_targets"]) != "1" || string(summary["rule_evaluations"]) != "4000" || !boolean(summary, "details_truncated") {
		t.Fatal(string(payload["policy_summary"]))
	}
	digest := sha256.Sum256([]byte(original))
	if str(summary, "original_error_sha256") != hex.EncodeToString(digest[:]) || string(envelope["structuredContent"]) != string(marshal(payload)) {
		t.Fatal("digest/structuredContent mismatch")
	}
	if supplied(payload, "persistent_store_modified") {
		t.Fatal("must not invent a full rollback claim")
	}
}

func TestPolicySummaryRequiresCompleteNativeContract(t *testing.T) {
	for _, value := range []string{
		"policy_denied: malformed " + strings.Repeat("x", 9000),
		"policy_denied: " + string(marshal(map[string]any{"verdict": "deny", "rule_results": []any{map[string]any{"target": strings.Repeat("x", 9000)}}})),
		"policy_denied: " + string(marshal(map[string]any{"verdict": "allow", "rule_results": strings.Repeat("x", 9000)})),
		"other error: " + strings.Repeat("x", 9000),
		"policy_denied: small report",
	} {
		p := object{"error": marshal(value)}
		if summarizePolicyDenial(p) || str(p, "error") != value {
			t.Fatal("nonmatching error changed")
		}
	}
	text := strings.Repeat("日本語", 300)
	if !utf8.ValidString(preview(text, 512)) || len(preview(text, 512)) > 515 {
		t.Fatal("invalid bounded UTF-8 preview")
	}
	a := &Adapter{}
	negative := jsonTool(object{"verdict": marshal("deny"), "rule_results": marshal([]any{})})
	if string(a.decorate("onto_policy_check", object{}, negative)) != string(negative) {
		t.Fatal("advisory deny verdict became an execution error")
	}
}

func TestMissingRepoConfigurationGivesGuidanceWithoutFallback(t *testing.T) {
	a := &Adapter{}
	raw := a.decorate("onto_repo_list", object{}, jsonTool(object{"error": marshal("no ontology_dirs configured; set [general] ontology_dirs in config.toml or OPEN_ONTOLOGIES_ONTOLOGY_DIRS")}))
	if !strings.Contains(string(raw), "repository_not_configured") || !strings.Contains(string(raw), "onto_cache_list") {
		t.Fatal(string(raw))
	}
	var p object
	_ = json.Unmarshal(raw, &p)
	if !boolean(p, "isError") {
		t.Fatal("missing config must not look like a successful exhaustive search")
	}
}
