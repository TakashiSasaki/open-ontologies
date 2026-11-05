# Native MCP improvement work

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

This public fork keeps the Rust source, tests, Cargo manifests and root
`Dockerfile` together for native Open Ontologies improvements.

- Upstream: <https://github.com/fabio-rovai/open-ontologies>
- Fork: <https://github.com/TakashiSasaki/open-ontologies>
- Working branch: `codex/native-mcp-improvements`.
- Initial upstream base: `375fbc4789debfb57e9c7dfc6bf0e8db8fb21f9b`
  (inspected 2026-11-05 UTC, package version 2.1.0).
- The earlier deployed/audited baseline was
  `cefe5997d04553ac495873e8aa1046c1d6b85f7d`. Previous findings and patch
  candidates must be requalified against this newer source.

## Preparation status

This initial fork change records the plan and preserves three extracted native
patch candidates under `patches/proposed-native/`. They are **not applied source
changes or an automatic build patch series**. Publishing this preparation branch
does not claim a newly tested engine image or production deployment.

Upstream already pins Rust 1.99.1 and uses `cargo build --locked` in the root
Dockerfile; this branch inherits both. Base images are not digest-pinned and apt
packages are not a frozen snapshot, so bit-for-bit rebuilds are not yet promised.

## Native work plan

1. **Policy enforcement at mutation time.** Define deny/error semantics and
   coordinate policy lookup, graph reads and actual writes, including concurrent
   and background writers, partial failures and rollback. A proxy preflight
   cannot guarantee this. SQLite metadata and the RDF store do not share a
   transaction automatically.
2. **Consistent action RDF terms.** Unify ASK/SELECT bindings and effects,
   distinguishing IRIs and literals. Reproduce bare/bracketed IRIs and inspect
   stored schemas before changing meaning. Qualify actual and concurrent apply.
3. **DL diagnostics.** Review the archived tableau patch against this base;
   preserve newer upstream budget fixes. Expose bounded clash/branch diagnostics
   and undecided results for exhausted searches. Diagnostics are not minimal
   axiom justifications or machine-checked proofs.
4. **PDDL.** Review the narrow archived predicate-`a` correction, then define
   supported SPARQL, constant declarations, reversible collision-free symbols
   and RDF term sorts. Report incomplete translation and qualify solver execution.
5. **Build identity and packaging.** Preserve upstream numeric version 2.1.0;
   identify modified builds as `2.1.0+local.<commit>[.dirty]` in CLI/status/MCP
   and container metadata. Supply provenance explicitly to Docker builds:
   linked-worktree Git pointers do not resolve inside the build container.
   Package external proof tools/solvers only with deliberate functional checks.

Keep independent fixes and their reproductions in separate commits. The generic
MCP bridge and application adapter remain separate projects; check interaction
with their normalizations/private workers rather than importing old interface
changes blindly into the engine.

## Validation and adoption

For source changes, run focused Rust tests and relevant existing integration
checks, then build the root Dockerfile for Linux arm64. Exercise MCP through
the adapter using an isolated data mount. Patch-application checks are not Rust
tests; missing optional solvers/checkers are not failed core-engine tests.

Before deployment, preserve data/logs and the prior image; record exact source,
image and executable identities. The adapter's private worker must match the
new HTTP engine build. Verify catalog/annotation behavior and actual cloud tool
calls before declaring the deployment qualified. Keep credentials and live
graph/registry payloads outside this public repository.

The upstream MIT notice is retained; fork contributions are MIT-licensed.
