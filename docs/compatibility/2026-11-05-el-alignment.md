# EL classification and alignment follow-up — 2026-11-05 UTC

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

## Report compared with code

| Area | Finding and disposition |
| --- | --- |
| Graph separation | Inline onto_load deliberately bypasses the file registry/cache (`src/server.rs:381`); active:null can coexist with persistent triples. Named graphs are dataset boundaries, not tenant authorization. Existing mixed data cannot be reliably partitioned without import provenance. No live graph migration was attempted. |
| EL timeouts | Classification still called the shared native engine directly. It now uses bounded private snapshots/workers, like defect analysis, with explicit scope and incomplete errors. This bounds work; it does not make full FIBO classification fast or add asynchronous jobs. |
| Optional resources | The prior adapter already reported missing clinical data and optional readiness. Known embedding model/index and compiled-feature errors now gain actionable dependency codes. Resources were not downloaded or packaged; volatile snapshots do not hide mixed-mode tools automatically. |
| Alignment | Native auto-eligibility checks confidence and ambiguity but not evidence coverage (`src/align.rs:978,1105`). Default informative high threshold is 0.95, so a 0.846 row needs a lowered caller threshold to be auto-eligible. A label-only score of 1.0 was independently reproduced with coverage 0.2. The adapter now gates before publication. |
| Error transport | Server error payloads must be distinguished from client facade error labels. Native restart/OOM counters remain zero. Existing MCP error normalization and application_error.code are retained; cloud errors followed by successful calls were tested. |
| Tool surface | Full adapter catalog is 125 tools, including onto_action_info; native full status reports 124. Four frontend profiles already existed. An optional 17-tool core endpoint was added; existing connections remain full. |

Plan: [SCOPED-CLASSIFICATION-ALIGNMENT-PLAN-2026-11-05.md](../SCOPED-CLASSIFICATION-ALIGNMENT-PLAN-2026-11-05.md).

## Deployed identities

- Adapter source: `ec346022ea8f8208a2201b8304e8f9ec603ff536`.
- Adapter version: `0.2.0+local.ec346022ea8f`.
- Adapter binary SHA-256:
  `11e4a9a6547048271c68889a657872ec31c4e52f07fa21334d0f0a5de8b4142c`.
- Native version/source: `2.1.0+local.a74f122c8826`,
  `a74f122c88269ec3b2300aae952d52e7d7befa94`.
- Native image ID:
  `sha256:93989cb0f72dc52eb46d32ccfd0b0203e7a6e6fc1b609cd2920da7eadb23e4b0`.
- Worker SHA-256:
  `a294b0c0b6c7010ea74ed5475e53f9b11ed72b399eda3dd4fbaea60e59a86f20`.
- Generic proxy remains `0.2.0+local.4d7674855d95`; only its exposure policy changed.
- Ubuntu 24.04.4 LTS aarch64, Go 1.26.10 linux/arm64, Docker 29.7.2.

This document is committed after the clean executable build; runtime metadata
records documentation and executable commits separately.

## Behavior and qualification

EL classification accepts ontology_turtle or graph_iri plus max_seconds (1–20,
default 15), with 10,000-row/4 MiB snapshots and 1 MiB inline input. Native
complete/incompleteness remain intact; analysis_complete only indicates that the
selected computation finished. External graph dependencies are not inferred.
The old EL readOnly grant was withdrawn because the new snapshot path uses
cache-aware native reads. The other 38 grants remain unchanged; annotations are
advisory and the client needs a catalog refresh for new arguments/hints.

Alignment is first evaluated with native dry_run=true using existing scores,
weights, cardinality and ambiguity. Auto-eligibility additionally requires
reported coverage>=0.4 and two distinct known informative signal types. Native
review rows are never promoted. Missing/legacy evidence requires review. Native
scores and rounded coverage values are retained, not reinterpreted as probabilities.
This post-selection gate cannot recover alternatives dropped by native cardinality.

