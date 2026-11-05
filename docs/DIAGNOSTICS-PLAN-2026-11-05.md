# Safe adapted-call diagnostics plan

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

## Finding and scope

The 10:19:24 UTC conversion failure returned an error in 12 ms, but existing logs
only recorded adapter_error=true and result size. Its original cause cannot now
be recovered. Add diagnostics at existing adapted-call boundaries, with detailed
origin classification for inline conversion and the common private worker.
During qualification the live store had grown to 139,781 quads. A full MCP SELECT
hit the existing response cap and returned 502; log proxy read/protocol/limit
failures too, without raising caps or changing the HTTP failure contract. Use a
bounded loopback administrative snapshot for full production comparisons.
Keep application changes in this adapter; no engine/proxy rebuild, image change,
permission change, solver install, crawler restriction or OAuth reconfiguration.

## Implementation

- Wrap internal errors with stable category/stage enums while preserving Error()
  and Unwrap(); existing MCP response content/schema/error semantics remain intact.
- Log is_error separately from adapter_error, including recognized tool errors
  returned without a Go error. Ordinary negative findings remain successful calls.
- For failures log a generated error_ref, SHA-256 of error text, bounded category
  and stage, and adapter version. For convert log only allowlisted format hints.
- Never log raw request arguments, content, exception text, file paths, client
  request/session IDs or arbitrary format/tool strings. Unknown failures remain
  explicitly unclassified; do not guess causes from native free-text errors.
- Distinguish argument validation, inline file-IRI policy, workspace I/O, native
  input/output operations, worker startup/protocol/exit/output/version failures,
  and deadline/cancellation. Preserve existing timeouts, output limits and cleanup.
- Keep the existing INFO call-summary level. A tool failure does not by itself
  mean an HTTP outage. Pass-through requests and pre-server connector validation
  are not made into a complete new audit stream by this change.
  Only transport/response-decoding failures are additionally observed for
  pass-through tool requests; their ordinary semantic results remain unchanged.

## Qualification and deployment

1. Add regression tests for error categories/stages, unchanged response text,
   non-error negative findings, secret-marker redaction, deadlines and concurrency.
2. Run gofmt, go test -race ./..., go vet ./.... Build a clean committed candidate.
3. Exercise the exact worker through a disposable chain: seven-format conversion,
   malformed inputs, native errors, private analyses and concurrent calls. Capture
   the new log fields without changing shared QA RDF.
4. Back up the current adapter/configuration. Drain the owning tunnel; replace
   only the adapter, gate proxy readiness, and resume that same tunnel service.
5. Verify production RDF/registries, catalog/read-only hints, public requests,
   control-plane polling and a real connected-app status call. Preserve rollback
   artifacts and immutable executable/documentation identities. Remove owned QA
   processes after qualification. No GitHub push is included.
