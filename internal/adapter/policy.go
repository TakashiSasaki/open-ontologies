// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

var auditedTools = map[string]bool{"onto_policy_check": true, "onto_shacl_check": true, "onto_action_info": true, "onto_action_applicable": true, "onto_plan_validate": true, "onto_plan_compile_pddl": true}

func (a *Adapter) auditCall(ctx context.Context, h http.Header, name string, args object) (json.RawMessage, error) {
	if a.cfg.NativeActionSemantics && (name == "onto_action_applicable" || name == "onto_plan_validate" || name == "onto_plan_compile_pddl" || name == "onto_policy_check") {
		return a.nativeTool(ctx, h, name, args)
	}
	switch name {
	case "onto_policy_check":
		return a.policyCheck(ctx, h, args)
	case "onto_shacl_check":
		return a.shaclCheck(ctx, h, args)
	case "onto_action_info", "onto_action_applicable", "onto_plan_validate", "onto_plan_compile_pddl":
		return a.actionCall(ctx, h, name, args)
	}
	return nil, errors.New("unknown adapted tool")
}
func indeterminate(reason string, results []object) json.RawMessage {
	r := jsonTool(object{"verdict": marshal("indeterminate"), "error": marshal(reason), "rule_results": marshal(results), "authorization_enforced": marshal(false), "evaluation_scope": marshal("advisory point-in-time check; no atomic mutation guard")})
	var o object
	_ = json.Unmarshal(r, &o)
	o["isError"] = marshal(true)
	return marshal(o)
}
func (a *Adapter) query(ctx context.Context, h http.Header, q string) (object, error) {
	r, e := a.nativeTool(ctx, h, "onto_query", object{"query": marshal(q)})
	if e != nil {
		return nil, e
	}
	return payload(r)
}
func (a *Adapter) policyCheck(ctx context.Context, h http.Header, args object) (json.RawMessage, error) {
	var targets []string
	if json.Unmarshal(args["target_iris"], &targets) != nil || len(targets) == 0 || len(targets) > 64 {
		return indeterminate("target_iris must contain 1–64 absolute IRIs", nil), nil
	}
	for i, t := range targets {
		value, e := normalizeIRI(t)
		if e != nil {
			return indeterminate("invalid target IRI; no allow verdict was issued", nil), nil
		}
		targets[i] = value
	}
	r, e := a.readRegistry(ctx)
	if e != nil {
		return indeterminate(e.Error(), nil), nil
	}
	if len(r.Policies)*len(targets) > 512 {
		return indeterminate("policy evaluation exceeds 512 rule/target pairs", nil), nil
	}
	results := []object{}
	denied := false
	for _, rule := range r.Policies {
		effect := str(rule, "effect")
		if effect != "allow" && effect != "deny" {
			return indeterminate("invalid policy effect", results), nil
		}
		for _, target := range targets {
			q, err := bindTerms(str(rule, "condition"), map[string]string{"target": target})
			if err != nil {
				return indeterminate(err.Error(), results), nil
			}
			if queryKind(q) != "ASK" {
				q = "ASK { " + q + " }"
			}
			p, err := a.query(ctx, h, q)
			if err != nil {
				return indeterminate("policy query failed for rule "+str(rule, "name")+": "+err.Error(), results), nil
			}
			var fired bool
			if json.Unmarshal(p["result"], &fired) != nil {
				return indeterminate("policy did not produce an ASK boolean", results), nil
			}
			if fired && effect == "deny" {
				denied = true
			}
			results = append(results, object{"rule_name": rule["name"], "target": marshal(target), "effect": rule["effect"], "fired": marshal(fired)})
		}
	}
	verdict := "allow"
	if denied {
		verdict = "deny"
	}
	return jsonTool(object{"verdict": marshal(verdict), "rule_results": marshal(results), "policy_semantics": marshal("deny overrides; default allow when all rules evaluate successfully"), "authorization_enforced": marshal(false), "evaluation_scope": marshal("advisory; registry snapshot and graph queries are not a shared write transaction")}), nil
}