Requested live application sends only eligible relation triples through one
additive native inline-load transaction. It bypasses the file registry, preserves
the native policy guard, fails the whole publication on denial and never retries
an unknown outcome. Dry runs publish no RDF; native evaluation can record lineage.
publication_result exposes the native acknowledgment; applied_count counts
confirmed mapping candidates, not necessarily newly inserted triples.

- Formatting, race tests and vet passed. Tests cover native review/ambiguity,
  missing/low evidence, duplicate/unknown signals, invalid mapping terms, unchanged
  scores, dry-run partitions/counts, single publication, policy/transport failures,
  native EL incompleteness, worker cleanup and dependency error classification.
- Disposable QA reproduced native label-only confidence=1.0, coverage=0.2 and
  requires_review=false. The structurally supported control had coverage=0.56
  with four informative signals. Through the adapter, the weak live request
  published zero RDF; the strong request added exactly one mapping. A subsequent
  native deny policy prevented the entire next batch. The policy was QA-only.
- QA seeded 10,010 triples including a five-row named graph. Named-graph EL,
  private classification and out-of-EL incompleteness worked. Oversized full
  selection was refused in about 0.14 seconds. Existing integration, seven private
  analysis modes, 16 concurrent calls and diagnostic/privacy checks passed.
- Production cutover: 13:29:55–13:30:41 UTC. The owning tunnel was drained/reused;
  adapter/proxy readiness gated restart. The native image/worker, credentials,
  Caddy access policy, identity/icons and existing endpoints remained unchanged.
- Before/drained/after full RDF matched: **139,787 quads**, sorted SPARQL-row JSON
  SHA-256 `541405cbb9034880fd79797a6da0ae2aa768508e72563e9bc38fe2b74ca2d39b`.
  This is same-store comparison, not RDFC canonicalization. All three fixed
  policy/action/invariant tables retained one row; combined SHA-256 remained
  `5b67a58d6e4ed0717ad4d4aa6ef56ca705d67c6f136a186972f537c5f154f56a`.
  Production alignment tests were dry-run only; no policies were registered.
- Catalog comparison changed only alignment, EL, search and embed metadata.
  EL gained three optional inputs and lost its old true hint. Full catalog is
  still 125 tools, with 38 readOnly grants; the four old profiles retain their
  counts and /mcp/core exposes exactly the 17 tracked names.
- Public HTTPS verified private EL, preserved incompleteness, evidence-gated
  dry runs and both catalogs. Full live EL selection was refused in about 0.28
  seconds; this timing is a guard response, not completed classification.
- Real connected-app calls verified the global EL refusal, label-only review
  downgrade, missing-model error and subsequent successful status reporting the
  new adapter, 139,787 triples and the core endpoint. The connected client's
  cached old EL schema did not exercise the new private-input arguments; those
  were tested through real HTTP and QA. Gemini's UI was not independently retested.
- Services are active/enabled with zero automatic restarts; native restart/OOM
  counters remain zero. Owned QA services/container were stopped and removed.

## Remaining work and rollback

Additive target_graph imports, provenance-based migration, tenant authorization,
automatic locality-module pipelines and asynchronous jobs need separate native
API/engine design. A graph-name filter alone is not that guarantee. External
model/dictionary/checker packaging also remains separate. Existing native patch
archives are preserved; no native patch was applied or native image rebuilt here.

Rollback backup (binary, proxy configuration, capability inventory and units):
`/data/service-backups/open-ontologies/20261105T132955Z-adapter-el-alignment/`.
Drain/stop the owning tunnel/proxy/adapter, restore the matching binary and both
proxy/inventory files, gate readiness and resume that same tunnel. Do not restore
the database or start a second poller. This is not a cold database backup.

Private evidence: `/data/service-builds/oo-el-alignment-20261105/`.
Frozen binary/source/evidence and checksums:
`/data/service-artifacts/open-ontologies/adapter-el-alignment-ec346022ea8f-20261105/`.
Runtime record: `/home/ubuntu/mcp-services/open-ontologies/ACTIVE-SOURCE.json`.
No GitHub push or external resource installation was included.
