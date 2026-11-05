// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed state_reader.py
var stateReader string

type registry struct {
	Policies []object `json:"policies"`
	Actions  []object `json:"actions"`
}
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxResponse {
		return 0, errors.New("output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func (a *Adapter) readRegistry(ctx context.Context) (registry, error) {
	var result registry
	path := a.cfg.StateDBPath
	if path == "" && a.cfg.ServerDataRoots["/data"] != "" {
		path = filepath.Join(a.cfg.ServerDataRoots["/data"], "open-ontologies.db")
	}
	if !filepath.IsAbs(path) {
		return result, errors.New("read-only state_db_path is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-c", stateReader, path)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	var out limitedBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return result, errors.New("cannot inspect registry read-only; no authorization verdict is available")
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return result, errors.New("invalid registry data")
	}
	return result, nil
}

func findAction(r registry, name string) (object, error) {
	for _, s := range r.Actions {
		if str(s, "name") == name {
			return clone(s), nil
		}
	}
	return nil, errors.New("unknown action; use onto_action_list / onto_action_info")
}
