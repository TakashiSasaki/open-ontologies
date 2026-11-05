// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func diagnosticAdapter(buffer *bytes.Buffer) *Adapter {
	return &Adapter{cfg: Config{TimeoutSeconds: 75}, Version: "test", instance: "test-instance",
		logger: slog.New(slog.NewJSONHandler(buffer, nil))}
}

func logObject(t *testing.T, buffer *bytes.Buffer) object {
	t.Helper()
	var entry object
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &entry); err != nil {
		t.Fatal(err, buffer.String())
	}
	return entry
}

func TestDiagnosticErrorsPreserveClientTextAndUnwrapping(t *testing.T) {
	base := errors.New("original error text")
	err := observe(codeNative, stageParse, base)
	if err.Error() != base.Error() || !errors.Is(err, base) || string(errorTool(err)) != string(errorTool(base)) {
		t.Fatal("diagnostics changed client behavior")
	}
	if observe(codeUnknown, stageUnknown, err) != err || observe(codeUnknown, stageUnknown, nil) != nil {
		t.Fatal("origin overwritten or nil changed")
	}
}

func TestFailureLogsRedactArgumentsErrorTextAndArbitraryEnums(t *testing.T) {
	const secret = "OO_SECRET_ARGUMENT_AND_ERROR_TEXT"
	for _, name := range []string{"onto_convert", secret} {
		for _, err := range []error{
			observe(codeNative, stageRender, errors.New(secret)),
			&observedError{cause: errors.New(secret), code: errorCode(secret), stage: errorStage(secret)},
		} {
			var buffer bytes.Buffer
			a := diagnosticAdapter(&buffer)
			args := object{"from": marshal(secret), "to": marshal(secret), "inline_content": marshal(secret), "path": marshal("/private/" + secret)}
			raw := errorTool(err)
			a.logAdaptedCall(context.Background(), name, args, time.Now(), raw, raw, err)
			if strings.Contains(buffer.String(), secret) || strings.Contains(buffer.String(), "/private/") {
				t.Fatal("sensitive content leaked", buffer.String())
			}
			entry := logObject(t, &buffer)
			digest := sha256.Sum256([]byte(secret))
			if str(entry, "error_sha256") != hex.EncodeToString(digest[:]) || !boolean(entry, "is_error") || !boolean(entry, "adapter_error") || str(entry, "error_ref") == "" {
				t.Fatal(buffer.String())
			}
			if name == "onto_convert" && (str(entry, "source_format_hint") != "unrecognized" || str(entry, "target_format") != "unrecognized") {
				t.Fatal(buffer.String())
			}
			if name == secret && str(entry, "tool") != "unlisted_tool" {
				t.Fatal("unknown tool name leaked")
			}
		}
	}
}

func TestAdaptedHTTPValidationLogsDiagnosticsWithoutChangingResponse(t *testing.T) {
	var buffer bytes.Buffer
	a := diagnosticAdapter(&buffer)
	request := message{Version: "2.0", ID: marshal("PRIVATE_CLIENT_ID"), Method: "tools/call", Params: marshal(map[string]any{
		"name": "onto_convert", "arguments": object{"inline_content": marshal("PRIVATE_RDF_CONTENT"), "to": marshal("rdfxml")},
	})}
	recorder := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(marshal(request)))
	r.Header.Set("Content-Type", "application/json")
	a.Handler().ServeHTTP(recorder, r)
	if recorder.Code != 200 {
		t.Fatal(recorder.Code)
	}
	var response message
	_ = json.Unmarshal(recorder.Body.Bytes(), &response)
	want := errorTool(errors.New("from is required for inline conversion: turtle, ntriples, rdfxml, nquads, trig, jsonld or ofn"))
	if string(response.Result) != string(want) || string(response.ID) != string(request.ID) {
		t.Fatal("client envelope changed", recorder.Body.String())
	}
	entry := logObject(t, &buffer)
	if str(entry, "error_code") != string(codeArguments) || str(entry, "error_stage") != string(stageArguments) || str(entry, "target_format") != "rdfxml" {
		t.Fatal(buffer.String())
	}
	if strings.Contains(buffer.String(), "PRIVATE_") {
		t.Fatal("request ID or content logged")
	}
}

