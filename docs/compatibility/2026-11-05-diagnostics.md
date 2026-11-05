# Private failure diagnostics — 2026-11-05 UTC

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

## Deployed identities and environment

- Adapter executable source: `5df3a0ce71bdaaff0e12a88c6962207b75cd7618`.
- Adapter version: `0.2.0+local.5df3a0ce71bd`.
- Adapter binary SHA-256:
  `6f28a3023a4e6454cccd3110a6933a9e8272f7978778272553ffabf856b3791b`.
- Native engine/worker: `2.1.0+local.a74f122c8826`, source
  `a74f122c88269ec3b2300aae952d52e7d7befa94`.
- Native image tag: `open-ontologies:2.1.0-native-a74f122c8826`.
- Native image ID:
  `sha256:93989cb0f72dc52eb46d32ccfd0b0203e7a6e6fc1b609cd2920da7eadb23e4b0`.
- Worker SHA-256:
  `a294b0c0b6c7010ea74ed5475e53f9b11ed72b399eda3dd4fbaea60e59a86f20`.
- Generic protocol proxy: `0.2.0+local.4d7674855d95`.
- Tunnel client: `0.0.19+01d67bb4da0d8359b808b4761f8db171ab682cd7`.
- Host: Ubuntu 24.04.4 LTS, aarch64; Docker 29.7.2;
  Go `go1.26.10 linux/arm64`.

The binary was built from a clean implementation commit. This verification
document is committed afterward; its commit is recorded separately in runtime
metadata rather than being represented as the executable's source commit.

## Findings and change

The earlier 10:19:24 conversion error returned in 12 ms, but its reason was not
logged. Its original input and cause cannot be reconstructed. The service was
otherwise healthy in the reviewed 09:01–10:20 window: no observed HTTP 5xx,
repeated authentication errors, crashes or automatic restarts. Unrelated
`.env`/`.git` probes returned 404; unsupported methods returned 405.

The adapter now records bounded error categories, stages, a generated reference,
error-text SHA-256, byte count and adapter version. Conversion failures can be
distinguished at argument validation, parsing, file-IRI inspection and rendering.
Common private-worker and backend transport/protocol/limit failures also have
typed origins. Other adapted failures can remain `unclassified_error` rather
than being guessed from exception text. Only pass-through transport/decoding
failures are newly logged; this is not a full audit of semantic tool results.

Raw exceptions, RDF, arguments, internal paths, client IDs and arbitrary tool or
format strings are excluded from these diagnostic records. Successful negative
findings, including SHACL `conforms:false`, remain successful. Client error text,
HTTP failure bodies, catalog schemas, read-only grants, limits and retry behavior
are preserved. See the [plan](../DIAGNOSTICS-PLAN-2026-11-05.md) and the README's
diagnostic contract for the exact scope.

During qualification, a full RDF query encountered the existing 10 MiB MCP
response cap. The shared store had grown externally to 139,781 quads; its
administrative JSON snapshot is approximately 34.1 MB. This does not establish
that the earlier conversion error had the same cause. The cap was retained and
the failure can now be identified as `backend_output_limit`. Clients should use
bounded queries rather than requesting the entire store in one MCP response.

## Verification and production cutover

- `make check`: formatting, `go test -race ./...` and `go vet ./...` passed.
- Unit checks cover unchanged error text/unwrapping and HTTP 502 body, failure
  categories/stages, secret-marker redaction, negative findings, unique concurrent
  references, cancellation, real worker deadline cleanup, startup/protocol/EOF
  failures, and JSON/SSE response-size limits.
- A disposable native container used the exact production image and pinned
  worker. Through the adapter and existing modern proxy, all seven conversion
  formats worked in both directions, dataset graph names and OFN warnings were
  retained, and the existing inline/private integration passed, including 16
  concurrent calls. A synthetic seed triple remained unchanged during these
  read-only checks. QA workers cleaned their temporary directories.
- Five diagnostic probes matched real codes/stages and unchanged client error
  hashes: invalid arguments, oversized input, parser failure, unsupported output
  and prohibited file IRIs. A dummy private marker was absent from captured
  journals. A valid SHACL violation was not logged as an execution error.
- Separately, the disposable QA store loaded an 11 MiB literal. A full query
  through the adapter returned the original HTTP 502 body and generated
  `backend_output_limit` at `backend.read_response`. No large-query mutation or
  intentional cap-hit test was run against production.
- The production cutover ran 11:07:57–11:08:41 UTC. The existing tunnel was
  inspected for zero active dispatches, stopped with the proxy/adapter, and then
  resumed through the same systemd unit after readiness and acceptance checks.
  The native container and persistent data mount continued running.
- Before/drained/after full RDF fingerprints matched: **139,781 quads**, SHA-256
  `344bfa57e2609c7d7aaa04e7a6584dd0fbafa9cfae411121a5058d5c8f4f9252`.
  This is the hash of newline-joined sorted SPARQL-result JSON rows, suitable
  for comparing this same running store; it is **not RDFC canonicalization**.
  A bounded loopback administrative read was used because the complete result
  exceeds the public MCP response cap. No RDF content is stored in these reports.
- Policy, action and invariant registries each retained one row. Fixed-table
  sorted-row compact-JSON SHA-256 matched:
  `5b67a58d6e4ed0717ad4d4aa6ef56ca705d67c6f136a186972f537c5f154f56a`.
- The complete catalog fingerprint and five diagnostic error-text hashes matched
  the old adapter. Catalog SHA-256:
  `db85fe45dfd5c8fb906bc448fa0b6cb943288cb1e865d7b410123534a0391be1`.
  There are still 125 tools and 39 reviewed read-only grants.
- Read-only/private integration and diagnostic log acceptance passed in
  production. Public HTTPS discovery and inline Turtle-to-RDF/XML conversion
  passed through Caddy. An actual connected-app `onto_status` call returned
  `status:ok`, the new adapter version, 125 tools and 139,781 triples.
- The owning tunnel resumed control-plane polling and accepted the real cloud
  tool response with zero dispatcher failures. Adapter, proxy and tunnel are
  active and enabled, with zero automatic restarts and no reload-needed flags.
  No post-cutover warning/error was present in the captured connection journals;
  adapter failure records were expected negative acceptance cases.
- Owned QA units/container were stopped and removed after acceptance. No native
  rebuild, worker/configuration change, solver installation, website regeneration
  or GitHub push was included. This change adds no new tool arguments and does
  not require a new connector schema refresh.

## Rollback and retained evidence

Previous executable and unchanged configuration backup:
`/data/service-backups/open-ontologies/20261105T110757Z-adapter-diagnostics/`.
This is an adapter rollback backup, not a cold database backup.

Private evidence and operator scripts:
`/data/service-builds/oo-diagnostics-20261105/`.
Frozen executable, source archive, evidence and SHA-256 manifest:
`/data/service-artifacts/open-ontologies/adapter-diagnostics-5df3a0ce71bd-20261105/`.
Runtime executable/documentation identities are recorded separately in
`/home/ubuntu/mcp-services/open-ontologies/ACTIVE-SOURCE.json`.

To roll back, inspect/drain and stop the owning tunnel, proxy and adapter;
restore the backed-up adapter executable; start adapter and proxy with readiness
gates; resume the same tunnel unit. Do not restore the database or start another
tunnel client for this interface-only rollback.
