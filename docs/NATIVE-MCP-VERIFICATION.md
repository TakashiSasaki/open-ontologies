# Native MCP qualification — 2026-11-05 UTC

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

This records the pre-deployment Linux arm64 qualification. The subsequent
user-authorized production adoption is recorded separately in
[NATIVE-MCP-DEPLOYMENT-2026-11-05.md](NATIVE-MCP-DEPLOYMENT-2026-11-05.md).
Cloud results below describe the original qualification boundary.

## Frozen identities

| Item | Identity |
| --- | --- |
| Upstream source base | `375fbc4789debfb57e9c7dfc6bf0e8db8fb21f9b` |
| Tested native source | `a74f122c88269ec3b2300aae952d52e7d7befa94` |
| Runtime/CLI/MCP version | `2.1.0+local.a74f122c8826` |
| Local image tag | `open-ontologies:2.1.0-native-a74f122c8826` |
| Local OCI index/image ID | `sha256:93989cb0f72dc52eb46d32ccfd0b0203e7a6e6fc1b609cd2920da7eadb23e4b0` |
| Saved image tar SHA-256 | `b9d62b5df5f8faf65aefd49805d6e9a98ceab3edc2de66f1c142942d55cf9d74` |
| Native executable SHA-256 | `a294b0c0b6c7010ea74ed5475e53f9b11ed72b399eda3dd4fbaea60e59a86f20` |
| Cargo.lock SHA-256 | `86d63446f344f58388e448ce5f95cf951731d4ca51548b51bcfaf42eb21f949b` |
| Actual prewarmed builder | `sha256:6eb6778e35101b5301b6bb1b56513b4f5c7f87d534ba91b36b2991ed1ee1665c` |
| Distroless base index | `gcr.io/distroless/cc-debian12@sha256:e5d81ddde149641e2a9ba55be4545bc125c67de07508b03ba4c22e6eb0ded5aa` |

The application image is a **local Dockerfile build**, not a published upstream
application container. `image.tar`, the extracted executable and full image
inspection are preserved on the operator's host at
`/data/service-artifacts/open-ontologies/native-a74f122c8826-20261105/`.
They are outside Git. Later documentation commits do not change the tested
executable-source identity above. No native release tag is implied.

## Environment

- Host: Ubuntu 24.04.4 LTS, aarch64, kernel `6.17.0-1011-oracle`.
- Docker Engine: `29.7.2`, Linux arm64; build used `--platform linux/arm64`.
- Rust: `1.99.1` (`58cf8f9c3`, 2026-10-13), release profile, committed lockfile.
- Features: `embeddings,plugins,sql` (SQL includes DuckDB/PostgreSQL).
- Oxigraph: `0.5.13`, upstream's patched revision
  `37db8e1522585ed2f9aa994be6c5c716859af5c8`; rmcp `1.4.0`.
- Other locked components: DuckDB Rust crate `1.10506.1`, wasmi `1.1.0`,
  tract-onnx `0.22.3`. No dependency/lockfile update was made by these fixes.
- QA server: UID/GID `1001:1001`, persistent storage, fresh synthetic data,
  localhost port `18082`, 1 CPU / 2 GiB / 256 PID limit. Container port 8080
  used `serve-http --host 0.0.0.0 --port 8080 --data-dir /data`.
- Compatibility bridge: localhost `18083`, version
  `0.2.0+local.4d7674855d95`, executable SHA-256
  `dbaa7846e777e3be7f4d9a1213d6f8d1288cdd5c7200f2c197b550362035ffbd`.
  This test used the generic bridge directly; the production dedicated
  adapter's pinned legacy worker was not silently reused.
- MCP: backend initialization/STDIO `2025-06-18`; modern bridge discovery
  `2026-07-28`. Catalogs matched: 124 native tools, 6 prompts.
- Fast Downward: source `ed3d389bf39eaa4427ae0b26ce7136546732e19f`,
  release build on arm64; planner executable SHA-256
  `3edd084856f7d6b668856b105529932ab6b39a4d2b3e16a8c110ba6e2f2974ce`.
  Source tar SHA-256:
  `1c344ed7bc76cc14dfc19601f6c8990126d5d49be782d1c901fc35eb00e9fdbe`.
  This is a development-only external oracle, not a tool shipped in the MIT
  application image. No LP solver or VAL verification is claimed.

## Checks and reproductions

