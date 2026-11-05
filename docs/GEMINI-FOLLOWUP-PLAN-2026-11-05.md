# Gemini follow-up: conversion and bounded policy diagnostics

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

## Evidence and ownership

The user reported successful representative conversions, reasoning and validation,
plus four remaining usability concerns. These are user-reported cases, not proof
of complete algorithm coverage, equivalence preservation, or checked Lean/ATP proofs.
The current engine is the pinned native fork a74f122c8826, adapter 234dff0ea4d6.

1. `onto_convert` requires a server path. Add explicit inline content plus source
   format hint in this dedicated adapter, using the exact private native worker.
   Preserve existing path/output calls. Inline mode rejects output files, limits
   size/time/concurrency, retains OFN loss reports, and rejects file IRIs to avoid
   turning relative input into private worker paths. Explicit document bases or
   absolute non-file IRIs are required. The shared store is never loaded/cleared.
2. Native transaction refusal serializes every rule/target result in one error.
   For large, valid native policy_denied reports, summarize exact distinct target
   and denied-target counts plus a few examples, preserving isError=true and a
   digest of the original error. This bounds client payload, not engine policy
   evaluation CPU or report construction. Do not retry the mutation or infer
   that every higher-level side effect was rolled back.
3. Missing ontology_dirs is a configuration gap. Add actionable machine-readable
   diagnostics, not implicit exposure of arbitrary caches/directories. Compile
   caches are not interchangeable with published source repositories. Explicit
   configuration of a managed source directory remains a separate operator step.
4. External solvers resolve inside the native container's environment, not the
   host's ordinary PATH. Keep missing/present/verified distinct. Optional solver
   packaging is a future container-image change, not a proxy workaround; this
   task does not install solvers or rebuild/replace the engine.

## Execution and acceptance

- Add boundary unit tests and run gofmt, race tests, vet.
- On a disposable native backend, reproduce an oversized policy rejection and
  confirm no target triples were removed; verify the adapted bounded result.
- Convert Turtle, OFN and other RDF source formats inline, preserve dataset graph
  names for dataset outputs, retain OFN warnings, reject invalid/oversize/relative
  file-producing input, and confirm shared data and scratch cleanup.
- Run the existing private/read-only integration suite through the modern proxy.
- Drain the existing tunnel before changing the adapter, retain worker/image pins
  and generic read-only policy, and verify real deployed calls and data identity.
- Record exact commits, binary hashes, backups, limits and verification. Keep real
  requests, runtime configuration and data outside Git. Do not clear production.
