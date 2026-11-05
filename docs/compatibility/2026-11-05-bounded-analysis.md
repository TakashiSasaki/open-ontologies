# Gemini report analysis and deployed improvements — 2026-11-05 UTC

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

## Evidence compared with the report

| Reported issue | Code/log evidence | Result and remaining boundary |
| --- | --- | --- |
| Parse errors, absent crosswalks and policy refusals crash/restart the server | Native conversion/clinical handlers return error JSON. `src/clinical.rs:53` propagates file/Parquet errors. Live native restart/OOM counters and system automatic restart counters are zero; no native panic match was found in the reviewed logs. | A client CONNECTION_CLOSED display alone does not establish a native crash. Direct and real cloud error-followed-by-success calls were verified. |
| Full-store defect checking times out | Proxy recorded onto_defects timing out at 10:23:19 after 90 seconds. `src/defects.rs` collects all findings before its display limit is applied. | The adapter now bounds snapshots and isolates/cancels the unchanged native algorithm. This is bounded execution and scoped analysis, not a faster full FIBO algorithm. |
| Large policy refusals may lose their response | `src/graph.rs` evaluates all affected targets and serializes the full report before the adapter can summarize it. A reply exceeding 10 MiB hits the adapter cap. | A known cap hit now returns a bounded MCP tool error. It reports outcome unknown and never retries or invents a deny verdict. Native report construction remains engine work. |
| Optional solvers and clinical data are ready because tools are advertised | Runtime inventory distinguishes core status from optional readiness. The actual container's fixed clinical mapping path is absent. | Crosswalk failure is explicitly classified and inventory now says missing. Solvers/data were not installed; skipped proof execution is not a verified proof. |

The implementation plan is [SCALABILITY-PLAN-2026-11-05.md](../SCALABILITY-PLAN-2026-11-05.md).
No additional native panic handler was justified by these observations. The
clinical error still describes a failed load, not a successful empty mapping.

## Deployed identities

- Adapter executable source: `3ae8eb9f5d0e74be4058dc1a35973c736b196496`.
- Adapter version: `0.2.0+local.3ae8eb9f5d0e`.
- Adapter binary SHA-256:
  `fadcda850c184f7889bb8765c266b53488c9c40dfca2fa09e4cf62aea5a24db6`.
- Native version/source: `2.1.0+local.a74f122c8826`,
  `a74f122c88269ec3b2300aae952d52e7d7befa94`.
- Native image: `open-ontologies:2.1.0-native-a74f122c8826`, ID
  `sha256:93989cb0f72dc52eb46d32ccfd0b0203e7a6e6fc1b609cd2920da7eadb23e4b0`.
- Extracted worker SHA-256:
  `a294b0c0b6c7010ea74ed5475e53f9b11ed72b399eda3dd4fbaea60e59a86f20`.
- Generic proxy: `0.2.0+local.4d7674855d95`; tunnel client:
  `0.0.19+01d67bb4da0d8359b808b4761f8db171ab682cd7`.
- Host: Ubuntu 24.04.4 LTS, aarch64; Go 1.26.10 linux/arm64;
  Docker 29.7.2. Native container working directory: `/`.

The native image/worker, generic bridge, public routes, identity/icons, profiles
and credentials were retained. Only the application adapter and the operator's
capability inventory changed. The source was clean when built; this verification
document's later commit is recorded separately from the executable source.

## Interface behavior

`onto_defects` now accepts `graph_iri` and `max_seconds` in addition to its
existing optional `ontology_turtle` and native display `limit`.

- Inline Turtle is at most 1 MiB and belongs to one private request.
- Without inline input, one SPARQL query captures at most 10,000 rows and 4 MiB
  Turtle. Omitted graph scope selects the union of all graphs, flattened like
  native defect analysis. An excessive scope is refused, not silently sampled.
- `graph_iri` selects one exact named graph; other graphs and their dependencies
  are excluded. Empty selection is explicitly reported as zero rows. Namespace
  substring filtering was not substituted for a well-defined dataset boundary.
- The total budget, including capacity/snapshot/worker time, defaults to 15
  seconds; `max_seconds` accepts integers 1–20. Expiry returns
  `analysis_deadline_exceeded`, cancellation returns `analysis_cancelled`, and
  both report `complete:false` without invented partial findings.
- A successful `complete:true` applies to the selected dataset's eight native
  declaration checks, not global OWL consistency. Native listing truncation is
  retained separately. The private process is reaped and its workspace removed.
  No analysis results are published to shared data. Snapshot acquisition uses
  the native read tool and retains its ordinary lazy-reload behavior.

Known adapter response-size failures return HTTP 200 with a valid JSON-RPC
CallToolResult, `isError:true`, `backend_output_limit`,
`execution_outcome:unknown` and `retry_safe:false`. The 10 MiB bound remains.
Other transport/session/protocol HTTP failures retain their existing behavior.
The adapter cannot infer a native mutation verdict from a lost oversized reply.

Clinical errors gain `dependency_unavailable` plus configuration guidance. The
operator inspector reads the fixed `data/crosswalks.parquet` path in the selected
container and distinguishes missing from present-unverified/unknown. Presence
does not establish valid Parquet/schema/provenance or a tested clinical mapping.

