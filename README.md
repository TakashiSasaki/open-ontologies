# Open Ontologies MCP adapter

Keep remote-client interface improvements outside the Open Ontologies engine.
This Go HTTP adapter sits between the generic MCP compatibility proxy and an
version-pinned Open Ontologies HTTP server (unmodified upstream or the selected
native fork). It adds explicit inline and
request-local input forms without patching or rebuilding the Rust application.

Author: 佐々木隆志 (Takashi Sasaki), <takashi316@gmail.com>,
[https://x.com/TakashiSasaki](https://x.com/TakashiSasaki). [MIT License](LICENSE).

The [2026-11-04 verification record](docs/compatibility/2026-11-04.md) fixes the
tested image, source revisions, binaries, protocols and actual verification
boundary. See [integration instructions](tests/README.md) for reproducible checks.
The [2026-11-05 audit and remediation](docs/compatibility/2026-11-05.md) describes
the 0.2.0 policy, SHACL, action and result adaptations and remaining engine work.

```text
MCP client / tunnel → generic mcp-compat-proxy → this adapter → upstream container
Explicit private input → fresh upstream STDIO process → private graph → discard
```

The generic proxy remains in its independent repository and owns modern MCP,
profiles, JSON projection and configurable annotations. Application-specific
semantics and the annotation audit live here. Neither proxy reads tunnel keys.

## Adapted inputs

| Tool | Added behavior |
| --- | --- |
| `onto_convert` | `inline_content` plus source format hint `from` and target `to`. Seven RDF/OWL source formats, direct content output, native OFN loss reports, isolated worker. Existing server path/output calls stay native. |
| `onto_ingest` | `inline_data` with `format` (`csv`, `json`, `ndjson`, `xml`, `yaml`); `dry_run` in a private worker. Normal inline writes publish generated RDF once through upstream `onto_load`. |
| `onto_diff` | `old_content` / `new_content` Turtle; exactly one inline content or server path per side. Existing file/file calls pass through unchanged. |
| `onto_dl_check`, `onto_dl_explain` | Full absolute class IRIs with or without angle brackets. CURIE prefixes are not expanded. |
| `onto_reason`, `onto_defects`, `onto_dl_check`, `onto_dl_explain`, `onto_justify`, `onto_shacl` | Optional `ontology_turtle` in a fresh process. Successful JSON objects include `execution_scope: request` and `persistent_store_modified: false`. Non-JSON reports stay unchanged. |
| `onto_policy_check` | Validate absolute IRIs and bind whole RDF terms in `{target}` or `<{target}>` templates. Invalid inputs/templates and query errors return `indeterminate`, `isError: true`. Valid rules retain deny-overrides/default-allow semantics. Advisory only. |
| `onto_shacl_check` | Check declarations referenced by all shapes, independent of target form, including nested shapes and property paths. Preserve native bound/parse checks; report incomplete discovery/path checks. This is not full SHACL data validation. |
| `onto_action_info` (new) | Inspect action parameters, preconditions, effects and outcomes through a fixed read-only SQLite snapshot. Includes private-simulation and live IRI-binding compatibility diagnostics. |
| `onto_action_applicable` | Safely bind IRI parameters in ASK/SELECT preconditions. Bare and bracketed absolute IRIs work; query failures are errors. A positive advisory result does not mean an incompatible native action can be applied. |
| `onto_plan_validate` | Copy selected deterministic IRI-effect schemas into a private worker, normalize whole-term placeholders and validate against a graph snapshot. Like native validation, all graphs are flattened; goals are checked independently. Shared actions/data are untouched. |
| `onto_plan_compile_pddl` | Use private normalized schemas and a default-graph snapshot. Expand predicate `a` in simple ASK triples. Return `execution_ready: false` with blockers for the incomplete native compiler. |
| `onto_status` | Include adapter identity and an operator snapshot of optional dependency readiness, with timestamps, image identity and tool/mode dependencies. `status: ok` describes core health. |
| `onto_align`, `onto_temporal_query` | Dry-run candidates are separate from actually applied matches; confidence is a score, not an equivalence probability. Temporal scope explicitly excludes the default graph. |

Native top-level JSON error payloads set MCP `isError: true` while preserving
content and metadata. Valid negative findings (`conforms: false`, `ok: false`,
`valid: false`) remain normal results. HTTP session errors and JSON-RPC errors
keep their original status/envelope. The generic proxy still owns structured
JSON projection. Tool-call final SSE responses may be returned as JSON; this
adapter does not expose the native progress event stream.

Inline strings are limited to 1 MiB; generated RDF/upstream responses to 10 MiB.
Default private concurrency is two, with a 75-second deadline including queueing.
Binary XLSX/Parquet ingestion keeps its normal server-side file API.

Each private process gets a new directory, HOME, SQLite metadata file, in-memory
RDF store and sanitized environment. It has no shared-store session or daemon
attachment. The worker is the exact executable extracted from the upstream
image, pinned by SHA-256. No Docker socket/group is granted to this service.
Scratch files are removed on completion and systemd places them under `/run`.

Private calls reject certificate/rules file options. SHACL shapes, dry-run data
and mapping files can be read only from configured backend data roots; path and
symlink escapes are rejected. Inline shapes use `inline: true`; inline mapping
uses `inline_mapping: true`. Paths always refer to the server, not a client VM.
No input is passed through a shell.

Mixed file/inline RDF comparison uses the upstream converter to preserve the
server file's base. Inline Turtle requires absolute IRIs or explicit `@base`.
Inline provenance uses a content SHA-256 identity and hides private worker paths.
Normal inline ingestion stages parsing/mapping/provenance privately, then makes
one shared `onto_load`. A lost final-write reply has an unknown outcome and is
never replayed. Isolation is explicit, not automatic per user/session.

## Engine boundary

The adapter uses upstream algorithms. It does not generate the local fork's
bounded tableau trace, change resource-budget verdict semantics, or invent a
proof when upstream returns `trace: []` / `explanation: []`. Original responses
retain these limitations. Private graphs do not change OWL/SHACL coverage.

The native-only changes are preserved as [upstream patch candidates](patches/open-ontologies/README.md),
with their exact base, regression tests and apply/verification instructions.

## Build and run

Go 1.25+ (preferred toolchain 1.26.10), Git, and a Linux executable
matching host architecture/libraries are required. Go code uses only stdlib.
Python 3 with its standard-library SQLite module is also required for the fixed
registry reader (no additional Python packages). It opens `mode=ro`, enables
`query_only`, reads fixed queries in one snapshot, and never creates tables.

```sh
make check
make build
bin/open-ontologies-mcp-adapter --version
bin/open-ontologies-mcp-adapter --listen 127.0.0.1:18084 --config /etc/open-ontologies-mcp-adapter.json
```

Example configuration (replace the hash with the executable's actual SHA-256):

```json
{
  "backend_url": "http://127.0.0.1:18080/mcp",
  "worker_binary": "/usr/local/libexec/open-ontologies-upstream",
  "worker_sha256": "<64 lowercase hexadecimal digits>",
  "upstream_version": "2.1.0",
  "work_root": "/run/open-ontologies-mcp-adapter/requests",
  "server_data_roots": {"/data": "/var/lib/open-ontologies-mcp-adapter/backend-data"},
  "state_db_path": "/var/lib/open-ontologies-mcp-adapter/backend-data/open-ontologies.db",
  "capability_file": "/etc/open-ontologies-capabilities.json",
  "max_workers": 2,
  "timeout_seconds": 75
}
```

Point the generic proxy at `http://127.0.0.1:18084/mcp`; client/tunnel URLs stay
on the generic proxy. Initialization, sessions, prompts and unadapted calls pass
through. `/health` reports adapter/worker identity and capacity; the generic
proxy's ping reports upstream connectivity.

`state_db_path` defaults to `open-ontologies.db` inside the mapped `/data` root;
missing/unreadable state never yields an allow verdict. Registry inspection is
limited to 256 actions/policies; policy checks to 64 targets and 512 pairs;
private plans to 256 steps/goals and 10,000 snapshot triples. Only complete IRI
bindings/effects are adapted. Literal and partial-IRI action templates require
an engine fix. Existing live action/policy registrations are never rewritten.

`capability_file` is optional. Without it readiness is explicitly unknown.
Build the operator-only `./cmd/inspect-runtime` helper with `CGO_ENABLED=0` and
run it **inside the exact backend container** with `--image-id sha256:...`.
Its JSON records default binary resolution; optional `--remove-self` removes a
temporary helper after inspection. Presence is not an execution/certification
test. Clinical data and model state need explicit application probes, so the
helper reports those unknown. Store the reviewed JSON outside Git and refresh
it after image/config/resource changes. The web adapter needs no Docker access.

## Read-only policy and upgrades

The generic proxy's `tool_annotations` policy sets `readOnlyHint` independently
of the upstream image. Wildcard false covers unreviewed tools; each true grant
is pinned to reviewed input-schema hashes. A mismatch downgrades to false and
logs a warning. A schema hash cannot detect arbitrary engine behavior changes.

`deploy/read-only-tools.json` contains the audit name list, and
`deploy/read-only-policy-2.1.0.json` contains the tested schema grants. Merge the
policy's `tool_annotations` into the generic proxy's existing deployment config.
After explicitly reviewing a new adapted catalog, regenerate its grants with:

```sh
go run ./cmd/pin-annotations --catalog /private/evidence/reviewed-tools.json --reviewed-tools deploy/read-only-tools.json
```

The conservative audit now grants 38 tools read-only status. EL classification's
expanded snapshot mode uses native cache-aware reads, so its old true grant is
withheld rather than inherited by the changed schema. Inline diff uses
only disposable private scratch files. Registry refresh, persistent schema
initialization, shared indexes, optional output and external checker paths stay
false. Annotations are advisory metadata, not access control.

On upgrade, pin the image, extract its matching worker, check system libraries,
review changed behavior/schemas and run isolated integration tests. Replace
backend/worker together after a data backup. Removed profile tool names fail
generic-proxy startup and require review. No automatic image upgrade or automatic
reapproval of read-only grants is performed. Verification and immutable hashes
go in `docs/compatibility/`; requests/credentials/data/logs stay outside Git.

The adapter refuses catalog import when the backend's `onto_status` version
differs from `upstream_version`, and checks that each worker reports that same
engine version. This prevents silently mixing an upgraded backend with an old
private engine; a compatible upstream update needs configuration/worker refresh,
not a patch/rebuild of the Rust source.

## Native fork deployment

`native_action_semantics: true` forwards policy checks, action applicability and
plan validation/compilation to the pinned native engine. This preserves native
RDF literal binding and executable PDDL results. Leave it false for the legacy
unmodified engine. Always qualify the exact backend version and extracted worker
hash together; interface-only adaptations remain in this adapter.

## Inline conversion and bounded refusal diagnostics

Follow-up plan: [2026-11-05](docs/GEMINI-FOLLOWUP-PLAN-2026-11-05.md).
For example:

```json
{
  "inline_content": "@prefix ex: <https://example.org/> . ex:Cell a <http://www.w3.org/2002/07/owl#Class> .",
  "from": "turtle",
  "to": "rdfxml"
}
```

Inline conversion stages a file only inside a disposable native worker, returns
an object with `content`, `format`, `source_format_hint`, `execution_scope` and
`persistent_store_modified`, and never loads the shared graph. Native source
sniffing still applies to the format hint. Source formats are Turtle, N-Triples,
RDF/XML, N-Quads, TriG, JSON-LD and OFN; supported output names remain native.
OFN reports retain `constructs_not_exported` and round-trip diagnostics. A graph
output may flatten named graphs or exclude the native inferred graph; choose a
dataset output such as N-Quads/TriG when graph identity matters. This adapter
does not certify losslessness or change the converter's algorithms.

Supply exactly one of `path` and `inline_content`; `from` is for inline mode only.
Inline mode rejects `output` paths and is bounded by the existing size/worker
limits. It rejects file IRIs anywhere in the parsed dataset, including datatype
and graph IRIs. Relative IRIs must resolve against an explicit non-file base
(`@base`, `xml:base`, or JSON-LD context `@base`); it does not silently substitute
an invented public namespace or expose scratch file identities. Intentional file
IRIs can still be converted using normal server path mode. A literal containing
text such as `file:///data/example` is not itself an IRI and is retained.

The entire `onto_convert` tool remains conservatively non-read-only because
ordinary file mode still supports native output files. No read-only grant was
added or broadened by the inline mode.

For native `policy_denied` errors larger than 8 KiB, the adapter summarizes a
valid deny report using exact distinct evaluated/blocked target counts, total
rule evaluations, up to three bounded examples, omitted-result count, original
error byte length and SHA-256. It retains MCP `isError: true`, metadata and
structured-content consistency. It never retries the mutation or disables a
policy. Small errors, query errors and advisory policy deny verdicts retain their
original contracts. This reduces the client payload, not the engine's policy
checking/report-building cost, and does not claim rollback of every higher-level
registry effect. Full native errors are not copied into client summaries or logs;
logs retain only counts and a digest.

An unconfigured `onto_repo_list` remains an error with
`error_code: repository_not_configured`, `configuration_required: true` and
configuration/cache-list guidance. The adapter does not search compilation
caches or silently widen filesystem exposure. Operators can explicitly configure
`OPEN_ONTOLOGIES_ONTOLOGY_DIRS=/data/ontologies` in the backend container and
populate that source directory under the persistent data mount.

The solver inventory still reflects the actual backend container. Installing
Vampire/Z3 on the host alone does not make them available to its native process.
FOL export and theorem references do not establish that a proof was independently
checked. An optional image with tested native solvers is separate future work.

## Safe adapted-call diagnostics

[Diagnostics plan](docs/DIAGNOSTICS-PLAN-2026-11-05.md) records the log-audit finding
and deployment boundary. Existing INFO `adapted MCP call` summaries now include
`adapter_version` and `is_error`, in addition to `adapter_error`, duration and
result size. `adapter_error` means a Go error; `is_error` also covers recognized
MCP tool errors returned without a Go error. Negative findings remain successful.

Failure records add `error_code`, `error_stage`, `error_ref`, `error_sha256`,
`error_bytes` and `context_state`. The reference is generated by the server, not
copied from a caller ID, and is not added to MCP responses. SHA-256 is computed
from the error text before JSON encoding; it is not a hash of the input ontology.
Variable values/paths in errors can change that digest between requests.

Categories distinguish validation/size/file-IRI rejections, native conversion
operations, workspace I/O, worker startup/I/O/protocol/RPC/exit/output/identity,
and interrupted worker deadlines/cancellation. Conversion stages distinguish
arguments, staging, input preflight, IRI checks and output rendering.
`native_tool_error` in `conversion.parse_input` means that native preflight
failed, not proof that the client submitted malformed RDF. No native free-text
error is heuristically promoted into a more specific root cause. Other failures
remain `unclassified_error` or coarse `tool_execution_error` when appropriate.

Only allowlisted format hints and adapted tool names enter logs. No raw arguments,
RDF, error text, arbitrary format/tool strings, client request/session IDs or file
paths are logged by this diagnostic record. Unknown formats are `unrecognized`.
Existing HTTP/MCP payloads, schemas, error messages and read-only hints are not
changed. No additional backend calls or retries are made by logging. Pass-through
calls and requests rejected by a connector before server delivery are outside
this adapted-call stream. Existing retention remains in the host log collector.

```sh
journalctl -u open-ontologies-mcp-adapter --since '1 hour ago' | rg 'is_error=true'
```

The earlier 10:19:24 UTC failure has no historical reason/hash to recover. These
fields support diagnosis of subsequent failures; they do not retroactively
identify that request's input or cause.

Proxy transport/response-decoding failures for tool calls also emit
`MCP upstream response failed` records through the same safe schema, distinguishing
`backend_output_limit`, `backend_read_error`, `backend_protocol_error` and
`backend_transport_error`. The 10 MiB MCP response cap is preserved. A known
size-limit failure is returned as a bounded MCP tool error with
`error_code: backend_output_limit`, `execution_outcome: unknown` and
`retry_safe:false`, preserving its request ID. It does not assert a policy denial
or successful mutation. Other transport/protocol/session HTTP failures retain
their HTTP behavior. A full SELECT on the 139,781-quad live store exceeded the cap
in the baseline test; this is not evidence of server corruption. Use bounded
queries/aggregates for MCP, and a bounded loopback administrative snapshot for
operator full-data comparisons. Ordinary pass-through semantic results and
session HTTP errors are not turned into a new complete audit stream.

## Bounded defect analysis and clinical dependencies

`onto_defects` runs the pinned native algorithm in a separate disposable worker.
Supply `ontology_turtle` for an explicit document, or `graph_iri` for one exact
named graph; omit both for a snapshot of all graphs flattened together. A snapshot
uses one SPARQL read, at most 10,000 selected rows and 4 MiB Turtle; larger scopes
return `analysis_scope_required`. Inline Turtle remains limited to 1 MiB.
`max_seconds` is an integer from 1 to 20 (default 15), including worker capacity,
snapshot and execution. The native `limit` parameter still only limits listed
findings. Named-graph analysis excludes other graphs and their dependencies.

Successful `complete:true` means the selected dataset's eight native declaration
checks finished, not that the whole ontology is OWL-consistent. The original
native `truncated` field concerns the findings listing. A timeout returns
`analysis_deadline_exceeded`, `complete:false` and no invented partial findings;
its process is killed/reaped and scratch removed. Existing shared triples and
policies are never cleared/reloaded to perform this analysis. Full large-store
analysis, genuine partial progress and native policy-report construction still
require engine work. See [the plan](docs/SCALABILITY-PLAN-2026-11-05.md).

Clinical mapping load failures keep `isError:true` and gain
`error_code:dependency_unavailable` and `dependency:clinical_crosswalk`. An empty
successful lookup remains an empty mapping result. `inspect-runtime` observes
`data/crosswalks.parquet` relative to the backend container's working directory,
distinguishing missing, unknown and present-unverified. It never equates file
presence with correct Parquet, clinically validated mappings or a tested lookup.
Run it with the exact `--image-id` and observed `--native-version`.

Application errors with a service `error_code` also retain that category in
`application_error.code`. A real connected-app call showed its facade replacing
the structured result's top-level `error_code` with `INVALID_ARGUMENT`, while the
text content retained `dependency_unavailable`. The nested category avoids this
collision. Existing upstream `application_error` extensions are preserved.

## Scoped EL classification and evidence-gated alignment

`onto_classify_el` accepts the same `ontology_turtle`, `graph_iri` and `max_seconds`
selection/budget controls as bounded defect analysis. Native `complete` and
`incompleteness` remain authoritative; `analysis_complete:true` only means the
selected computation finished. Oversized selections/timeouts are incomplete
application errors, never a manufactured complete classification.

`onto_align` always evaluates with native `dry_run:true` before RDF publication.
Auto-eligibility requires native non-review/unambiguous status,
`evidence_coverage>=0.4` and at least two distinct known `informative_signals`.
Missing legacy evidence, label-only matches and unsupported mapping terms move
to `borderline`. Scores stay unchanged. This post-selection gate cannot recover
alternatives already removed by native cardinality selection.

Requested live application publishes eligible triples through one additive native
inline-load transaction, preserving the policy guard. A denial fails the batch;
lost replies report unknown outcome and are never retried. Dry runs publish no
RDF, though native evaluation can record lineage. `auto_apply_candidates` lists
eligible rows, `auto_applied` published rows, and `evidence_policy` explains the gate.

Merge [deploy/core-profile.json](deploy/core-profile.json) into the generic proxy's
configuration for an optional `/mcp/core` catalog of 17 tools. Existing `/mcp`
and the four profiles remain available. `onto_status.tools` describes the full
adapter; use `tools/list` at the chosen endpoint for its actual catalog. Operator
capability metadata can list configured endpoints. Model/index errors include
setup guidance; volatile snapshots do not silently hide mixed-mode tools.

Named graphs provide dataset scope, not tenant authorization. Inline loads bypass
the native file cache, so `active:null` can coexist with populated persistent data.
This adapter does not retrospectively partition imports or implement a named-graph
writer by replacing/restoring the live dataset. See the
[plan](docs/SCOPED-CLASSIFICATION-ALIGNMENT-PLAN-2026-11-05.md).
