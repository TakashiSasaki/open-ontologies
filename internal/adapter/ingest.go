// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func checkWorkerHash(path, want string) error {
	if len(want) != 64 {
		return errors.New("worker_sha256 must pin the extracted upstream executable")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != want {
		return errors.New("upstream worker SHA-256 mismatch")
	}
	return nil
}

func (a *Adapter) ingest(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	inline, hasPath := supplied(args, "inline_data"), supplied(args, "path")
	if inline == hasPath {
		return nil, errors.New("provide exactly one of path and inline_data")
	}
	var content []byte
	var e error
	format := strings.ToLower(str(args, "format"))
	if inline {
		content = []byte(str(args, "inline_data"))
		if len(content) > maxInline {
			return nil, errors.New("inline_data exceeds 1 MiB")
		}
		if format == "" {
			return nil, errors.New("format is required with inline_data")
		}
	} else {
		content, e = a.readServerFile(str(args, "path"))
		if e != nil {
			return nil, e
		}
		if format == "" {
			format = strings.TrimPrefix(strings.ToLower(filepath.Ext(str(args, "path"))), ".")
		}
	}
	switch format {
	case "csv", "json", "ndjson", "xml", "yaml":
	case "jsonl":
		format = "ndjson"
	case "yml":
		format = "yaml"
	default:
		return nil, errors.New("private/inline ingest supports csv, json, ndjson, xml or yaml; use normal server paths for binary formats")
	}
	if supplied(args, "mapping") && !boolean(args, "inline_mapping") {
		mapping, e := a.readServerFile(str(args, "mapping"))
		if e != nil {
			return nil, e
		}
		args["mapping"] = marshal(string(mapping))
		args["inline_mapping"] = marshal(true)
	}
	dryRun, prov := boolean(args, "dry_run"), boolean(args, "provenance")
	sum := sha256.Sum256(content)
	source := "inline-" + hex.EncodeToString(sum[:])
	if !inline {
		source = str(args, "path")
	}
	return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		file := filepath.Join(w.dir, "input."+format)
		if e := os.WriteFile(file, content, 0o600); e != nil {
			return nil, e
		}
		workerArgs := clone(args)
		delete(workerArgs, "inline_data")
		delete(workerArgs, "dry_run")
		workerArgs["path"] = marshal(file)
		workerArgs["provenance"] = marshal(false)
		report, e := w.checked("onto_ingest", workerArgs)
		if e != nil {
			return nil, e
		}
		rdf, e := w.save("generated.nt")
		if e != nil {
			return nil, e
		}
		extraCount := 0
		if prov && rdf != "" {
			base := str(args, "base_iri")
			if base == "" {
				base = "http://example.org/data/"
			}
			sourceID := source
			if !inline {
				sourceID = filepath.Base(source)
			}
			// Keep the logical inline source stable; never publish the private worker's
			// filesystem path into a provenance label or IRI.
			var clean strings.Builder
			for _, r := range sourceID {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
					clean.WriteRune(r)
				} else {
					clean.WriteByte('_')
				}
			}
			iri := "<" + base + "source/" + clean.String() + ">"
			label := string(marshal(source))
			date := string(marshal(time.Now().UTC().Format(time.RFC3339Nano)))
			extra := fmt.Sprintf("%s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://www.w3.org/ns/prov#Entity> .\n%s <http://www.w3.org/2000/01/rdf-schema#label> %s .\n%s <http://www.w3.org/ns/prov#generatedAtTime> %s^^<http://www.w3.org/2001/XMLSchema#dateTime> .\n", iri, iri, label, iri, date)
			extraCount = 3
			seen := map[string]bool{}
			for _, line := range strings.Split(rdf, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && strings.HasPrefix(fields[0], "<") && !seen[fields[0]] {
					seen[fields[0]] = true
					extra += fmt.Sprintf("%s <http://www.w3.org/ns/prov#wasDerivedFrom> %s .\n", fields[0], iri)
					extraCount++
				}
			}
			if _, e = w.checked("onto_load", object{"turtle": marshal(extra)}); e != nil {
				return nil, e
			}
			rdf, e = w.save("with-provenance.nt")
			if e != nil {
				return nil, e
			}
		}
		generated := len(strings.Split(strings.TrimSpace(rdf), "\n"))
		if strings.TrimSpace(rdf) == "" {
			generated = 0
		}
		report["triples_generated"] = marshal(generated)
		report["dry_run"] = marshal(dryRun)
		report["provenance_triples"] = marshal(extraCount)
		if dryRun {
			report["triples_loaded"] = marshal(0)
		} else {
			raw, e := a.nativeTool(ctx, h, "onto_load", object{"turtle": marshal(rdf)})
			if e != nil {
				return nil, e
			}
			loaded, e := payload(raw)
			if e != nil {
				return nil, e
			}
			report["triples_loaded"] = loaded["triples_loaded"]
		}
		return jsonTool(report), nil
	})
}

func (a *Adapter) diff(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	var content [2]string
	for i, side := range []string{"old", "new"} {
		textKey, pathKey := side+"_content", side+"_path"
		if supplied(args, textKey) == supplied(args, pathKey) {
			return nil, fmt.Errorf("provide exactly one of %s and %s", textKey, pathKey)
		}
		if supplied(args, textKey) {
			content[i] = str(args, textKey)
			if len(content[i]) > maxInline {
				return nil, errors.New(textKey + " exceeds 1 MiB")
			}
		} else {
			// Read RDF through the backend's existing converter. Its own filesystem
			// parser preserves server file bases and accepts all upstream RDF formats.
			raw, e := a.nativeTool(ctx, h, "onto_convert", object{"path": args[pathKey], "to": marshal("turtle")})
			if e != nil {
				return nil, e
			}
			text, e := textContent(raw)
			if e != nil {
				return nil, e
			}
			var problem object
			if json.Unmarshal([]byte(text), &problem) == nil && supplied(problem, "error") {
				return nil, errors.New(str(problem, "error"))
			}
			content[i] = text
		}
	}
	return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		paths := [2]string{}
		for i, text := range content {
			if i > 0 {
				if _, e := w.checked("onto_clear", object{}); e != nil {
					return nil, e
				}
			}
			if _, e := w.checked("onto_load", object{"turtle": marshal(text)}); e != nil {
				return nil, e
			}
			path := filepath.Join(w.dir, fmt.Sprintf("diff-%d.nt", i))
			if _, e := w.checked("onto_save", object{"path": marshal(path), "format": marshal("ntriples")}); e != nil {
				return nil, e
			}
			paths[i] = path
		}
		return w.call("onto_diff", object{"old_path": marshal(paths[0]), "new_path": marshal(paths[1])})
	})
}
