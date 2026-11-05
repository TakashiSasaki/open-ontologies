// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"
)

var scopedTools = map[string]bool{"onto_reason": true, "onto_defects": true, "onto_dl_check": true, "onto_dl_explain": true, "onto_justify": true, "onto_shacl": true}

func normalizeIRI(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		s = s[1 : len(s)-1]
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || strings.ContainsAny(s, "<>\"{}|\\^`") {
		return "", errors.New("class IRI must be an absolute IRI; CURIE prefixes are not expanded")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", errors.New("invalid whitespace in class IRI")
		}
	}
	return "<" + s + ">", nil
}

func overlayCatalog(raw json.RawMessage) (json.RawMessage, error) {
	var result object
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	var tools []object
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		return nil, err
	}
	for _, tool := range tools {
		name := str(tool, "name")
		if name == "onto_action_info" {
			return nil, errors.New("upstream now supplies onto_action_info; review the adapter before upgrading")
		}
		switch name {
		case "onto_convert":
			tool["description"] = marshal("Convert server files or explicit inline RDF/OWL content using the pinned native engine. For inline mode supply inline_content, from (turtle, ntriples, rdfxml, nquads, trig, jsonld or ofn) and to; omit path/output. Returns content directly, preserves native OFN loss diagnostics, never modifies the shared store. Inline content is limited to 1 MiB and must use absolute non-file IRIs or explicit document bases; file IRIs are rejected to prevent scratch-path identities. Native graph flattening/inferred-graph rules still apply. Ordinary path/output behavior is unchanged.")
		case "onto_policy_check":
			tool["description"] = marshal("Advisory policy evaluation for 1–64 absolute target IRIs, with or without angle brackets. Whole RDF-term placeholders are bound safely. Query or IRI errors return indeterminate with isError=true. Deny overrides, otherwise allow only after every rule evaluates. This is not an atomic authorization guard for mutations.")
		case "onto_shacl_check":
			tool["description"] = marshal("Declaration/bound preflight for all SHACL NodeShape/PropertyShape references, independently of targetClass, targetNode, targetSubjectsOf or targetObjectsOf. Walks property paths; reports coverage limits explicitly. Does not evaluate data conformance or fully prove SHACL well-formedness. Server files must be inside configured readable data roots.")
		case "onto_action_applicable":
			tool["description"] = marshal("Advisory evaluation of a registered action's ASK/SELECT preconditions using complete absolute IRI bindings, with or without angle brackets. Invalid input or query errors are failures. Inspect onto_action_info for schema details and live-apply compatibility; live apply keeps native semantics.")
		case "onto_plan_validate":
			tool["description"] = marshal("Validate a deterministic IRI-effect plan in a private upstream worker. A point-in-time graph snapshot is flattened across graphs like native validation. Parameter/schema copies are normalized without rewriting shared actions. Reports goals separately. Not a policy/invariant authorization check or a concurrent transaction. Limits: 256 steps, 10,000 triples.")
		case "onto_plan_compile_pddl":
			tool["description"] = marshal("Emit exploratory PDDL from private normalized action copies and a default-graph snapshot (10,000 triples maximum). Expands predicate a to rdf:type for simple ASK triples. The native compiler is incomplete: execution_ready=false and execution_blockers explain why the output must not be run as a verified plan.")
		case "onto_temporal_query":
			tool["description"] = marshal("Query temporal NAMED graphs only using a SPARQL graph pattern and optional valid_at/as_of instants. The default graph is excluded. complete describes the selected temporal scope, not all stored triples; use onto_query for default-graph data.")
		case "onto_status":
			tool["description"] = marshal("Core MCP health and tool-surface status, plus the adapter identity and an operator snapshot of optional dependency readiness. status:ok does not mean every optional tool/mode is usable. Inventory timestamps and availability-versus-execution limits are explicit.")
		}
		if name != "onto_convert" && name != "onto_ingest" && name != "onto_diff" && !scopedTools[name] {
			continue
		}
		var schema object
		if err := json.Unmarshal(tool["inputSchema"], &schema); err != nil {
			return nil, err
		}
		var properties object
		if err := json.Unmarshal(schema["properties"], &properties); err != nil {
			return nil, err
		}
		if properties == nil {
			properties = object{}
		}
		stringField := func(description string) json.RawMessage {
			return marshal(map[string]any{"type": []string{"string", "null"}, "description": description})
		}
		removeRequired := func(names ...string) {
			var required []string
			_ = json.Unmarshal(schema["required"], &required)
			out := []string{}
			for _, r := range required {
				keep := true
				for _, n := range names {
					if r == n {
						keep = false
					}
				}
				if keep {
					out = append(out, r)
				}
			}
			if len(out) == 0 {
				delete(schema, "required")
			} else {
				schema["required"] = marshal(out)
			}
		}
		if scopedTools[name] {
			properties["ontology_turtle"] = stringField("Optional inline Turtle for a fresh request-local upstream process. Does not load or modify the shared store. At most 1 MiB. File-output options are rejected.")
		}
		switch name {
		case "onto_convert":
			properties["inline_content"] = stringField("Inline RDF/OWL text, at most 1 MiB. Supply inline_content OR path. No output file in inline mode; results return content directly. Requires from and to. File IRIs are rejected; relative IRIs require an explicit non-file document base.")
			properties["from"] = marshal(map[string]any{"type": []string{"string", "null"}, "enum": []any{"turtle", "ttl", "ntriples", "nt", "rdfxml", "rdf", "xml", "nquads", "nq", "trig", "jsonld", "ofn", nil}, "description": "Source format hint for inline_content only. Native content sniffing still applies."})
			properties["path"] = stringField("Source file in the server filesystem. Supply path OR inline_content. The client VM filesystem is not shared with the server.")
			removeRequired("path")
		case "onto_ingest":
			properties["inline_data"] = stringField("Inline CSV, JSON, NDJSON, XML or YAML, at most 1 MiB. Supply exactly one of path and inline_data; format is required for inline data.")
			properties["dry_run"] = marshal(map[string]any{"type": []string{"boolean", "null"}, "description": "Parse and map in a fresh upstream worker, without publishing to the shared store. Default false."})
			properties["path"] = stringField("Server-side file path. Supply path OR inline_data. Dry-run server paths must be inside configured readable data roots.")
			removeRequired("path")
		case "onto_diff":
			properties["old_path"] = stringField("Old server-side RDF file. Supply old_path OR old_content.")
			properties["new_path"] = stringField("New server-side RDF file. Supply new_path OR new_content.")
			properties["old_content"] = stringField("Inline old Turtle, at most 1 MiB. Supply old_path OR old_content. Relative IRIs require an explicit @base.")
			properties["new_content"] = stringField("Inline new Turtle, at most 1 MiB. Supply new_path OR new_content. Relative IRIs require an explicit @base.")
			removeRequired("old_path", "new_path")
		}
		schema["properties"] = marshal(properties)
		tool["inputSchema"] = marshal(schema)
	}
	tools = append(tools, object{"name": marshal("onto_action_info"), "description": marshal("Read registered action schema details (parameters, preconditions, effects, outcomes) and private-simulation compatibility diagnostics. Optional action_name selects one; omitted lists all (maximum 256). Reads SQLite in mode=ro without creating tables or modifying registries. Live apply retains native semantics."), "inputSchema": marshal(map[string]any{"type": "object", "properties": map[string]any{"action_name": map[string]any{"type": []string{"string", "null"}, "maxLength": 256}}, "additionalProperties": false})})
	result["tools"] = marshal(tools)
	return marshal(result), nil
}
