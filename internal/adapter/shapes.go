// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const rdf = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
const sh = "http://www.w3.org/ns/shacl#"

type shapeRef struct{ shape, kind, value string }
type rdfIndex map[string]map[string][]string

func term(iri string) string { return "<" + iri + ">" }
func bare(s string) string   { return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">") }
func indexRows(p object) (rdfIndex, error) {
	var rows []map[string]string
	if json.Unmarshal(p["results"], &rows) != nil {
		return nil, errors.New("RDF inspection returned no rows")
	}
	idx := rdfIndex{}
	for _, r := range rows {
		s, p, o := r["s"], r["p"], r["o"]
		if s == "" || p == "" || o == "" {
			return nil, errors.New("incomplete RDF inspection row")
		}
		if idx[s] == nil {
			idx[s] = map[string][]string{}
		}
		idx[s][p] = append(idx[s][p], o)
	}
	return idx, nil
}

func inspectShapes(idx rdfIndex) ([]shapeRef, []object, []object) {
	shapes := map[string]bool{}
	var refs []shapeRef
	issues := []object{}
	for s, ps := range idx {
		for _, t := range ps[term(rdf+"type")] {
			if t == term(sh+"NodeShape") || t == term(sh+"PropertyShape") {
				shapes[s] = true
			}
		}
		for p := range ps {
			switch bare(p) {
			case sh + "targetClass", sh + "targetNode", sh + "targetSubjectsOf", sh + "targetObjectsOf", sh + "path", sh + "class", sh + "datatype", sh + "property", sh + "node", sh + "minCount", sh + "maxCount", sh + "and", sh + "or", sh + "xone", sh + "not", sh + "qualifiedValueShape":
				shapes[s] = true
			}
		}
	}
	// Referenced shapes can be untyped and contain no constraints at all.
	// Count those nodes too; list/path nodes themselves are not shapes.
	for _, ps := range idx {
		for _, p := range []string{"property", "node", "not", "qualifiedValueShape"} {
			for _, v := range ps[term(sh+p)] {
				if strings.HasPrefix(v, "<") || strings.HasPrefix(v, "_:") {
					shapes[v] = true
				}
			}
		}
		for _, p := range []string{"and", "or", "xone"} {
			for _, head := range ps[term(sh+p)] {
				seen := map[string]bool{}
				for head != term(rdf+"nil") && !seen[head] && len(seen) < 256 {
					seen[head] = true
					list := idx[head]
					for _, v := range list[term(rdf+"first")] {
						if strings.HasPrefix(v, "<") || strings.HasPrefix(v, "_:") {
							shapes[v] = true
						}
					}
					rest := list[term(rdf+"rest")]
					if len(rest) != 1 {
						break
					}
					head = rest[0]
				}
				if head != term(rdf+"nil") {
					issues = append(issues, object{"shape": marshal(bare(head)), "kind": marshal("incomplete_shape_discovery"), "message": marshal("Cyclic, malformed or over-limit logical shape list; discovery is incomplete")})
				}
			}
		}
	}
	add := func(s, k, v string) { refs = append(refs, shapeRef{s, k, bare(v)}) }
	bad := func(s, v, why string) {
		issues = append(issues, object{"shape": marshal(bare(s)), "kind": marshal("incomplete_reference_check"), "value": marshal(v), "message": marshal(why)})
	}
	var walkPath func(string, string, map[string]bool, int)
	walkPath = func(s, v string, seen map[string]bool, depth int) {
		if strings.HasPrefix(v, "<") {
			add(s, "missing_path", v)
			return
		}
		if !strings.HasPrefix(v, "_:") || seen[v] || depth > 32 {
			bad(s, v, "unsupported, cyclic or over-depth SHACL property path")
			return
		}
		seen[v] = true
		defer delete(seen, v)
		ps := idx[v]
		recognized := 0
		for _, p := range []string{"inversePath", "alternativePath", "zeroOrMorePath", "oneOrMorePath", "zeroOrOnePath"} {
			for _, x := range ps[term(sh+p)] {
				recognized++
				walkPath(s, x, seen, depth+1)
			}
		}
		if first := ps[term(rdf+"first")]; len(first) > 0 {
			recognized++
			if len(first) != 1 || len(ps[term(rdf+"rest")]) != 1 {
				bad(s, v, "malformed SHACL path list")
				return
			}
			walkPath(s, first[0], seen, depth+1)
			rest := ps[term(rdf+"rest")][0]
			if rest != term(rdf+"nil") {
				walkPath(s, rest, seen, depth+1)
			}
		}
		if recognized == 0 {
			bad(s, v, "unrecognised SHACL path node")
		}
	}
	for s, ps := range idx {
		for _, v := range ps[term(sh+"targetClass")] {
			add(s, "missing_target_class", v)
		}
		for _, v := range ps[term(sh+"class")] {
			add(s, "missing_class_constraint", v)
		}
		for _, v := range ps[term(sh+"path")] {
			walkPath(s, v, map[string]bool{}, 0)
		}
		for _, k := range []string{"targetSubjectsOf", "targetObjectsOf"} {
			for _, v := range ps[term(sh+k)] {
				add(s, "missing_target_property", v)
			}
		}
		for _, v := range ps[term(sh+"datatype")] {
			if !strings.HasPrefix(bare(v), "http://www.w3.org/2001/XMLSchema#") {
				issues = append(issues, object{"shape": marshal(bare(s)), "kind": marshal("unrecognised_datatype"), "value": marshal(bare(v)), "message": marshal("Only the XSD namespace is recognised by this lightweight datatype preflight")})
			}
		}
		if len(ps[term(sh+"target")]) > 0 {
			bad(s, "sh:target", "custom SHACL targets are not evaluated by this reference preflight")
		}
	}
	reports := []object{}
	names := make([]string, 0, len(shapes))
	for s := range shapes {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		targets := object{}
		for _, k := range []string{"targetClass", "targetNode", "targetSubjectsOf", "targetObjectsOf"} {
			if v := idx[s][term(sh+k)]; len(v) > 0 {
				targets[k] = marshal(v)
			}
		}
		reports = append(reports, object{"shape_iri": marshal(bare(s)), "targets": marshal(targets)})
	}
	sort.Slice(refs, func(i, j int) bool { a, b := refs[i], refs[j]; return a.shape+a.kind+a.value < b.shape+b.kind+b.value })
	return refs, reports, issues
}

