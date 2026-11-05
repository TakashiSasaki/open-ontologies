// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var sourceExtensions = map[string]string{
	"turtle": "ttl", "ttl": "ttl", "ntriples": "nt", "nt": "nt",
	"rdfxml": "rdf", "rdf": "rdf", "xml": "rdf", "nquads": "nq", "nq": "nq",
	"trig": "trig", "jsonld": "jsonld", "ofn": "ofn",
}

func (a *Adapter) convert(ctx context.Context, args object) (json.RawMessage, error) {
	if !supplied(args, "inline_content") || supplied(args, "path") {
		return nil, errors.New("provide exactly one of path and inline_content; from is only for inline content")
	}
	if supplied(args, "output") {
		return nil, errors.New("inline conversion returns content directly and does not accept output paths")
	}
	var content string
	if json.Unmarshal(args["inline_content"], &content) != nil {
		return nil, errors.New("inline_content must be a string")
	}
	if len(content) > maxInline {
		return nil, errors.New("inline_content exceeds 1 MiB")
	}
	format := strings.ToLower(str(args, "from"))
	ext, ok := sourceExtensions[format]
	if !ok {
		return nil, errors.New("from is required for inline conversion: turtle, ntriples, rdfxml, nquads, trig, jsonld or ofn")
	}
	if strings.TrimSpace(str(args, "to")) == "" {
		return nil, errors.New("to is required")
	}
	return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		file := filepath.Join(w.dir, "input."+ext)
		if e := os.WriteFile(file, []byte(content), 0o600); e != nil {
			return nil, errors.New("cannot stage inline conversion input")
		}
		call := func(to string) (string, error) {
			raw, e := w.call("onto_convert", object{"path": marshal(file), "to": marshal(to)})
			if e != nil {
				return "", e
			}
			text, e := textContent(raw)
			if e != nil {
				return "", e
			}
			var problem object
			if json.Unmarshal([]byte(text), &problem) == nil && supplied(problem, "error") {
				// Scratch paths are implementation details even on parse failures.
				return "", errors.New(strings.ReplaceAll(str(problem, "error"), w.dir, "[inline input]"))
			}
			return text, nil
		}
		// Inspect the parsed dataset, not the source text: this catches relative
		// RDF/XML/JSON-LD/Turtle bases, datatype IRIs and graph names without
		// mistaking a literal containing "file:" for an IRI. Do not rewrite IDs.
		nquads, e := call("nquads")
		if e != nil {
			return nil, e
		}
		if e := checkInlineIRIs(nquads); e != nil {
			return nil, e
		}
		to := str(args, "to")
		text := nquads
		if to != "nquads" && to != "nq" {
			text, e = call(to)
			if e != nil {
				return nil, e
			}
		}
		p, e := conversionPayload(to, text)
		if e != nil {
			return nil, e
		}
		// OFN's original constructs_not_exported and round-trip diagnostics stay
		// in the result; a successful conversion is not a losslessness claim.
		p["source_format_hint"] = marshal(format)
		p["execution_scope"] = marshal("request")
		p["persistent_store_modified"] = marshal(false)
		return jsonTool(p), nil
	})
}

func conversionPayload(to, text string) (object, error) {
	if strings.EqualFold(to, "ofn") || strings.EqualFold(to, "functional") {
		var p object
		if json.Unmarshal([]byte(text), &p) != nil || str(p, "format") != "ofn" || !supplied(p, "content") || !supplied(p, "constructs_not_exported") {
			return nil, errors.New("native OFN converter returned an unexpected report")
		}
		return p, nil
	}
	// A JSON-LD document may itself have a content key. It is RDF text, not
	// the converter's report envelope, so never unpack it as an OFN report.
	return object{"content": marshal(text), "format": marshal(to)}, nil
}

func checkInlineIRIs(nquads string) error {
	terms, e := tokens(nquads)
	if e != nil {
		return errors.New("cannot inspect converted RDF terms")
	}
	for _, token := range terms {
		if token.kind == "iri" && strings.HasPrefix(strings.ToLower(token.text), "<file:") {
			return errors.New("inline conversion does not accept file IRIs; use absolute non-file IRIs or an explicit @base, xml:base or JSON-LD @base; use path mode for intentional file IRIs")
		}
	}
	return nil
}
