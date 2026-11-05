// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxDefectRows = 10000
const maxDefectBytes = 4 << 20

func defectFailure(code, reason string, scope object) json.RawMessage {
	return jsonTool(object{"error": marshal(reason), "error_code": marshal(code),
		"complete": marshal(false), "partial_results_available": marshal(false),
		"persistent_store_modified": marshal(false), "analysis_scope": marshal(scope),
		"hint": marshal("Select a smaller named graph with graph_iri, or supply a relevant ontology_turtle document. No complete global verdict was obtained.")})
}

// Native limit only caps displayed findings. Analyze a bounded dataset in a
// process we can cancel/reap, without ever loading the user's shared store.
func (a *Adapter) defects(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	scope := object{"kind": marshal("shared_snapshot_union"), "global_verdict": marshal(false)}
	fail := func(code, reason string) (json.RawMessage, error) { return defectFailure(code, reason, scope), nil }
	seconds := 15
	if supplied(args, "max_seconds") && (json.Unmarshal(args["max_seconds"], &seconds) != nil || seconds < 1 || seconds > 20) {
		return fail("invalid_arguments", "max_seconds must be an integer from 1 to 20")
	}
	if supplied(args, "limit") {
		var limit uint64
		if json.Unmarshal(args["limit"], &limit) != nil {
			return fail("invalid_arguments", "limit must be a nonnegative integer")
		}
	}
	if supplied(args, "ontology_turtle") && supplied(args, "graph_iri") {
		return fail("invalid_arguments", "provide ontology_turtle or graph_iri, not both")
	}
	if supplied(args, "certificate_dir") || supplied(args, "rules_file") {
		return fail("invalid_arguments", "request-local evaluation rejects certificate_dir and rules_file")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	interrupted := func(reason string) (json.RawMessage, error) {
		if ctx.Err() == context.Canceled {
			return fail("analysis_cancelled", "The analysis request was cancelled; no complete findings are available")
		}
		return fail("analysis_deadline_exceeded", reason)
	}
	var turtle string
	if supplied(args, "ontology_turtle") {
		scope["kind"] = marshal("inline_document")
		if json.Unmarshal(args["ontology_turtle"], &turtle) != nil {
			return fail("invalid_arguments", "ontology_turtle must be a string")
		}
		if len(turtle) > maxInline {
			return fail("input_too_large", "ontology_turtle exceeds 1 MiB")
		}
	} else {
		pattern := "{ ?s ?p ?o } UNION { GRAPH ?g { ?s ?p ?o } }"
		if supplied(args, "graph_iri") {
			iri, e := normalizeIRI(str(args, "graph_iri"))
			if e != nil {
				return fail("invalid_arguments", "graph_iri must be an absolute IRI, optionally enclosed in angle brackets")
			}
			pattern = "GRAPH " + iri + " { ?s ?p ?o }"
			scope["kind"], scope["graph_iri"] = marshal("named_graph_snapshot"), marshal(iri)
		}
		p, e := a.query(ctx, h, fmt.Sprintf("SELECT ?s ?p ?o WHERE { %s } LIMIT %d", pattern, maxDefectRows+1))
		if e != nil {
			if ctx.Err() != nil {
				return interrupted("The analysis deadline expired while capturing its dataset")
			}
			return fail("analysis_snapshot_failed", "The bounded analysis dataset could not be read; no complete result is available")
		}
		var rows []map[string]string
		if json.Unmarshal(p["results"], &rows) != nil || rows == nil {
			return fail("analysis_snapshot_failed", "The backend returned an invalid snapshot")
		}
		if len(rows) > maxDefectRows {
			return fail("analysis_scope_required", "The selected dataset exceeds 10,000 rows; choose a smaller explicit analysis scope")
		}
		var document strings.Builder
		for _, row := range rows {
			if row["s"] == "" || row["p"] == "" || row["o"] == "" {
				return fail("analysis_snapshot_failed", "The backend returned an incomplete RDF row")
			}
			if document.Len()+len(row["s"])+len(row["p"])+len(row["o"])+5 > maxDefectBytes {
				return fail("analysis_scope_required", "The selected dataset exceeds the 4 MiB analysis snapshot limit")
			}
			fmt.Fprintf(&document, "%s %s %s .\n", row["s"], row["p"], row["o"])
		}
		turtle = document.String()
		scope["selected_rows"] = marshal(len(rows))
		scope["snapshot_semantics"] = marshal("One SPARQL read flattened to a private dataset; later changes excluded. Named-graph scope excludes other graphs and their dependencies.")
	}
	workerArgs := object{}
	if supplied(args, "limit") {
		workerArgs["limit"] = args["limit"]
	}
	result, err := a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		if _, e := w.checked("onto_load", object{"turtle": marshal(turtle)}); e != nil {
			return nil, e
		}
		return w.call("onto_defects", workerArgs)
	})
	if ctx.Err() != nil {
		return interrupted("The analysis deadline expired; the private worker was stopped and no complete findings are available")
	}
	if err == nil {
		var p object
		p, err = payload(result)
		if err == nil {
			p["execution_scope"], p["persistent_store_modified"] = marshal("request"), marshal(false)
			p["complete"], p["completeness_scope"] = marshal(true), marshal("selected dataset only; not a global OWL consistency verdict")
			p["analysis_scope"], p["max_seconds"] = marshal(scope), marshal(seconds)
			return jsonTool(p), nil
		}
	}
	return fail("analysis_failed", err.Error())
}
