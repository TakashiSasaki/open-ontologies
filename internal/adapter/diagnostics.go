// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const callRefHeader = "X-Mcp-Call-Ref"

type callRefKey struct{}

var callRefPattern = regexp.MustCompile("^[0-9a-f]{32}$")

type errorCode string
type errorStage string

const (
	codeArguments        errorCode = "invalid_arguments"
	codeInputSize        errorCode = "input_too_large"
	codeFileIRI          errorCode = "inline_file_iri"
	codeScratch          errorCode = "scratch_io_error"
	codeNative           errorCode = "native_tool_error"
	codeNativeResult     errorCode = "invalid_native_result"
	codeStart            errorCode = "worker_start_failed"
	codeIO               errorCode = "worker_io_error"
	codeProtocol         errorCode = "worker_protocol_error"
	codeRPC              errorCode = "worker_rpc_error"
	codeOutput           errorCode = "worker_output_error"
	codeExit             errorCode = "worker_exited"
	codeIdentity         errorCode = "worker_identity_error"
	codeVersion          errorCode = "worker_version_mismatch"
	codeDeadline         errorCode = "deadline_exceeded"
	codeCancelled        errorCode = "request_cancelled"
	codeToolResult       errorCode = "tool_execution_error"
	codeUnknown          errorCode = "unclassified_error"
	codeBackendLimit     errorCode = "backend_output_limit"
	codeBackendRead      errorCode = "backend_read_error"
	codeBackendProtocol  errorCode = "backend_protocol_error"
	codeBackendTransport errorCode = "backend_transport_error"

	stageArguments        errorStage = "conversion.arguments"
	stageStage            errorStage = "conversion.stage_input"
	stageParse            errorStage = "conversion.parse_input"
	stageIRIs             errorStage = "conversion.check_iris"
	stageRender           errorStage = "conversion.render_output"
	stageCapacity         errorStage = "worker.capacity"
	stageWorkspace        errorStage = "worker.create_workspace"
	stageStart            errorStage = "worker.start"
	stageSend             errorStage = "worker.send"
	stageRead             errorStage = "worker.read"
	stageRPC              errorStage = "worker.rpc"
	stageIdentity         errorStage = "worker.identity"
	stageResult           errorStage = "adapter.result"
	stageUnknown          errorStage = "adapter.adapt"
	stageBackendRead      errorStage = "backend.read_response"
	stageBackendRoundTrip errorStage = "backend.roundtrip"
)

var diagnosticCodes = map[errorCode]bool{
	codeArguments: true, codeInputSize: true, codeFileIRI: true, codeScratch: true,
	codeNative: true, codeNativeResult: true, codeStart: true, codeIO: true,
	codeProtocol: true, codeRPC: true, codeOutput: true, codeExit: true,
	codeIdentity: true, codeVersion: true, codeDeadline: true, codeCancelled: true,
	codeToolResult: true, codeUnknown: true,
	codeBackendLimit: true, codeBackendRead: true, codeBackendProtocol: true, codeBackendTransport: true,
}
var diagnosticStages = map[errorStage]bool{
	stageArguments: true, stageStage: true, stageParse: true, stageIRIs: true,
	stageRender: true, stageCapacity: true, stageWorkspace: true, stageStart: true,
	stageSend: true, stageRead: true, stageRPC: true, stageIdentity: true,
	stageResult: true, stageUnknown: true,
	stageBackendRead: true, stageBackendRoundTrip: true,
}

// Error text and unwrapping semantics stay unchanged for existing clients.
// Diagnostics are internal metadata, never interpolated into the error text.
type observedError struct {
	cause error
	code  errorCode
	stage errorStage
}

func (e *observedError) Error() string { return e.cause.Error() }
func (e *observedError) Unwrap() error { return e.cause }
func observe(code errorCode, stage errorStage, err error) error {
	if err == nil {
		return nil
	}
	var existing *observedError
	if errors.As(err, &existing) {
		return err // Keep the more specific origin, rather than guessing upstream.
	}
	return &observedError{cause: err, code: code, stage: stage}
}

