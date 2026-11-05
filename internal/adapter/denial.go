// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

const policyDetailThreshold = 8 << 10

// Only the native structured rejection contract is summarized. Query errors,
// ordinary negative findings, HTTP/JSON-RPC failures and small errors stay intact.
// No mutation is retried, no guard is disabled, no full-query success is inferred.
func summarizePolicyDenial(p object) bool {
	original := str(p, "error")
	if len(original) <= policyDetailThreshold || !strings.HasPrefix(original, "policy_denied: ") {
		return false
	}
	var report struct {
		Verdict string `json:"verdict"`
		Results []struct {
			Rule   string `json:"rule_name"`
			Target string `json:"target"`
			Effect string `json:"effect"`
			Fired  *bool  `json:"fired"`
		} `json:"rule_results"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(original, "policy_denied: ")), &report) != nil || report.Verdict != "deny" || len(report.Results) == 0 {
		return false
	}
	targets, blocked := map[string]bool{}, map[string]bool{}
	examples := []map[string]any{}
	for _, result := range report.Results {
		if result.Target == "" || result.Rule == "" || result.Fired == nil || (result.Effect != "allow" && result.Effect != "deny") {
			return false
		}
		targets[result.Target] = true
		if result.Effect == "deny" && *result.Fired {
			if !blocked[result.Target] && len(examples) < 3 {
				examples = append(examples, map[string]any{
					"rule_name": preview(result.Rule, 128), "target": preview(result.Target, 512),
					"rule_name_truncated": len(result.Rule) > 128, "target_truncated": len(result.Target) > 512,
					"effect": "deny", "fired": true,
				})
			}
			blocked[result.Target] = true
		}
	}
	if len(blocked) == 0 {
		return false
	}
	digest := sha256.Sum256([]byte(original))
	hash := hex.EncodeToString(digest[:])
	p["error"] = marshal(fmt.Sprintf("policy_denied: native mutation guard rejected the transaction; %d of %d evaluated targets blocked (%d rule evaluations). Details summarized.", len(blocked), len(targets), len(report.Results)))
	p["error_code"] = marshal("policy_denied")
	p["verdict"] = marshal("deny")
	p["policy_summary"] = marshal(map[string]any{
		"evaluated_targets": len(targets), "blocked_targets": len(blocked), "rule_evaluations": len(report.Results),
		"representative_denials": examples, "omitted_rule_results": len(report.Results) - len(examples),
		"details_truncated": true, "original_error_bytes": len(original), "original_error_sha256": hash,
		"scope": "native policy report; does not claim rollback of every higher-level side effect",
	})
	slog.Info("summarized native policy denial", "targets", len(targets), "blocked", len(blocked), "rule_evaluations", len(report.Results), "original_error_bytes", len(original), "original_error_sha256", hash)
	return true
}

func preview(s string, bytes int) string {
	if len(s) <= bytes {
		return s
	}
	s = s[:bytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