| Check | Result |
| --- | --- |
| `cargo clippy --release --locked --features embeddings,plugins,sql --all-targets -- -D warnings` | Passed at the tested source; examples included |
| Complete native Rust test suite, same feature/profile set | Passed: 2,308 tests, 0 failed, 2 explicitly ignored across 250 result groups |
| Focused new unit/transaction regressions | Final source: 469 unit tests; 45 focused regression tests; all included in the successful aggregate suite |
| Selected-file rustfmt, toolchain pins, shell syntax, workflow YAML | Passed |
| Updated three-layer planner example | Passed; native validation/certification/apply and derived goal both succeeded |
| Actual Dockerfile arm64 build and `--version` | Passed; fork identity matches OCI revision/version labels |
| Native HTTP MCP and STDIO | Passed, correct identity and 124 tools |
| Modern bridge vs native catalog and `onto_status` | Passed, catalog schemas/descriptions preserved |
| Multi-session shared policy authority | Passed; second session does not disable future writes |
| Policy invalid input/query; denied load and clear | Passed, MCP `isError: true`, no partial RDF writes |
| Snapshot save/change/rollback with a protected organization | Passed; 10 → 11 → 10 quads, organization retained, no metadata-lock stall |
| Valid negative syntax finding | Passed, `valid: false` remains a normal tool result |
| Bare/bracketed class IRIs, DL explanation/check | Passed, nonempty bounded diagnostics |
| Literal action binding and actual apply counts | Passed |
| Generated PDDL → Fast Downward → reverse symbols → native private plan validation | Passed; goal satisfied without modifying shared data during validation |
| Aliased add/remove parameters | Passed; planner reports the delete-wins goal unreachable, exit 11 |
| Restart and REST write before any MCP session | Passed; persisted policy blocks mutation; startup logs confirm no MCP initialization yet |

The concrete solver roundtrip and MCP cases live in
[`scripts/qualify-native-mcp.py`](../scripts/qualify-native-mcp.py). It requires
an explicit `--allow-disposable-data` and an initially empty graph/action/policy
registry. Never point this write suite at production. Supply the generic proxy
repository's `scripts/check-http.py` as `--client-script`, and the pinned
Fast Downward `fast-downward.py` as `--planner`.

Native Rust acceptance command (inside the isolated builder):

```sh
OPEN_ONTOLOGIES_BUILD_VERSION=2.1.0+local.a74f122c8826 \
CARGO_TARGET_DIR=/out/target CARGO_BUILD_JOBS=4 \
cargo test --release --locked --features embeddings,plugins,sql --no-fail-fast
```

The builder mounts the shared Git metadata read-only at its original path, so
Git-backed corpus/fixture checks can inspect the real index. Earlier runs without
that mount failed those gates rather than proving anything. The complete suite
also exposed a rollback self-deadlock; the final source releases the metadata
guard and replaces the dataset atomically, with a new bounded regression.

Log and fixture evidence is preserved at
`/data/service-builds/open-ontologies-native-20261105/`: final image build,
`native-clippy-a74f122.log`, final test log, `mcp-a74f122/`,
`modern-a74f122.json`, `stdio-a74f122.json`, `rest-a74f122.json` and startup
logs. Intermediate failed/superseded development attempts are also retained;
they are not acceptance results. All graph/action/policy examples are synthetic.

## Inherited limits and adoption

Configured `cargo audit` 0.22.2 passed against advisory DB commit
`965bf9ad083b58e6340d65829d85217b18be9290`. Upstream's `.cargo/audit.toml`
already ignores `RUSTSEC-2026-0194` and `RUSTSEC-2026-0195` in quick-xml 0.37.5.
An audit without that configuration still reports both; four unmaintained-crate
warnings remain. The lockfile and ignore list are unchanged. This is not a
claim of zero dependency vulnerabilities; XML dependency migration remains work.

The two explicitly ignored cases need the external ORE 2015 corpus or run a
wall-clock benchmark over thirty ontologies. Other upstream tests can return
early when optional proof fixtures/tools are unavailable; aggregate counts do
not establish positive external-kernel verification.

External proof checkers, embedding model assets, crosswalk data and general
OWL/SPARQL-to-PDDL completeness are not qualified by the two solver fixtures.
Named-graph/temporal planning and tenant isolation are not established here.
Policies still use trusted administrator-supplied rules; [FORK.md](../FORK.md)
defines the RDF/audit transaction boundary and action-template migration.

Production is still the older unmodified `cefe5997d045...` engine. Deploying this
candidate requires the matching adapter worker/version/hash pins, an inspected
stored-action migration, cold backup, isolated chain tests and rollback plan.
No host reboot, production replacement or cloud-connector reconfiguration was
performed as part of this qualification.
