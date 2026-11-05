// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

func parameterTerms(s object) (map[string]string, error) {
	var ps []object
	if json.Unmarshal(s["parameters"], &ps) != nil {
		return nil, errors.New("invalid action parameters")
	}
	terms := map[string]string{}
	for _, p := range ps {
		n := str(p, "name")
		if !identifier(n) || terms[n] != "" {
			return nil, errors.New("invalid or duplicate action parameter")
		}
		terms[n] = "<{" + n + "}>"
	}
	return terms, nil
}
func actionBindings(s object, raw json.RawMessage) (map[string]string, error) {
	params, e := parameterTerms(s)
	if e != nil {
		return nil, e
	}
	var input map[string]string
	if len(raw) == 0 {
		raw = marshal(map[string]string{})
	}
	if json.Unmarshal(raw, &input) != nil || len(input) != len(params) {
		return nil, errors.New("bindings must contain exactly the declared action parameters")
	}
	bound := map[string]string{}
	for n := range params {
		v, ok := input[n]
		if !ok {
			return nil, errors.New("missing action binding: " + n)
		}
		iri, e := normalizeIRI(v)
		if e != nil {
			return nil, errors.New("action adapter currently supports absolute IRI bindings only; native literal effects are not reliable")
		}
		bound[n] = iri
	}
	return bound, nil
}
func normalizedAction(s object) (object, error) {
	out := clone(s)
	params, e := parameterTerms(s)
	if e != nil {
		return nil, e
	}
	var pre []string
	if json.Unmarshal(s["preconditions"], &pre) != nil {
		return nil, errors.New("invalid action preconditions")
	}
	for i, q := range pre {
		pre[i], e = normalizePrecondition(q, params)
		if e != nil {
			return nil, e
		}
	}
	out["preconditions"] = marshal(pre)
	var outcomes []json.RawMessage
	_ = json.Unmarshal(s["outcomes"], &outcomes)
	if len(outcomes) > 0 {
		return nil, errors.New("stochastic actions require native outcome-aware validation; this adapter only simulates deterministic IRI effects")
	}
	var effects []object
	if json.Unmarshal(s["effects"], &effects) != nil {
		return nil, errors.New("invalid action effects")
	}
	for _, effect := range effects {
		keys := []string{"subject", "predicate", "object"}
		switch str(effect, "kind") {
		case "add_class":
			keys = []string{"iri"}
		case "add_triple", "remove_triple":
		default:
			return nil, errors.New("unsupported action effect")
		}
		for _, key := range keys {
			v := str(effect, key)
			ts, _ := significant(v)
			if len(ts) == 1 && placeholder(ts[0]) != "" {
				n := placeholder(ts[0])
				if params[n] == "" {
					return nil, errors.New("undeclared effect parameter")
				}
				effect[key] = marshal("{" + n + "}")
			} else {
				iri, e := normalizeIRI(v)
				if e != nil {
					return nil, errors.New("effect must be a whole IRI or whole parameter; literal/partial templates require a native fix")
				}
				effect[key] = marshal(bare(iri))
			}
		}
	}
	out["effects"] = marshal(effects)
	return out, nil
}

func liveBindingCompatibility(s object) bool {
	n, e := normalizedAction(s)
	if e != nil {
		return false
	}
	// Only term structure matters; canonicalizing predicate a is not required
	// by the native SPARQL parser. Probe with bare IRIs as live effects expect.
	params, e := parameterTerms(s)
	if e != nil {
		return false
	}
	for k := range params {
		params[k] = "urn:adapter:binding-probe:" + k
	}
	var original, normalized []string
	_ = json.Unmarshal(s["preconditions"], &original)
	_ = json.Unmarshal(n["preconditions"], &normalized)
	for i, q := range original {
		// Binding through the safe RDF-term path must match native raw string
		// substitution with bare IRIs. Otherwise native preflight disagrees.
		safe := map[string]string{}
		for k, v := range params {
			safe[k] = term(v)
		}
		bound, err := bindTerms(q, safe)
		if err != nil {
			return false
		}
		raw := q
		for k, v := range params {
			raw = strings.ReplaceAll(raw, "{"+k+"}", v)
		}
		if bound != raw || i >= len(normalized) {
			return false
		}
	}
	var effects []object
	_ = json.Unmarshal(s["effects"], &effects)
	for _, effect := range effects {
		for _, key := range []string{"subject", "predicate", "object", "iri"} {
			if supplied(effect, key) && strings.Contains(str(effect, key), "<") {
				return false
			}
		}
	}
	return true
}

