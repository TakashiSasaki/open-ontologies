// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const workerTools = "onto_load,onto_clear,onto_save,onto_convert,onto_reason,onto_defects,onto_classify_el,onto_dl_check,onto_dl_explain,onto_justify,onto_shacl,onto_ingest,onto_diff,onto_status,onto_query,onto_action_register,onto_plan_validate,onto_plan_compile_pddl,onto_validate"

type worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	out      *bufio.Scanner
	dir      string
	sequence uint64
}

func startWorker(ctx context.Context, binary, root, version string) (_ *worker, err error) {
	return startWorkerLimited(ctx, binary, root, version, 0)
}

func startWorkerLimited(ctx context.Context, binary, root, version string, memoryMiB int) (_ *worker, err error) {
	dir, err := os.MkdirTemp(root, "request-")
	if err != nil {
		return nil, observe(codeScratch, stageWorkspace, err)
	}
	w := &worker{dir: dir}
	defer func() {
		if err != nil {
			w.close()
		}
	}()
	w.cmd = exec.CommandContext(ctx, binary, "serve", "--no-connect", "--data-dir", filepath.Join(dir, "state"), "--storage-mode", "memory", "--config", filepath.Join(dir, "absent.toml"), "--tools-allow", workerTools)
	if memoryMiB != 0 {
		args := append([]string{fmt.Sprintf("--as=%d", int64(memoryMiB)<<20), "--cpu=20", "--fsize=8388608", "--nofile=128", "--"}, w.cmd.Args...)
		w.cmd = exec.CommandContext(ctx, "/usr/bin/prlimit", args...)
	}
	w.cmd.Dir = dir
	// Do not pass credentials, persistent HOME, checker/model overrides or the
	// host's storage configuration into an isolated analysis process.
	w.cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "RUST_LOG=error", "OPEN_ONTOLOGIES_STORAGE_MODE=memory"}
	w.cmd.Stderr = io.Discard
	w.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	w.cmd.WaitDelay = time.Second
	w.cmd.Cancel = func() error {
		if w.cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-w.cmd.Process.Pid, syscall.SIGKILL)
	}
	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		return nil, observe(codeIO, stageStart, err)
	}
	stdout, e := w.cmd.StdoutPipe()
	if e != nil {
		return nil, observe(codeIO, stageStart, e)
	}
	w.out = bufio.NewScanner(stdout)
	w.out.Buffer(make([]byte, 4096), maxResponse)
	if err = w.cmd.Start(); err != nil {
		return nil, observe(codeStart, stageStart, errors.New("cannot start upstream worker"))
	}
	_, err = w.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "open-ontologies-mcp-adapter", "version": version}})
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintln(w.stdin, string(marshal(message{Version: "2.0", Method: "notifications/initialized", Params: marshal(object{})})))
	if err != nil {
		return nil, observe(codeIO, stageSend, err)
	}
	return w, nil
}
func (w *worker) rpc(method string, params any) (json.RawMessage, error) {
	w.sequence++
	id := marshal(w.sequence)
	if _, err := fmt.Fprintln(w.stdin, string(marshal(message{Version: "2.0", ID: id, Method: method, Params: marshal(params)}))); err != nil {
		return nil, observe(codeIO, stageSend, errors.New("worker input closed"))
	}
	for w.out.Scan() {
		var m message
		if err := json.Unmarshal(w.out.Bytes(), &m); err != nil {
			return nil, observe(codeProtocol, stageRead, errors.New("invalid worker JSON"))
		}
		if string(m.ID) != string(id) {
			continue
		}
		if len(m.Error) > 0 {
			return nil, observe(codeRPC, stageRPC, errors.New("upstream worker rejected MCP request"))
		}
		return m.Result, nil
	}
	code := codeExit
	if w.out.Err() != nil {
		code = codeOutput
	}
	return nil, observe(code, stageRead, errors.New("upstream worker stopped or exceeded request deadline/output limit"))
}
func (w *worker) call(name string, args object) (json.RawMessage, error) {
	return w.rpc("tools/call", map[string]any{"name": name, "arguments": args})
}
func (w *worker) checked(name string, args object) (object, error) {
	r, e := w.call(name, args)
	if e != nil {
		return nil, e
	}
	return payload(r)
}
func (w *worker) close() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Cancel()
		_ = w.cmd.Wait()
	}
	if w.dir != "" {
		_ = os.RemoveAll(w.dir)
	}
}
func (w *worker) save(name string) (string, error) {
	p := filepath.Join(w.dir, name)
	_, err := w.checked("onto_save", object{"path": marshal(p), "format": marshal("ntriples")})
	if err != nil {
		return "", err
	}
	b, err := readLimited(p, maxResponse)
	return string(b), err
}
func readLimited(path string, max int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return readBounded(f, max)
}
func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, max+1))
	if e == nil && int64(len(b)) > max {
		return nil, errors.New("input or generated RDF exceeds size limit")
	}
	return b, e
}

// Read only explicitly mapped backend data roots. OpenRoot also rejects
// symlink escapes; a client-supplied /etc or host-home path is never opened.
func (a *Adapter) readServerFile(path string) ([]byte, error) {
	path = filepath.Clean(path)
	for backend, local := range a.cfg.ServerDataRoots {
		rel, err := filepath.Rel(backend, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		root, err := os.OpenRoot(local)
		if err != nil {
			return nil, errors.New("configured server data root is unavailable")
		}
		f, err := root.Open(rel)
		_ = root.Close()
		if err != nil {
			return nil, errors.New("cannot read file within configured server data roots")
		}
		defer f.Close()
		return readBounded(f, maxResponse)
	}
	return nil, errors.New("request-local file input must be inside a configured server data root; use inline input instead")
}