func (a *Adapter) shaclCheck(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	turtle := str(args, "shapes")
	if !boolean(args, "inline") {
		b, e := a.readServerFile(turtle)
		if e != nil {
			return nil, e
		}
		turtle = string(b)
	}
	if len(turtle) > maxInline {
		return nil, errors.New("SHACL shapes exceed 1 MiB")
	}
	raw, e := a.nativeTool(ctx, h, "onto_shacl_check", object{"shapes": marshal(turtle), "inline": marshal(true)})
	if e != nil {
		return nil, e
	}
	base, e := payload(raw)
	if e != nil {
		return raw, nil
	}
	if !boolean(base, "parses") {
		return raw, nil
	}
	return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		if _, e := w.checked("onto_load", object{"turtle": marshal(turtle)}); e != nil {
			return nil, e
		}
		rows, e := w.checked("onto_query", object{"query": marshal("SELECT ?s ?p ?o WHERE { ?s ?p ?o }")})
		if e != nil {
			return nil, e
		}
		idx, e := indexRows(rows)
		if e != nil {
			return nil, e
		}
		refs, reports, extra := inspectShapes(idx)
		var issues []object
		_ = json.Unmarshal(base["issues"], &issues)
		kept := []object{}
		for _, issue := range issues {
			k := str(issue, "kind")
			if k != "missing_target_class" && k != "missing_path" && k != "missing_class_constraint" && k != "unrecognised_datatype" {
				kept = append(kept, issue)
			}
		}
		kept = append(kept, extra...)
		probes := map[string]bool{}
		var probe strings.Builder
		for _, r := range refs {
			iri, e := normalizeIRI(r.value)
			if e != nil {
				return nil, errors.New("shape reference is not an absolute IRI")
			}
			kind := "class"
			if r.kind == "missing_path" || r.kind == "missing_target_property" {
				kind = "property"
			}
			key := kind + ":" + iri
			if probes[key] {
				continue
			}
			probes[key] = true
			if kind == "class" {
				fmt.Fprintf(&probe, "<urn:adapter:probe:%d> a <%sNodeShape> ; <%stargetClass> %s .\n", len(probes), sh, sh, iri)
			} else {
				fmt.Fprintf(&probe, "<urn:adapter:probe:%d> a <%sNodeShape> ; <%stargetClass> <urn:adapter:probe:unused> ; <%sproperty> [ <%spath> %s ] .\n", len(probes), sh, sh, sh, sh, iri)
			}
		}
		missing := map[string]bool{}
		if len(probes) > 0 {
			p, e := a.nativeTool(ctx, h, "onto_shacl_check", object{"shapes": marshal(probe.String()), "inline": marshal(true)})
			if e != nil {
				return nil, e
			}
			pobj, e := payload(p)
			if e != nil {
				return nil, e
			}
			var errs []object
			_ = json.Unmarshal(pobj["issues"], &errs)
			for _, issue := range errs {
				k := str(issue, "kind")
				if k == "missing_target_class" {
					missing["class:"+term(str(issue, "value"))] = true
				}
				if k == "missing_path" {
					missing["property:"+term(str(issue, "value"))] = true
				}
			}
		}
		seen := map[string]bool{}
		for _, r := range refs {
			kind := "class"
			if r.kind == "missing_path" || r.kind == "missing_target_property" {
				kind = "property"
			}
			key := r.shape + "\x00" + r.kind + "\x00" + r.value
			if !missing[kind+":"+term(r.value)] || seen[key] {
				continue
			}
			seen[key] = true
			kept = append(kept, object{"shape": marshal(bare(r.shape)), "kind": marshal(r.kind), "value": marshal(r.value), "message": marshal("Referenced IRI is not declared as an ontology " + kind + " in the default or named graphs")})
		}
		base["upstream_shape_count"] = base["shape_count"]
		base["upstream_shapes"] = base["shapes"]
		base["shapes"] = marshal(reports)
		base["shape_count"] = marshal(len(reports))
		base["issues"] = marshal(kept)
		base["issue_count"] = marshal(len(kept))
		base["ok"] = marshal(len(kept) == 0)
		base["reference_check_complete"] = marshal(len(extra) == 0)
		base["check_scope"] = marshal("All shape references, independent of target form; declaration and bound preflight only. Does not evaluate data conformance, target reachability, or full SHACL well-formedness.")
		return jsonTool(base), nil
	})
}
