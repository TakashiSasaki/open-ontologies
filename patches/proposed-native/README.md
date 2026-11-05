# Preserved upstream patch candidates

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

These files preserve native fixes and future submission candidates. Some have
adapter workarounds; engine diagnostics still require native changes.
They are published as proposals in the TakashiSasaki fork; no upstream issue
or pull request has been made. These files do not alter the working source or Docker build.
The adapter's existing `v0.1.0` tag remains the original deployment checkpoint.

Current-source note (2026-11-05): the native development branch now implements
these diagnostics/version ideas and a broader PDDL compiler against the newer
`375fbc4789debfb57e9c7dfc6bf0e8db8fb21f9b` base. Read [FORK.md](../../FORK.md)
and [native verification](../../docs/NATIVE-MCP-VERIFICATION.md) for that separate
source/image qualification. The archived patch bytes, base and historical
verification statements below remain unchanged; do not apply them again to the
modified native source.

Target repository: <https://github.com/fabio-rovai/open-ontologies.git>.
Exact base: **`cefe5997d04553ac495873e8aa1046c1d6b85f7d`**, upstream 2.1.0.
Extraction source: local fork commit
`b8cb6ce7d72a82811f3268c951857d3ccb279d1e`. The local native Git history is
retained separately. [manifest.json](manifest.json) records provenance, candidate
commit/tree/blob identities and verification; [SHA256SUMS](SHA256SUMS) fixes the
patch bytes. Patches 0001/0002 use `git format-patch`; 0003 is a standalone
unified diff for `git apply`, as detailed below.

| Patch | Preserved behavior | Native files |
| --- | --- | --- |
| [0001](0001-tableaux-diagnostics-and-unknown-verdicts.patch) | Named, bounded tableau diagnostics, retained failed-branch/clash steps, and unknown JSON verdicts on exhausted budgets. Includes three regression tests. | `src/tableaux.rs`, new `src/tableaux/diagnostic_trace_tests.rs` |
| [0002](0002-cli-version-flag.patch) | Root CLI `--version` support through clap using the package version. | `src/main.rs` |
| [0003](0003-pddl-rdf-type-shorthand.patch) | SPARQL predicate `a` becomes the same `rdf:type` term as an explicit IRI in simple ASK-to-PDDL translation; includes a regression test. | `src/plan_pddl.rs` |

**0003 is a standalone `git apply` unified diff, not a format-patch mailbox.**
It was authored during the 2026-11-05 audit. Apply/reverse was checked against
the exact base in a private Git index. Its module tests were checked in an
isolated Rust harness with lightweight stand-ins for the action data types;
the full upstream crate has not been rebuilt or tested for this candidate.
It does not make the PDDL stub complete: domain constants, reverse symbol maps,
collisions, RDF literals and general SPARQL still require engine design work.
The adapter performs this narrow normalization in private action copies today.

```sh
git apply --check "$PATCH_DIR/0003-pddl-rdf-type-shorthand.patch"
git apply "$PATCH_DIR/0003-pddl-rdf-type-shorthand.patch"
cargo test --release --locked --features embeddings,plugins,sql --lib plan_pddl::tests
```

Atomic policy enforcement across registry lookup and graph mutation is also
still an engine responsibility. No speculative transaction patch is included:
SQLite metadata and the RDF store do not share a transaction boundary, and a
proxy precheck or mutex cannot serialize native background/alternative writers.

Patch 0002 is independent of the tableau change and may be considered separately.
It is reconstructed from the old local version flag in an upstream-friendly
form: `CARGO_PKG_VERSION`, without the fork-only `build.rs` / `+local` identity
scheme, Cargo packaging edits, or health/startup version rewrites.

## Engine/API review notes

Patch 0001 reports operational diagnostics: labels, unfolding, conjunctions,
failed alternatives/clashes and outcomes. It caps recorded steps to 512 plus one
truncation marker, and each recorded step to 2048 characters. These output caps
do not constitute a proof, a minimal source-axiom explanation, or a bound on all
intermediate allocations. It does not change the engine's supported OWL fragment.

In the `explain_class` and `check_subsumption` JSON wrappers, budget exhaustion
becomes `satisfiable: null` or `subsumed: null`, with `complete: false` and
`reason: "budget_exhausted"`. Clients that assumed an unconditional boolean must
handle this third state. Determinate results retain booleans. Existing legacy
boolean/Option helper signatures are not converted to tri-state APIs globally.

The extraction deliberately omits IRI angle-bracket normalization, inline
ingestion/diff, request-local graph plumbing, annotations, protocol translation,
and deployment/version-provenance code. Those are handled by the current adapter
and generic proxy. The trace regression module/test names and a branch hunk's
indentation were normalized; the engine behavior is taken from the source fork.

## Apply and validate for a future submission

Use a separate upstream checkout at the exact base; keep the live service's
unmodified checkout and image intact. Check payload integrity in this directory:

```sh
sha256sum -c SHA256SUMS
```

In that upstream checkout, set the location of these files and apply the series:

```sh
PATCH_DIR=/path/to/open-ontologies-mcp-adapter/patches/open-ontologies
git rev-parse HEAD
git apply --check "$PATCH_DIR/0001-tableaux-diagnostics-and-unknown-verdicts.patch"
git apply --check "$PATCH_DIR/0002-cli-version-flag.patch"
git am "$PATCH_DIR/0001-tableaux-diagnostics-and-unknown-verdicts.patch" "$PATCH_DIR/0002-cli-version-flag.patch"
```

Suggested checks before submitting (not run for this archive-only extraction):

```sh
cargo test --release --locked --features embeddings,plugins,sql --lib diagnostic_trace_tests
cargo test --release --locked --features embeddings,plugins,sql --test tableaux_test
cargo run --release --locked --features embeddings,plugins,sql -- --version
```

Confirm the actual integration-test target name in the chosen checkout before
running a broader suite. For a newer upstream base, rebase/review and rerun the
checks; the preserved base revision is not an assertion of current upstream HEAD.

Archive verification applied both exported patches to a private index, compared
the exact resulting tree, then reversed them back to the exact base tree, with
`--whitespace=error-all`. No native branch/worktree or deployed binary changed.
The original source fork passed its three trace regressions earlier on
2026-11-04; the newly extracted candidate has **not** been recompiled, so that
historical run must not be treated as fresh validation of this exact series.

The contributions are MIT-licensed under the adapter repository's LICENSE.
[UPSTREAM-LICENSE](UPSTREAM-LICENSE) preserves the upstream copyright and terms.