func (a *Adapter) actionCall(ctx context.Context, h http.Header, name string, args object) (json.RawMessage, error) {
	r, e := a.readRegistry(ctx)
	if e != nil {
		return nil, e
	}
	if name == "onto_action_info" {
		all := r.Actions
		if n := str(args, "action_name"); n != "" {
			s, e := findAction(r, n)
			if e != nil {
				return nil, e
			}
			all = []object{s}
		}
		entries := []object{}
		for _, s := range all {
			if a.cfg.NativeActionSemantics {
				entries = append(entries, object{"schema": marshal(s), "native_term_binding": marshal(true), "binding_diagnostic": marshal("Native whole-term IRI/N-Triples literal binding; partial templates require migration. Live mutations enforce native policies and preconditions transactionally.")})
				continue
			}
			_, err := normalizedAction(s)
			diagnostic := "Whole-term IRI bindings can be normalized in a private simulation; live apply retains native semantics."
			if err != nil {
				diagnostic = err.Error()
			}
			entries = append(entries, object{"schema": marshal(s), "private_simulation_supported": marshal(err == nil), "live_iri_binding_compatible": marshal(liveBindingCompatibility(s)), "binding_diagnostic": marshal(diagnostic)})
		}
		return jsonTool(object{"actions": marshal(entries), "count": marshal(len(entries)), "read_only_registry_snapshot": marshal(true), "live_execution_semantics": marshal("Applicability remains advisory; execution semantics are owned by the selected native backend. No shared schema rewrite.")}), nil
	}
	if name == "onto_action_applicable" {
		s, e := findAction(r, str(args, "action_name"))
		if e != nil {
			return nil, e
		}
		bindings, e := actionBindings(s, args["bindings"])
		if e != nil {
			return nil, e
		}
		var pre []string
		if json.Unmarshal(s["preconditions"], &pre) != nil {
			return nil, errors.New("invalid precondition list")
		}
		applicable := true
		results := []object{}
		for i, q := range pre {
			q, e = bindTerms(q, bindings)
			if e != nil {
				return nil, e
			}
			kind := queryKind(q)
			if kind != "ASK" && kind != "SELECT" {
				return nil, errors.New("action precondition must be ASK or SELECT")
			}
			p, e := a.query(ctx, h, q)
			if e != nil {
				return nil, fmt.Errorf("precondition %d could not be evaluated: %w", i, e)
			}
			fired := false
			if kind == "ASK" {
				if json.Unmarshal(p["result"], &fired) != nil {
					return nil, errors.New("precondition did not return an ASK result")
				}
			} else {
				var rows []object
				if json.Unmarshal(p["results"], &rows) != nil {
					return nil, errors.New("precondition did not return SELECT rows")
				}
				fired = len(rows) > 0
			}
			applicable = applicable && fired
			results = append(results, object{"index": marshal(i), "satisfied": marshal(fired)})
		}
		return jsonTool(object{"applicable": marshal(applicable), "action_name": s["name"], "bindings": marshal(bindings), "preconditions_evaluated": marshal(len(pre)), "preconditions": marshal(results), "live_iri_binding_compatible": marshal(liveBindingCompatibility(s)), "evaluation_scope": marshal("advisory point-in-time graph reads; live apply uses the unchanged native action schema; use onto_action_info to inspect incompatibilities")}), nil
	}
	// Match each native operation's graph scope explicitly: the native plan
	// validator flattens all graphs, while the PDDL compiler reads the default.
	query := "SELECT ?s ?p ?o WHERE { ?s ?p ?o } LIMIT 10001"
	scope := "request_snapshot_of_default_graph"
	if name == "onto_plan_validate" {
		query = "SELECT ?s ?p ?o WHERE { { ?s ?p ?o } UNION { GRAPH ?g { ?s ?p ?o } } } LIMIT 10001"
		scope = "request_snapshot_of_all_graphs_flattened_like_native"
	}
	p, e := a.query(ctx, h, query)
	if e != nil {
		return nil, e
	}
	var rows []map[string]string
	if json.Unmarshal(p["results"], &rows) != nil || len(rows) > 10000 {
		return nil, errors.New("planning snapshot exceeds 10,000 triples")
	}
	var nt strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&nt, "%s %s %s .\n", row["s"], row["p"], row["o"])
	}
	if nt.Len() > maxResponse {
		return nil, errors.New("planning snapshot exceeds output limit")
	}
	selected := map[string]bool{}
	var steps []object
	if name == "onto_plan_validate" {
		if json.Unmarshal(args["steps"], &steps) != nil || len(steps) > 256 {
			return nil, errors.New("steps must be an array of at most 256 steps")
		}
		for _, step := range steps {
			n := str(step, "action_name")
			s, e := findAction(r, n)
			if e != nil {
				return nil, e
			}
			bound, e := actionBindings(s, step["bindings"])
			if e != nil {
				return nil, e
			}
			for k, v := range bound {
				bound[k] = bare(v)
			}
			step["bindings"] = marshal(bound)
			selected[n] = true
		}
		args["steps"] = marshal(steps)
		// Goal positions are sent to the actual upstream RDF parser first.
		var goals [][]string
		if supplied(args, "goal_facts") && json.Unmarshal(args["goal_facts"], &goals) != nil {
			return nil, errors.New("invalid goal_facts")
		}
		for _, goal := range goals {
			if len(goal) != 3 {
				return nil, errors.New("goal_facts require three RDF terms")
			}
		}
	} else {
		var names []string
		if supplied(args, "action_names") && json.Unmarshal(args["action_names"], &names) != nil {
			return nil, errors.New("invalid action_names")
		}
		if len(names) == 0 {
			for _, s := range r.Actions {
				names = append(names, str(s, "name"))
			}
		}
		for _, n := range names {
			if _, e := findAction(r, n); e != nil {
				return nil, e
			}
			selected[n] = true
		}
	}
	schemas := []object{}
	for _, s := range r.Actions {
		if selected[str(s, "name")] {
			normalized, e := normalizedAction(s)
			if e != nil {
				return nil, e
			}
			schemas = append(schemas, normalized)
		}
	}
	return a.withWorker(ctx, func(w *worker) (json.RawMessage, error) {
		if name == "onto_plan_validate" {
			goalNT, e := goalSyntax(args)
			if e != nil {
				return nil, e
			}
			if goalNT != "" {
				if _, e := w.checked("onto_validate", object{"input": marshal(goalNT), "inline": marshal(true)}); e != nil {
					return nil, e
				}
			}
		}
		if _, e := w.checked("onto_load", object{"turtle": marshal(nt.String())}); e != nil {
			return nil, e
		}
		for _, s := range schemas {
			if _, e := w.checked("onto_action_register", object{"schema_json": marshal(string(marshal(s)))}); e != nil {
				return nil, e
			}
		}
		result, e := w.call(name, args)
		if e != nil {
			return nil, e
		}
		body, e := payload(result)
		if e != nil {
			return result, nil
		}
		body["execution_scope"] = marshal(scope)
		body["persistent_store_modified"] = marshal(false)
		body["simulation_semantics"] = marshal("Whole IRI terms normalized in private schema copies; live schemas are unchanged. This is not a policy/invariant authorization check or a concurrent transaction.")
		if name == "onto_plan_compile_pddl" {
			decoratePDDL(body, schemas, rows)
		}
		return jsonTool(body), nil
	})
}

