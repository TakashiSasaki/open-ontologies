# Native Open Ontologies fork

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

This branch contains native Rust fixes to the upstream MIT-licensed source. The root Dockerfile builds the modified source. The independent
compatibility proxy, application adapter and public landing page remain separate
repositories.

- Upstream base: `375fbc4789debfb57e9c7dfc6bf0e8db8fb21f9b`, upstream version 2.1.0.
- Development: `TakashiSasaki/open-ontologies`, `codex/native-mcp-improvements`.
- Current candidate: `a74f122c88269ec3b2300aae952d52e7d7befa94`.
- Plan: [docs/NATIVE-MCP-PLAN.md](docs/NATIVE-MCP-PLAN.md).
- Verification and image identities: [docs/NATIVE-MCP-VERIFICATION.md](docs/NATIVE-MCP-VERIFICATION.md).

## Future implementation policy

The operator's policy, confirmed on 2026-11-05, is to implement improvements in
the generic compatibility proxy or dedicated Open Ontologies adapter whenever
those layers can provide the required behavior correctly. This fork is modified
only when native engine changes are necessary. An existing fork and permission
to publish it do not change that choice.

For each native proposal, record the guarantee a proxy cannot provide and keep
the native change to that responsibility. Split mixed improvements across the
appropriate projects.

## Implemented engine changes

1. Native graph writes use fail-closed policy decisions over validated RDF
   terms. The shared server binds its metadata authority at startup; HTTP
   sessions reuse that connection. CLI and batch writers bind it too.
2. Preconditions and effects share whole-term bindings. Actions and concurrent
   ticks stage their RDF changes, check invariants and policies, and commit
   once. Failed actions discard their staged state. Ramification is computed
   privately before publication; effect counts report actual changes. Snapshot
   rollback releases metadata reads before mutation and replaces the dataset
   atomically, including policy/parse failures.
3. DL class diagnostics retain failed branches and clashes, with bounded output.
   Exhausted class explanation/subsumption searches return an unknown verdict.
4. PDDL domain and problem are compiled together for a finite deterministic
   fragment, including `a` normalization, declared typed constants, reversible
   symbols and delete-wins semantics for aliased action parameters. Unsupported
   constructs or unencoded native guards withhold execution readiness.
5. Application JSON errors become MCP tool errors. Valid negative findings
   remain normal results. CLI, MCP, health, batch and saved packages report the
   same fork build identity. Docker CI passes that identity explicitly.

## Policy and transaction boundary

A server-owned graph checks all its central RDF mutators, including bulk loads,
SPARQL updates, clear/replace and action effects. Targets are subjects of actual
changed quads plus created/dropped named-graph identities. No-op writes need no
authorization. Rules evaluate against the committed **pre-state**, using the
existing deny-overrides/default-allow combination. Input, query and storage
errors abort the change. Blank nodes are bound by stored identity.

SQLite `BEGIN IMMEDIATE` freezes policy configuration and serializes governed
writers while an RDF transaction stages, checks and commits. This is not a
distributed transaction: lineage/audit entries are recorded separately after
commit. A multi-stage tool can still consist of multiple individually guarded
RDF commits. With policies, the implementation scans before/after quads to find
actual changed targets; large-store performance is not qualified here.

Policy administration remains trusted configuration. It is not per-client
identity/tenant authorization, nor a filesystem/network access policy. A standalone
library `GraphStore::new()` remains ungoverned until explicitly bound; bind before
exposing it to concurrent writers. Reuse `StateDb` clones for one shared graph.
Independently opened metadata authorities are rejected.

## Input and planning compatibility

Bindings accept a bare absolute IRI or `<IRI>`, and literals in N-Triples form,
such as `"SN-003"` or `"3"^^<http://www.w3.org/2001/XMLSchema#integer>`.
Use whole `{param}` or `<{param}>` slots. Partial IRI/literal interpolation is
rejected. Existing stored schemas using `"{value}"` or callers passing unquoted
literal strings need an explicit migration. `check_preconditions=false` remains
accepted for compatibility but cannot bypass the transaction's check.

The executable PDDL fragment is finite, default-graph, positive ASK basic triple
patterns and deterministic triple effects. Constants come from actions, initial
facts and goals. General SPARQL, blank-node identity, stochastic effects, OWL
entailment and native guard compilation are not supported. A captured planning
model is advisory: actual apply always checks its live pre-state and policies.
The Rust helper API is now `plan_pddl::compile(domain, schemas, init, goals)`;
the old independent domain/problem helpers could not declare constants correctly.

DL traces are operational diagnostics (512 steps plus a truncation marker,
2048 characters per step), not minimal axiom explanations or checked proofs.
Legacy boolean helper APIs elsewhere are not globally changed to three states.
Optional external proof checkers, planners, models and crosswalk data are not
bundled merely because their tools are advertised.

## Build and adoption

Build from a clean checkout with Docker, Git and Bash:

```sh
bash scripts/build-native-image.sh open-ontologies:my-native-build --platform linux/arm64
```

The helper retains upstream version 2.1.0 and passes `+local.<12-digit-commit>`
and full OCI source revision. A direct Docker build without provenance explicitly
reports `+local.archive`. `NATIVE_BUILDER_IMAGE` optionally selects a prewarmed
builder, whose Rust 1.99.1 compiler is checked. Base tags and apt repositories are
not immutable snapshots, so source/lockfile pins do not promise bit-for-bit
rebuilds; freeze actual image/binary hashes independently.

The user authorized production adoption on 2026-11-05. The deployed engine is
`2.1.0+local.a74f122c8826`, with a matching extracted worker and adapter
`0.2.0+local.234dff0ea4d6`. The complete 141-quad graph and stored action,
policy and invariant registries were preserved. The existing tunnel resumed
and a connected MCP app returned the new engine identity. See
[the deployment record](docs/NATIVE-MCP-DEPLOYMENT-2026-11-05.md) for verification,
remaining limits and rollback. The earlier qualification record remains a
historical pre-deployment snapshot.

The preserved patch files under `patches/proposed-native/` remain byte-for-byte
historical proposals against `cefe5997d045...`. Their diagnostics/version ideas
are implemented in current source; the narrow old PDDL patch is superseded by
the new compiler. They are not reapplied during a Docker build.
