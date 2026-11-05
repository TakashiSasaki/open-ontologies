// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import "encoding/json"

// An operator takes this snapshot inside the selected container. Checking the
// adapter host's PATH would describe a different execution environment.
func (a *Adapter) capabilities() json.RawMessage {
	unknown := marshal(map[string]any{"inventory_status": "unknown", "note": "No readable container inventory; core status does not prove optional dependencies are installed or loaded.", "optional_modes": []string{"verified SHACL (oo-shacl)", "proof certification (oo-cert)", "classical planning (Fast Downward)", "external SMT/FOL (Z3/Vampire)", "clinical crosswalk data", "semantic search embedding model"}})
	if a.cfg.CapabilityFile == "" {
		return unknown
	}
	b, e := readLimited(a.cfg.CapabilityFile, maxInline)
	if e != nil {
		return unknown
	}
	var p object
	if json.Unmarshal(b, &p) != nil || str(p, "checked_at") == "" || str(p, "backend_image_id") == "" {
		return unknown
	}
	p["inventory_status"] = marshal("operator_snapshot")
	p["freshness"] = marshal("Point-in-time container inventory, not continuous monitoring; refresh after image/config/resource changes. Presence alone does not prove successful execution.")
	p["tool_mode_dependencies"] = marshal(map[string]any{
		"onto_shacl":          map[string]any{"builtin": "No external SHACL checker required", "verified": "verified_shacl"},
		"onto_reason":         map[string]any{"builtin": "Built-in reasoning remains available; engine fragment/budget limits still apply", "certificate_checking": "proof_certification"},
		"onto_plan_classical": []string{"classical_planning"},
		"onto_fol_model":      map[string]any{"z3": "external_smt", "other_solvers": "Not inventoried"},
		"onto_fol_prove":      map[string]any{"vampire": "external_fol", "other_provers": "Not inventoried"},
		"onto_crosswalk":      []string{"clinical_crosswalk"}, "onto_validate_clinical": []string{"clinical_crosswalk"}, "onto_search": []string{"semantic_search"},
	})
	return marshal(p)
}
