// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type callKey struct{}
type callInfo struct {
	name    string
	args    object
	id      json.RawMessage
	started time.Time
}

func (a *Adapter) proxyFailure(w http.ResponseWriter, r *http.Request, err error) {
	if c, ok := r.Context().Value(callKey{}).(callInfo); ok {
		err = observe(codeBackendTransport, stageBackendRoundTrip, err)
		var detail *observedError
		if errors.As(err, &detail) && detail.code == codeBackendLimit {
			result := a.decorate(c.name, c.args, jsonTool(object{
				"error":      marshal("The backend response exceeded the 10 MiB adapter limit. Execution outcome is unknown; do not automatically retry mutations."),
				"error_code": marshal("backend_output_limit"), "execution_outcome": marshal("unknown"),
				"retry_safe": marshal(false), "partial_results_available": marshal(false),
			}))
			a.logDiagnosticCall("MCP upstream response failed", r.Context(), c.name, c.args, c.started, result, result, err)
			a.respond(w, c.id, result)
			return
		}
		a.logDiagnosticCall("MCP upstream response failed", r.Context(), c.name, c.args, c.started, nil, nil, err)
	}
	// Unclassified transport/session failures retain their HTTP contract.
	// No error path retries a request or raises the response cap.
	http.Error(w, "upstream connection failed", http.StatusBadGateway)
}

// Keep HTTP session failures and JSON-RPC errors intact. Only successful
// tools/call responses contain application results that can be interpreted.
func (a *Adapter) modifyResponse(r *http.Response) error {
	c, ok := r.Request.Context().Value(callKey{}).(callInfo)
	if !ok || r.StatusCode < 200 || r.StatusCode >= 300 || r.StatusCode == 202 {
		return nil
	}
	m, err := responseMessage(r.Body, r.Header.Get("Content-Type"), c.id)
	_ = r.Body.Close()
	if err != nil {
		return err
	}
	if len(m.Result) > 0 {
		m.Result = a.decorate(c.name, c.args, m.Result)
	}
	raw := marshal(m)
	r.Body = ioBody(raw)
	r.ContentLength = int64(len(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	r.Header.Del("Content-Encoding")
	r.Header.Del("ETag")
	return nil
}

// Preserve content blocks and extensions, including exact JSON number values.
// A valid negative finding (conforms:false, ok:false, valid:false) is not a
// failed tool execution. Only the native top-level error contract is promoted.
func (a *Adapter) decorate(name string, args object, raw json.RawMessage) json.RawMessage {
	var result object
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return raw
	}
	var blocks []object
	if json.Unmarshal(result["content"], &blocks) != nil || len(blocks) != 1 || str(blocks[0], "type") != "text" {
		return raw
	}
	var p object
	if json.Unmarshal([]byte(str(blocks[0], "text")), &p) != nil || p == nil {
		return raw
	}
	if supplied(p, "error") && string(p["error"]) != `""` {
		result["isError"] = marshal(true)
		changed := summarizePolicyDenial(p)
		if (name == "onto_crosswalk" || name == "onto_enrich" || name == "onto_validate_clinical") && strings.HasPrefix(str(p, "error"), "Crosswalks not loaded:") {
			p["error_code"], p["dependency"] = marshal("dependency_unavailable"), marshal("clinical_crosswalk")
			p["configuration_required"] = marshal(true)
			p["hint"] = marshal("Provision and verify the clinical mapping Parquet inside the backend container at the path expected by this engine. Missing or unreadable data is not an empty mapping result; sample mappings are not validated clinical guidance.")
			changed = true
		}
		if name == "onto_repo_list" && str(p, "error") == "no ontology_dirs configured; set [general] ontology_dirs in config.toml or OPEN_ONTOLOGIES_ONTOLOGY_DIRS" {
			p["error_code"] = marshal("repository_not_configured")
			p["configuration_required"] = marshal(true)
			p["hint"] = marshal("Configure [general] ontology_dirs or OPEN_ONTOLOGIES_ONTOLOGY_DIRS inside the backend container (for example a managed /data/ontologies directory). Compilation cache is not a source repository and is not searched automatically. Use onto_cache_list for compiled-cache entries.")
			changed = true
		}
		if changed {
			blocks[0]["text"] = marshal(string(marshal(p)))
			result["content"] = marshal(blocks)
			if supplied(result, "structuredContent") {
				result["structuredContent"] = marshal(p)
			}
		}
		return marshal(result)
	}
	if boolean(result, "isError") {
		return raw
	}
	changed := false
	switch name {
	case "onto_align":
		p["dry_run"] = marshal(boolean(args, "dry_run"))
		p["confidence_semantics"] = marshal("Similarity/fusion score, not a calibrated probability of semantic equivalence. Review evidence coverage independently.")
		if boolean(args, "dry_run") {
			var candidates, borderline []json.RawMessage
			_ = json.Unmarshal(p["auto_applied"], &candidates)
			_ = json.Unmarshal(p["borderline"], &borderline)
			p["auto_apply_candidates"] = p["auto_applied"]
			p["auto_applied"] = marshal([]any{})
			p["applied_count"] = marshal(0)
			p["summary_for_review"] = marshal(fmt.Sprintf("Dry run: %d auto-apply candidates, %d borderline candidates; 0 applied. Scores do not establish semantic equivalence.", len(candidates), len(borderline)))
		}
		changed = true
	case "onto_temporal_query":
		p["scope"] = marshal(map[string]any{"kind": "temporal_named_graphs", "default_graph_included": false, "complete_refers_to": "selected temporal named graphs only", "note": "An empty result does not establish absence from the default graph. Use onto_query for default-graph data."})
		changed = true
	case "onto_status":
		p["native_tools"] = p["tools"]
		p["tools"] = marshal(125)
		p["adapter_version"] = marshal(a.Version)
		p["status_scope"] = marshal("Core MCP service health; does not imply every optional tool/mode is ready.")
		p["capabilities"] = a.capabilities()
		changed = true
	}
	if !changed {
		return raw
	}
	blocks[0]["text"] = marshal(string(marshal(p)))
	result["content"] = marshal(blocks)
	if supplied(result, "structuredContent") {
		result["structuredContent"] = marshal(p)
	}
	return marshal(result)
}
