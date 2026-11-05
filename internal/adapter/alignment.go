// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

var alignmentPredicates = map[string]string{
	"owl:equivalentClass": "http://www.w3.org/2002/07/owl#equivalentClass",
	"skos:exactMatch":     "http://www.w3.org/2004/02/skos/core#exactMatch",
	"skos:closeMatch":     "http://www.w3.org/2004/02/skos/core#closeMatch",
	"skos:relatedMatch":   "http://www.w3.org/2004/02/skos/core#relatedMatch",
	"rdfs:subClassOf":     "http://www.w3.org/2000/01/rdf-schema#subClassOf",
}

var alignmentSignals = map[string]bool{
	"label_similarity": true, "property_overlap": true, "parent_overlap": true,
	"instance_overlap": true, "restriction_similarity": true,
	"neighborhood_similarity": true, "embedding_similarity": true,
}

func alignmentFailure(code, reason string, unknown bool) json.RawMessage {
	p := object{"error": marshal(reason), "error_code": marshal(code), "retry_safe": marshal(false)}
	if unknown {
		p["execution_outcome"] = marshal("unknown")
	}
	return jsonTool(p)
}

// Native confidence/cardinality/ambiguity stay authoritative. The adapter can
// only add review requirements, never promote a native review row.
func alignmentEligibility(row object) (string, []string) {
	reasons := []string{}
	var review *bool
	_ = json.Unmarshal(row["requires_review"], &review)
	if review == nil || *review {
		reasons = append(reasons, "native_requires_review")
	}
	if boolean(row, "ambiguous") {
		reasons = append(reasons, "native_ambiguity")
	}
	var coverage *float64
	if json.Unmarshal(row["evidence_coverage"], &coverage) != nil || coverage == nil || *coverage < 0.4 || *coverage > 1 {
		reasons = append(reasons, "insufficient_evidence_coverage")
	}
	var signals []string
	_ = json.Unmarshal(row["informative_signals"], &signals)
	known := map[string]bool{}
	unknown := false
	for _, signal := range signals {
		if alignmentSignals[signal] {
			known[signal] = true
		} else {
			unknown = true
		}
	}
	if len(known) < 2 {
		reasons = append(reasons, "insufficient_independent_signal_types")
	}
	if unknown {
		reasons = append(reasons, "unreviewed_signal_schema")
	}
	source, e1 := normalizeIRI(str(row, "source_iri"))
	target, e2 := normalizeIRI(str(row, "target_iri"))
	predicate, ok := alignmentPredicates[str(row, "relation")]
	if e1 != nil || e2 != nil || !ok {
		reasons = append(reasons, "unsupported_mapping_terms")
	}
	if len(reasons) != 0 {
		return "", reasons
	}
	return fmt.Sprintf("%s <%s> %s .\n", source, predicate, target), reasons
}

func (a *Adapter) align(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	dryRun := false
	if supplied(args, "dry_run") && json.Unmarshal(args["dry_run"], &dryRun) != nil {
		return alignmentFailure("invalid_arguments", "dry_run must be a boolean", false), nil
	}
	evaluation := clone(args)
	evaluation["dry_run"] = marshal(true)
	raw, err := a.nativeTool(ctx, h, "onto_align", evaluation)
	if err != nil {
		return alignmentFailure("alignment_evaluation_failed", err.Error(), false), nil
	}
	if resultFailure(raw) != nil {
		return raw, nil
	}
	p, err := payload(raw)
	if err != nil {
		return alignmentFailure("alignment_evaluation_failed", err.Error(), false), nil
	}
	var candidates []object
	if !supplied(p, "candidates") || json.Unmarshal(p["candidates"], &candidates) != nil || len(candidates) > 10000 {
		return alignmentFailure("alignment_contract_error", "Native alignment returned no bounded candidate array; no publication attempted", false), nil
	}
	var applied int
	if !supplied(p, "applied_count") || json.Unmarshal(p["applied_count"], &applied) != nil || applied != 0 {
		return alignmentFailure("alignment_contract_error", "The backend did not confirm dry-run evaluation; no further publication attempted", true), nil
	}
	eligible, borderline := []object{}, []object{}
	var publication strings.Builder
	demoted := 0
	for _, row := range candidates {
		var nativeApplied *bool
		_ = json.Unmarshal(row["applied"], &nativeApplied)
		if row == nil || nativeApplied == nil || *nativeApplied {
			return alignmentFailure("alignment_contract_error", "The backend reported an invalid/already-applied dry-run candidate; no further publication attempted", true), nil
		}
		triple, reasons := alignmentEligibility(row)
		if len(reasons) > 0 {
			if !boolean(row, "requires_review") {
				demoted++
			}
			row["requires_review"] = marshal(true)
			row["adapter_review_reasons"] = marshal(reasons)
			row["review_context_note"] = marshal("Inspect source/target definitions and structural evidence; native context may be absent for a newly demoted row.")
			borderline = append(borderline, row)
		} else {
			if !dryRun && publication.Len()+len(triple) > maxInline {
				return alignmentFailure("alignment_publication_too_large", "Eligible mappings exceed the 1 MiB publication bound; no publication attempted", false), nil
			}
			if !dryRun {
				publication.WriteString(triple)
			}
			eligible = append(eligible, row)
		}
	}
	if !dryRun && len(eligible) > 0 {
		// One native inline load is an additive, policy-guarded transaction.
		// It bypasses the file registry and does not replace the shared dataset.
		published, e := a.nativeTool(ctx, h, "onto_load", object{"turtle": marshal(publication.String())})
		if e != nil {
			return alignmentFailure("alignment_publication_outcome_unknown", e.Error(), true), nil
		}
		if resultFailure(published) != nil {
			return published, nil
		}
		confirmation, e := payload(published)
		if e != nil || !boolean(confirmation, "ok") {
			return alignmentFailure("alignment_publication_outcome_unknown", "The backend did not confirm mapping publication; do not retry automatically", true), nil
		}
		p["publication_result"] = marshal(confirmation)
		for _, row := range eligible {
			row["applied"] = marshal(true)
		}
		applied = len(eligible)
	}
	p["candidates"], p["borderline"] = marshal(candidates), marshal(borderline)
	p["borderline_count"] = marshal(len(borderline))
	p["auto_apply_candidates"] = marshal(eligible)
	p["auto_applied"] = marshal([]object{})
	if !dryRun {
		p["auto_applied"] = marshal(eligible)
	}
	p["applied_count"], p["dry_run"] = marshal(applied), marshal(dryRun)
	p["evidence_policy"] = marshal(map[string]any{
		"minimum_coverage": 0.4, "minimum_signal_types": 2, "demoted_count": demoted,
		"coverage_measurement":   "native reported evidence_coverage (rounded by the engine)",
		"native_scores_retained": true, "native_review_decisions_only_strengthened": true,
		"evaluation_dry_run": true, "publication": "single native guarded inline transaction when requested",
		"scope": "post-selection evidence gate; does not recover candidates removed by native cardinality selection; native evaluation can record lineage",
	})
	p["summary_for_review"] = marshal(fmt.Sprintf("%d eligible mappings, %d requiring review, %d applied. dry_run=%t. Similarity scores do not establish semantic equivalence.", len(eligible), len(borderline), applied, dryRun))
	return jsonTool(p), nil
}
