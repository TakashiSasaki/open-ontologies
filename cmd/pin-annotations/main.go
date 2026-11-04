// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// Pin an explicitly reviewed name list to an offline adapted tool catalog.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	catalog := flag.String("catalog", "", "JSON array from adapted tools/list")
	reviewed := flag.String("reviewed-tools", "", "JSON array of audited read-only names")
	flag.Parse()
	b, e := os.ReadFile(*catalog)
	if e != nil {
		return e
	}
	var tools []struct {
		Name        string          `json:"name"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if e = json.Unmarshal(b, &tools); e != nil {
		return e
	}
	b, e = os.ReadFile(*reviewed)
	if e != nil {
		return e
	}
	var names []string
	if e = json.Unmarshal(b, &names); e != nil {
		return e
	}
	schemas := map[string]json.RawMessage{}
	for _, t := range tools {
		schemas[t.Name] = t.InputSchema
	}
	policies := map[string]any{"*": map[string]any{"readOnlyHint": false}}
	for _, name := range names {
		raw, ok := schemas[name]
		if !ok {
			return fmt.Errorf("reviewed tool %q is absent", name)
		}
		var value any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if e = d.Decode(&value); e != nil {
			return e
		}
		canonical, e := json.Marshal(value)
		if e != nil {
			return e
		}
		h := sha256.Sum256(canonical)
		policies[name] = map[string]any{"readOnlyHint": true, "input_schema_sha256": []string{hex.EncodeToString(h[:])}}
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	return out.Encode(map[string]any{"tool_annotations": policies})
}