func TestToolResultsAndNegativeFindingsHaveDistinctLogs(t *testing.T) {
	for _, p := range []object{{"conforms": marshal(false)}, {"ok": marshal(false)}, {"valid": marshal(false)}, {"verdict": marshal("deny")}, {"error": marshal("")}, {"error": marshal(nil)}} {
		var buffer bytes.Buffer
		a := diagnosticAdapter(&buffer)
		raw := jsonTool(p)
		a.logAdaptedCall(context.Background(), "onto_shacl", object{}, time.Now(), a.decorate("onto_shacl", object{}, raw), raw, nil)
		entry := logObject(t, &buffer)
		if boolean(entry, "is_error") || supplied(entry, "error_sha256") || supplied(entry, "error_ref") {
			t.Fatal("negative finding relabeled as failure", buffer.String())
		}
	}
	for _, raw := range []json.RawMessage{
		jsonTool(object{"error": marshal("PRIVATE_NATIVE_ERROR")}),
		marshal(object{"isError": marshal(true), "content": marshal([]object{{"type": marshal("text"), "text": marshal("PRIVATE_NATIVE_ERROR")}})}),
		marshal(object{"isError": marshal(true), "content": marshal([]any{})}),
	} {
		var buffer bytes.Buffer
		a := diagnosticAdapter(&buffer)
		a.logAdaptedCall(context.Background(), "onto_shacl", object{}, time.Now(), a.decorate("onto_shacl", object{}, raw), raw, nil)
		entry := logObject(t, &buffer)
		if !boolean(entry, "is_error") || boolean(entry, "adapter_error") || str(entry, "error_code") != string(codeToolResult) || str(entry, "error_stage") != string(stageResult) {
			t.Fatal(buffer.String())
		}
		if strings.Contains(buffer.String(), "PRIVATE_NATIVE_ERROR") {
			t.Fatal("native error text logged")
		}
	}
}

func TestDeadlineAndCancellationDoNotHideConcreteInputErrors(t *testing.T) {
	for _, deadline := range []bool{true, false} {
		var ctx context.Context
		var cancel context.CancelFunc
		want := codeCancelled
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = codeDeadline
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		for _, code := range []errorCode{codeExit, codeIO, codeArguments, codeNative} {
			var buffer bytes.Buffer
			a := diagnosticAdapter(&buffer)
			err := observe(code, stageRead, errors.New("old message"))
			a.logAdaptedCall(ctx, "onto_convert", object{}, time.Now(), errorTool(err), errorTool(err), err)
			entry := logObject(t, &buffer)
			expected := code
			if code == codeExit || code == codeIO {
				expected = want
			}
			if str(entry, "error_code") != string(expected) {
				t.Fatal(buffer.String())
			}
		}
		cancel()
	}
}

func TestConcurrentFailureReferencesAreUnique(t *testing.T) {
	var buffer bytes.Buffer
	a := diagnosticAdapter(&buffer)
	var group sync.WaitGroup
	for range 64 {
		group.Go(func() {
			err := observe(codeArguments, stageArguments, errors.New("same safe error"))
			a.logAdaptedCall(context.Background(), "onto_convert", object{}, time.Now(), errorTool(err), errorTool(err), err)
		})
	}
	group.Wait()
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(buffer.Bytes()), []byte{'\n'}) {
		var entry object
		_ = json.Unmarshal(line, &entry)
		ref := str(entry, "error_ref")
		if ref == "" || seen[ref] {
			t.Fatal("duplicate reference", ref)
		}
		seen[ref] = true
	}
	if len(seen) != 64 {
		t.Fatal(len(seen))
	}
}