func goalSyntax(args object) (string, error) {
	var goals [][]string
	_ = json.Unmarshal(args["goal_facts"], &goals)
	if goals == nil {
		goals = [][]string{}
	}
	if len(goals) > 256 {
		return "", errors.New("at most 256 goal facts are supported")
	}
	var nt strings.Builder
	for _, goal := range goals {
		if len(goal) != 3 {
			return "", errors.New("goal_facts require exactly three RDF terms")
		}
		for i, v := range goal {
			if i < 2 || strings.HasPrefix(strings.TrimSpace(v), "<") {
				iri, e := normalizeIRI(v)
				if e != nil {
					return "", e
				}
				goal[i] = iri
				continue
			}
			ts, e := significant(v)
			if e != nil || len(ts) == 0 || ts[0].kind != "literal" {
				return "", errors.New("goal object must be one IRI or N-Triples literal")
			}
			valid := len(ts) == 1 || (len(ts) == 2 && regexp.MustCompile(`^@[A-Za-z]+(?:-[A-Za-z0-9]+)*$`).MatchString(ts[1].text))
			if len(ts) == 3 && ts[1].text == "^^" && ts[2].kind == "iri" {
				_, e := normalizeIRI(ts[2].text)
				valid = e == nil
			}
			if !valid {
				return "", errors.New("goal object must be a single RDF term, not a SPARQL fragment")
			}
		}
		fmt.Fprintf(&nt, "%s %s %s .\n", goal[0], goal[1], goal[2])
	}
	if nt.Len() > maxInline {
		return "", errors.New("goal facts exceed 1 MiB")
	}
	args["goal_facts"] = marshal(goals)
	return nt.String(), nil
}

func decoratePDDL(body object, schemas []object, rows []map[string]string) {
	var notes []string
	_ = json.Unmarshal(body["translation_notes"], &notes)
	reasons := append([]string{}, notes...)
	for _, row := range rows {
		for _, k := range []string{"s", "p", "o"} {
			if !strings.HasPrefix(row[k], "<") {
				reasons = append(reasons, "Initial state contains literals/blank nodes; the native compiler only declares type iri")
				break
			}
		}
	}
	for _, s := range schemas {
		var pre []string
		_ = json.Unmarshal(s["preconditions"], &pre)
		for _, q := range pre {
			if _, ok := simpleAsk(q); !ok {
				reasons = append(reasons, "Only single-triple ASK preconditions can be translated faithfully")
			}
		}
	}
	// The upstream stub omits domain constant declarations and an invertible
	// RDF↔PDDL symbol map. Do not claim executability even if its notes are empty.
	reasons = append(reasons, "Native PDDL stub has no verified constant declarations or collision-free reverse symbol map; external planner execution has not been qualified")
	sort.Strings(reasons)
	unique := []string{}
	for _, r := range reasons {
		if len(unique) == 0 || r != unique[len(unique)-1] {
			unique = append(unique, r)
		}
	}
	body["translation_complete"] = marshal(false)
	body["execution_ready"] = marshal(false)
	body["execution_blockers"] = marshal(unique)
	body["normalizations"] = marshal([]string{"Whole IRI parameter placeholders canonicalized in private schema copies", "SPARQL predicate a expanded to rdf:type for single-triple ASK"})
}
