# Native MCP implementation plan — 2026-11-05

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

Base: upstream `375fbc4789debfb57e9c7dfc6bf0e8db8fb21f9b`; development branch
`codex/native-mcp-improvements`. Existing production uses an older unmodified
engine plus independent adapters. Native work is built and qualified separately.

## Implementation and acceptance

1. **Policy and graph mutation.** Validate RDF target terms and template binding;
   propagate query/storage failures without an allow verdict. Integrate checks
   with graph mutations under a consistent pre-state and policy configuration.
   Cover graph writers centrally, bulk changes, removals, no-op writes and
   concurrent policy changes. Policy administration is trusted configuration,
   not an authentication system. State the crash/transaction boundary honestly.
2. **Action semantics.** Share term binding between preconditions and effects;
   support canonical IRIs/literals without double angle brackets or query
   injection. Stage additions/removals as one RDF transaction; report actual
   changes. Concurrent ticks check preconditions and invariants before commit,
   preserving existing triples on failure rather than compensating afterward.
3. **DL diagnostics.** Adapt the preserved bounded trace/unknown-result patch to
   this base, retain newer upstream budget fixes, and run actual native tests.
4. **PDDL.** Normalize predicate `a`; emit declared constants, reversible symbols
   and valid RDF-term handling for a documented finite triple-pattern fragment.
   Unsupported SPARQL, stochastic effects or semantics stay explicitly
   incomplete and execution-unready. Test generated syntax and symbol roundtrips;
   distinguish compiler qualification from an absent external planner.
5. **Build and publication.** Preserve upstream numeric version and embed
   `+local.<revision>[.dirty]` in native CLI/MCP/status. Pass provenance into the
   Docker build, build Linux arm64, exercise actual HTTP/STDIO tools in disposable
   storage, and publish independently reviewable commits to the authorized fork.

## Verification and constraints

- Run focused native regressions, adjacent existing tests, formatting/lint and
  the meaningful repository checks. Verify against the final source identities.
- Tests must cover rejected input, denial/error with no writes, concurrent
  change, mixed add/remove failure, invariant rejection and preserved data.
- Keep credentials/live payloads out of Git. Preserve upstream MIT attribution.
- Retain old patch proposals with their original base; record which changes are
  implemented in source. Apply checks are not compile or runtime evidence.
- No host reboot or replacement of production services is part of publishing
  this native implementation. Produce the reviewable image and adoption record
  before any separately requested production cutover.

## Progress

- Planning and current-base source review complete; implementation in progress.
