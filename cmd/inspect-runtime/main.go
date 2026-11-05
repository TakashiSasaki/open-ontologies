// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// Operator-only container inventory. Run inside the exact selected image;
// never grant the web adapter access to the Docker socket to collect it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

type item struct {
	State string `json:"state"`
	Scope string `json:"scope"`
	Note  string `json:"note"`
}

func binary(env string, names []string, lean bool) item {
	r := item{"missing", "default container executable resolution", "No executable found in the inspected default locations. Explicit per-call paths were not inspected."}
	if path := os.Getenv(env); env != "" && path != "" {
		if st, e := os.Stat(path); e == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return item{"present_unverified", "container environment override", "Executable file found; identity, behavior and successful execution have not been verified."}
		}
		return item{"missing", "container environment override", "Configured executable is absent or not executable. No fallback assumed."}
	}
	for _, name := range names {
		if lean {
			bases := []string{"/build/lean/.lake/build/bin", "lean/.lake/build/bin"}
			if env == "OO_SHACL" {
				bases = bases[:1]
			}
			for _, base := range bases {
				if st, e := os.Stat(filepath.Join(base, name)); e == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
					return item{"present_unverified", "image build-tree location", "Found a file; no proof or execution verified."}
				}
			}
		}
		if _, e := exec.LookPath(name); e == nil && env != "OO_SHACL" {
			return item{"present_unverified", "container PATH", "Found an executable; successful execution has not been verified."}
		}
	}
	return r
}
func main() {
	image := flag.String("image-id", "", "Exact backend Docker image SHA-256")
	removeSelf := flag.Bool("remove-self", false, "Remove this temporary inspection executable after use")
	flag.Parse()
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(*image) {
		fmt.Fprintln(os.Stderr, "a full --image-id is required")
		os.Exit(2)
	}
	out := map[string]any{"schema_version": 1, "checked_at": time.Now().UTC().Format(time.RFC3339), "backend_image_id": *image, "native_version": "2.1.0", "execution_environment": "inside selected native container; default dependency discovery only", "optional_modes": map[string]item{
		"verified_shacl":      binary("OO_SHACL", []string{"oo-shacl"}, true),
		"proof_certification": binary("OO_CERT", []string{"oo-cert"}, true),
		"classical_planning":  binary("FAST_DOWNWARD_BIN", []string{"fast-downward.py"}, false),
		"external_smt":        binary("", []string{"z3"}, false),
		"external_fol":        binary("", []string{"vampire"}, false),
		"clinical_crosswalk":  {State: "unknown", Scope: "application configuration", Note: "Requires a configured readable Parquet crosswalk; file presence and correctness have not been verified by this executable inventory."},
		"semantic_search":     {State: "unknown", Scope: "running engine memory", Note: "An executable inventory cannot establish whether an embedding model is loaded. Query the application or perform an explicit functional test."},
	}}
	if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
		os.Exit(1)
	}
	if *removeSelf {
		p, e := os.Executable()
		if e == nil {
			e = os.Remove(p)
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, "inventory completed, but temporary inspector removal failed")
			os.Exit(1)
		}
	}
}
