// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponse = 10 << 20
const maxInline = 1 << 20

type object = map[string]json.RawMessage

func marshal(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(v)
}
func str(o object, key string) string   { var s string; _ = json.Unmarshal(o[key], &s); return s }
func boolean(o object, key string) bool { var b bool; _ = json.Unmarshal(o[key], &b); return b }
func supplied(o object, key string) bool {
	r, ok := o[key]
	return ok && string(bytes.TrimSpace(r)) != "null"
}
func clone(o object) object {
	n := object{}
	for k, v := range o {
		n[k] = v
	}
	return n
}

type message struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func responseMessage(body io.Reader, contentType string, expected json.RawMessage) (message, error) {
	var result message
	r := io.LimitReader(body, maxResponse+1)
	check := func(data []byte) (bool, error) {
		if err := json.Unmarshal(data, &result); err != nil {
			return false, err
		}
		if result.Version != "2.0" {
			return false, errors.New("invalid backend JSON-RPC version")
		}
		return bytes.Equal(bytes.TrimSpace(result.ID), expected), nil
	}
	if strings.HasPrefix(contentType, "application/json") {
		data, err := io.ReadAll(r)
		if err != nil {
			return result, err
		}
		if len(data) > maxResponse {
			return result, errors.New("backend response exceeds limit")
		}
		matched, err := check(data)
		if err == nil && !matched {
			err = errors.New("unrelated backend response")
		}
		return result, err
	}
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return result, errors.New("unsupported backend response")
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxResponse)
	var parts []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			continue
		}
		if line != "" || len(parts) == 0 {
			continue
		}
		data := strings.Join(parts, "\n")
		parts = nil
		if strings.TrimSpace(data) == "" {
			continue
		}
		matched, err := check([]byte(data))
		if err != nil || matched {
			return result, err
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, errors.New("backend ended without matching result")
}

func (a *Adapter) native(ctx context.Context, headers http.Header, method string, params any) (json.RawMessage, error) {
	id := marshal(fmt.Sprintf("adapter-%s-%d", a.instance, a.sequence.Add(1)))
	body := marshal(message{Version: "2.0", ID: id, Method: method, Params: marshal(params)})
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.BackendURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Del("Content-Length")
	req.Header.Del("Idempotency-Key")
	req.Header.Del("X-Idempotency-Key")
	req.Header.Set("Mcp-Method", method)
	req.Header.Del("Mcp-Name")
	if tool, ok := params.(map[string]any); ok {
		if name, ok := tool["name"].(string); ok {
			req.Header.Set("Mcp-Name", name)
		}
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.New("backend request failed or timed out; outcome may be unknown; no retry was made")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("backend HTTP status %d; no retry was made", resp.StatusCode)
	}
	m, err := responseMessage(resp.Body, resp.Header.Get("Content-Type"), id)
	if err != nil {
		return nil, err
	}
	if len(m.Error) > 0 {
		return nil, errors.New("backend rejected an internal MCP request")
	}
	return m.Result, nil
}
func (a *Adapter) nativeTool(ctx context.Context, h http.Header, name string, args object) (json.RawMessage, error) {
	return a.native(ctx, h, "tools/call", map[string]any{"name": name, "arguments": args})
}
func textContent(raw json.RawMessage) (string, error) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		return "", errors.New("upstream tool did not return one successful text result")
	}
	return result.Content[0].Text, nil
}
func payload(raw json.RawMessage) (object, error) {
	text, err := textContent(raw)
	if err != nil {
		return nil, err
	}
	var p object
	if err := json.Unmarshal([]byte(text), &p); err != nil || p == nil {
		return nil, errors.New("upstream tool returned non-object JSON")
	}
	if supplied(p, "error") {
		return nil, fmt.Errorf("upstream tool error: %s", str(p, "error"))
	}
	return p, nil
}
func jsonTool(p object) json.RawMessage {
	return marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(marshal(p))}}})
}
func errorTool(err error) json.RawMessage {
	return marshal(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": string(marshal(map[string]any{"error": err.Error()}))}}})
}
func scopeResult(raw json.RawMessage) json.RawMessage {
	p, err := payload(raw)
	if err != nil {
		return raw
	}
	p["execution_scope"] = marshal("request")
	p["persistent_store_modified"] = marshal(false)
	return jsonTool(p)
}