func TestWorkerCapacityTimeoutIsClassifiedWithoutStartingWorker(t *testing.T) {
	var buffer bytes.Buffer
	a := diagnosticAdapter(&buffer)
	a.slots = make(chan struct{}, 1)
	a.slots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := a.withWorker(ctx, func(*worker) (json.RawMessage, error) {
		t.Fatal("worker started despite full capacity")
		return nil, nil
	})
	a.logAdaptedCall(ctx, "onto_convert", object{}, time.Now(), errorTool(err), errorTool(err), err)
	entry := logObject(t, &buffer)
	if str(entry, "error_code") != string(codeDeadline) || str(entry, "error_stage") != string(stageCapacity) || len(a.slots) != 1 {
		t.Fatal(buffer.String())
	}
}

type testWriteCloser struct{ io.Writer }

func (testWriteCloser) Close() error { return nil }

func TestWorkerRPCFailureOrigins(t *testing.T) {
	for _, test := range []struct {
		name, reply string
		limit       int
		code        errorCode
		stage       errorStage
	}{
		{"protocol", "not JSON\n", 4096, codeProtocol, stageRead},
		{"rpc", "{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32601}}\n", 4096, codeRPC, stageRPC},
		{"eof", "", 4096, codeExit, stageRead},
		{"output_limit", strings.Repeat("x", 30) + "\n", 8, codeOutput, stageRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(test.reply))
			scanner.Buffer(make([]byte, 1), test.limit)
			w := &worker{stdin: testWriteCloser{io.Discard}, out: scanner}
			_, err := w.rpc("initialize", object{})
			var diagnostic *observedError
			if !errors.As(err, &diagnostic) || diagnostic.code != test.code || diagnostic.stage != test.stage {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkerStartFailureCleansScratchAndKeepsStage(t *testing.T) {
	root := t.TempDir()
	w, err := startWorker(context.Background(), filepath.Join(root, "absent-worker"), root, "test")
	if w != nil || err == nil {
		t.Fatal("absent worker started")
	}
	var diagnostic *observedError
	if !errors.As(err, &diagnostic) || diagnostic.code != codeStart || diagnostic.stage != stageStart || err.Error() != "cannot start upstream worker" {
		t.Fatal(err)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("scratch leaked", e)
	}
}

func TestBackendResponseLimitsAndProtocolFailuresAreClassified(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		code                    errorCode
	}{
		{"json_limit", "application/json", strings.Repeat(" ", maxResponse+1), codeBackendLimit},
		{"sse_limit", "text/event-stream", "data: " + strings.Repeat("x", maxResponse) + "\n\n", codeBackendLimit},
		{"json_protocol", "application/json", "not JSON", codeBackendProtocol},
		{"unexpected_type", "text/plain", "PRIVATE_BACKEND_TEXT", codeBackendProtocol},
		{"ended", "text/event-stream", "", codeBackendRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := responseMessage(strings.NewReader(test.body), test.contentType, marshal(1))
			var diagnostic *observedError
			if !errors.As(err, &diagnostic) || diagnostic.code != test.code || diagnostic.stage != stageBackendRead {
				t.Fatal(err)
			}
		})
	}
}

func TestProxyFailureLogsCodeWithoutChangingHTTPContract(t *testing.T) {
	var buffer bytes.Buffer
	a := diagnosticAdapter(&buffer)
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r = r.WithContext(context.WithValue(r.Context(), callKey{}, callInfo{name: "onto_query", args: object{"query": marshal("PRIVATE_QUERY")}, id: marshal("PRIVATE_ID"), started: time.Now()}))
	w := httptest.NewRecorder()
	err := observe(codeBackendLimit, stageBackendRead, errors.New("PRIVATE_LIMIT_ERROR"))
	a.proxyFailure(w, r, err)
	if w.Code != http.StatusBadGateway || w.Body.String() != "upstream connection failed\n" {
		t.Fatal("HTTP response contract changed", w.Code, w.Body.String())
	}
	entry := logObject(t, &buffer)
	if str(entry, "tool") != "onto_query" || str(entry, "error_code") != string(codeBackendLimit) || str(entry, "error_stage") != string(stageBackendRead) {
		t.Fatal(buffer.String())
	}
	if strings.Contains(buffer.String(), "PRIVATE_") {
		t.Fatal("proxy failure diagnostics leaked request or error text")
	}
}