Real connected-app testing found that the facade replaces the structured result's
top-level service `error_code` with `INVALID_ARGUMENT`. The server's text content
retains the original category. Application errors now also retain it at
`application_error.code`, verified through the actual cloud facade. Existing
upstream extensions at that key are preserved.

## Qualification and production evidence

- Formatting, `go test -race ./...` and `go vet ./...` passed on the code committed
  as the release source. Unit tests cover snapshot row/byte bounds, unsafe
  scopes/budgets, blank-node links, cancellations/deadlines, worker cleanup,
  application-code preservation, and one execution/no retry for an oversized
  simulated mutation reply. Other transport errors still return their HTTP 502.
- Disposable QA used the exact native image and worker. Initial feature testing
  used de2f4a8; the final release candidate repeated integration, diagnostic and
  scope/error checks after cancellation and cloud-projection refinements.
- QA policy refusals and seven-format conversions passed on a separate synthetic
  dataset. The scoped-analysis fixture had 10,009 triples including a small
  named graph and blank-node link. Scope/refusal/private tests retained its full
  comparison or count as appropriate; 16 concurrent private calls passed.
- A private 1,000-class cycle hit a one-second budget and returned an incomplete
  MCP error in approximately 1.03 seconds on the final candidate. A subsequent
  status call worked and scratch was empty. No full native progress was invented.
- An independently loaded 11 MiB literal verified bounded MCP cap errors on the
  final candidate. No cap increase or automatic mutation replay occurred.
- Initial QA setup corrected an unsupported `--bind` launch flag to the actual
  `--host` option. Policy-refusal and file-replacement fixtures were separated
  into fresh QA stores; file loading replaces the QA dataset, so named-graph
  checks preceded the large-literal replacement. These were fixture/setup issues,
  not production modifications. Owned QA processes/container were removed.
- Production was first updated to f563d9105dc1 at 11:41:21–11:42:05 UTC, then to
  the final projection fix at 11:48:20–11:49:04 UTC. Both cutovers drained and
  reused the owning tunnel and gated dependent readiness. The native container
  continued running from 06:53:48 UTC with zero restarts/OOM events.
- Production before/drained/after data matched: **139,781 quads**, SHA-256
  `344bfa57e2609c7d7aaa04e7a6584dd0fbafa9cfae411121a5058d5c8f4f9252`.
  This is sorted SPARQL-row JSON comparison of the same running store, not RDFC
  canonicalization. The complete administrative snapshot was approximately
  34.1 MB and was read through a bounded loopback API, not the MCP response cap.
- Policy/action/invariant tables each retained one row; sorted fixed-table JSON
  hash remained `5b67a58d6e4ed0717ad4d4aa6ef56ca705d67c6f136a186972f537c5f154f56a`.
  No production clear, policy registration or dataset replacement was tested.
- The catalog comparison changed only onto_defects (description/two optional
  inputs) and onto_crosswalk (description). Every annotation and unrelated
  schema stayed equal: 125 tools, 39 reviewed read-only grants, all profiles
  retained. No changed schema inherited a new read-only grant.
- Production private integration, diagnostic/privacy checks and scoped refusal
  passed. Public HTTPS rejected the oversized global analysis scope in about
  0.28 seconds, without running a full defect analysis; missing crosswalks and
  malformed RDF returned errors, followed by valid conversion and status calls.
- Real connected-app crosswalk errors retained `application_error.code`, followed
  by successful private defect analysis and onto_status reporting the final
  adapter, 125 tools and 139,781 triples. Gemini's UI itself was not re-tested.
  Refresh its tool definitions before using the new optional arguments.

## Remaining engine work, rollback and artifacts

Full large-ontology defect throughput, genuine per-kind partial progress and
cooperative cancellation inside native queries remain engine responsibilities.
The proxy cannot stop native policy-report allocation/evaluation already in the
shared server. A native bounded-serialization proposal is preserved at
[proposed-native-a74f122c8826](../../patches/proposed-native-a74f122c8826/README.md).
Its exact-base apply check passed; it is **not compiled, applied or deployed**.
It still retains/evaluates the full native rule vector. The older native patch
archive remains intact. Optional solver/data packaging also remains separate.

Whole-task baseline rollback executable/configuration/inventory backup:
`/data/service-backups/open-ontologies/20261105T114121Z-adapter-bounded/`
(adapter 5df3a0ce71bd). The final cutover also backed up intermediate adapter
f563d9105dc1 at
`/data/service-backups/open-ontologies/20261105T114820Z-adapter-bounded/`.
Use executable versions/hashes from the cutover records; runtime metadata was
finalized after both stages. Neither backup is a cold database backup.

For rollback, drain/stop the owning tunnel/proxy/adapter, restore the matching
adapter binary and capability inventory, gate adapter/proxy readiness, then
resume the same tunnel. Do not restore the database or launch a second poller.

Private evidence: `/data/service-builds/oo-bounded-20261105/`.
Frozen binary/source/evidence and SHA-256 manifest:
`/data/service-artifacts/open-ontologies/adapter-bounded-3ae8eb9f5d0e-20261105/`.
Runtime identities: `/home/ubuntu/mcp-services/open-ontologies/ACTIVE-SOURCE.json`.
No GitHub push, native rebuild or external solver/data installation was performed.