// A negative finding is not an error. Only the existing top-level error
// contract or an explicit MCP isError flag marks an execution failure.
func resultFailure(raw json.RawMessage) error {
	var result object
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	var blocks []object
	if json.Unmarshal(result["content"], &blocks) == nil && len(blocks) == 1 && str(blocks[0], "type") == "text" {
		text := str(blocks[0], "text")
		var p object
		if json.Unmarshal([]byte(text), &p) == nil && supplied(p, "error") && string(p["error"]) != `""` {
			message := str(p, "error")
			if message == "" {
				message = string(p["error"])
			}
			return errors.New(message)
		}
		if boolean(result, "isError") {
			return errors.New(text)
		}
	}
	if boolean(result, "isError") {
		return errors.New("MCP tool execution failed with non-single-text content")
	}
	return nil
}

func safeFormat(args object, key string) string {
	if !supplied(args, key) {
		return "unspecified"
	}
	var value string
	if json.Unmarshal(args[key], &value) != nil {
		return "invalid_type"
	}
	if len(value) > 16 {
		return "unrecognized"
	}
	value = strings.ToLower(value)
	if _, ok := sourceExtensions[value]; ok {
		return value
	}
	if key == "to" && value == "functional" {
		return value
	}
	return "unrecognized"
}

func safeAdaptedTool(name string) string {
	if auditedTools[name] || scopedTools[name] || name == "onto_convert" || name == "onto_ingest" || name == "onto_diff" || name == "onto_query" || name == "onto_load" || name == "onto_clear" || name == "onto_save" {
		return name
	}
	return "unlisted_tool"
}

// This is deliberately a bounded log schema. Never log err, args, result text,
// client IDs, file paths or arbitrary format/tool names. Keep the INFO summary
// level so a rejected input is not mistaken for a service outage.
func (a *Adapter) logAdaptedCall(ctx context.Context, name string, args object, started time.Time, result, original json.RawMessage, callErr error) {
	a.logDiagnosticCall("adapted MCP call", ctx, name, args, started, result, original, callErr)
}

func (a *Adapter) logDiagnosticCall(message string, ctx context.Context, name string, args object, started time.Time, result, original json.RawMessage, callErr error) {
	err := callErr
	code, stage := codeUnknown, stageUnknown
	if err == nil {
		// The caller has already decorated the MCP result. Avoid decoding
		// successful large RDF content a second time just for logging.
		var flags struct {
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(result, &flags) == nil && flags.IsError {
			err = resultFailure(original)
			if err == nil {
				err = errors.New("MCP tool execution failed")
			}
		}
		code, stage = codeToolResult, stageResult
	}
	attrs := []any{"tool", safeAdaptedTool(name),
		"adapter_error", callErr != nil, "is_error", err != nil, "result_bytes", len(result), "adapter_version", a.Version}
	if ref, ok := ctx.Value(callRefKey{}).(string); ok {
		attrs = append(attrs, "call_ref", ref)
	}
	if !started.IsZero() {
		attrs = append(attrs, "duration_ms", time.Since(started).Milliseconds())
	}
	if name == "onto_convert" {
		attrs = append(attrs, "source_format_hint", safeFormat(args, "from"), "target_format", safeFormat(args, "to"))
	}
	if err != nil {
		var detail *observedError
		if errors.As(err, &detail) {
			if diagnosticCodes[detail.code] {
				code = detail.code
			}
			if diagnosticStages[detail.stage] {
				stage = detail.stage
			}
		}
		contextState := "active"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			contextState = "deadline_exceeded"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			contextState = "cancelled"
		}
		if callErr != nil {
			// A coincident cancellation must not relabel a concrete validation or
			// parser failure. Only interrupted worker transport/capacity failures
			// or an actual wrapped context error inherit the context category.
			interruptible := code == codeExit || code == codeIO || code == codeStart || code == codeDeadline || code == codeBackendTransport || code == codeBackendRead
			if errors.Is(callErr, context.DeadlineExceeded) || (interruptible && contextState == "deadline_exceeded") {
				code = codeDeadline
			} else if errors.Is(callErr, context.Canceled) || (interruptible && contextState == "cancelled") {
				code = codeCancelled
			}
		}
		digest := sha256.Sum256([]byte(err.Error()))
		ref := "adp-" + a.instance + "-" + strconv.FormatUint(a.sequence.Add(1), 10)
		attrs = append(attrs, "error_code", string(code), "error_stage", string(stage), "error_ref", ref,
			"error_sha256", hex.EncodeToString(digest[:]), "error_bytes", len(err.Error()), "context_state", contextState)
	}
	logger := a.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(message, attrs...)
}
