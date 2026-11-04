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
		if name != "onto_ingest" && name != "onto_diff" && !scopedTools[name] {
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
	result["tools"] = marshal(tools)
	return marshal(result), nil
}
