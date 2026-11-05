// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package main

import (
	"os"
	"testing"
)

func TestCrosswalkInventoryDoesNotClaimDataVerification(t *testing.T) {
	t.Chdir(t.TempDir())
	if p := crosswalk(); p.State != "missing" {
		t.Fatal(p)
	}
	_ = os.Mkdir("data", 0o700)
	_ = os.WriteFile("data/crosswalks.parquet", nil, 0o600)
	if p := crosswalk(); p.State != "unknown" {
		t.Fatal(p)
	}
	_ = os.WriteFile("data/crosswalks.parquet", []byte("not actually Parquet"), 0o600)
	if p := crosswalk(); p.State != "present_unverified" {
		t.Fatal(p)
	}
}
