// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type callKey struct{}
type callInfo struct {
	name string
	args object
	id   json.RawMessage
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
