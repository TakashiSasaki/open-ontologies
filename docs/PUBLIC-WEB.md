# Public read-only web API

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

`cmd/open-ontologies-web` is a separate binary/service, not an unrestricted MCP
gateway. Its UI lives in the independent open-ontologies-site repository.
The existing MCP adapter and generic compatibility proxy remain unchanged in
production. Build and install the new binary with its own Git build identity.

## Contract

All routes use `/web-api/v1/`. JSON responses use `Cache-Control: no-store`.
No login, source IP allowlist, or cookies are required. Caddy's existing MCP
source filtering still applies to the separately routed MCP endpoints.

| Method / route | Arguments | Behavior |
| --- | --- | --- |
| GET `status` | optional `graph` | Current triple count, exact engine/API versions, observation time and configured public graphs. |
| GET `resources` | optional `graph`, `q`, `kind`, `offset` | Search IRI subjects by label/IRI; `kind` = all/class/property. 25 results/page, `has_more`, offset at most 10,000, query at most 128 characters. |
| GET `resource` | `id`, optional `graph` | Outgoing property/value pairs for one absolute IRI. At most 100 rows; values clipped at 2,000 characters with original length. |
| POST `calculate` | JSON described below | Operate on supplied text in a disposable worker. Never modifies the shared store. |

Graph scopes are explicit operator configuration, initially only `default`, the
same graph already publicly exported. Unknown graphs are rejected. The service
never automatically adds named graphs. Classes/properties are filtered by their
explicit RDF type declarations, not inferred type memberships. Each response is
a separate live observation, not a transaction spanning pages or status queries.
Blank nodes are displayed with their `_:` marker, but cannot be searched as if
they were stable IRIs: SPARQL query blank labels are existential, and `STR(blank)`
is undefined. The static canonical export supports their detail navigation.

Read requests use fixed SELECT templates on native `/api/query`, which calls
`GraphStore.sparql_select` directly. Unlike MCP `onto_query`, it does not call
`registry.ensure_loaded`. No arbitrary SPARQL, REST path, URL, or tool name can be
supplied. Strings are encoded as RDF terms; never append client query fragments.

Calculation example:

```json
{"operation":"reason","format":"turtle","profile":"rdfs","content":"<urn:Fido> a <urn:Dog> ."}
```

`format` must be `turtle` or `ntriples`. Operations:

- `validate`: parse and count unique input triples.
- `convert`: require `to` = turtle/ntriples/rdfxml/nquads/trig/jsonld/ofn.
- `reason`: require `profile` = rdfs/owl-rl; return original + inferred N-Triples.
- `classify`: the native OWL 2 EL classification and coverage report.
- `defects`: native structural checks, capped at 100 returned defects.

Unknown fields, inappropriate options, paths and other input formats are refused.
Turtle/N-Triples do not have remote JSON-LD context or XML entity expansion. OWL
import statements remain statements, not requests to fetch imported files. `file:`
IRIs are rejected after parsing; explicit absolute bases are required for relative
terms. No input or raw native error is logged. A fixed error with `complete:false`
is returned if parsing, calculation, output, or deadline limits fail; successful
HTTP does not imply a complete OWL proof. Native diagnostics, unsupported-construct
information and conversion-loss reports remain in `report`.

## Limits and isolation

- Text: 64 KiB UTF-8; JSON envelope: 384 KiB; unique input triples: 2,000.
- Public output: 2 MiB; reading deadline: 6 seconds; calculation: 15 seconds.
- Reads: 2 concurrent, 120 requests/minute globally. Jobs: 2 concurrent,
  12/minute globally. Excess returns 429, not queued background work.
- Worker `prlimit`: 512 MiB address space, 20 CPU seconds, 8 MiB file size,
  128 open files. Parent deadline kills/reaps the process group and removes scratch.
- systemd service: DynamicUser, no `/data` or home access, private temporary
  directory, read-only system, loopback networking, no privilege escalation;
  1.5 GiB aggregate memory, 100% of one CPU, 128 tasks, no swap/core dumps.

The worker must match the native engine version and a configured SHA-256. Never
pass production state paths, root mappings, host credentials or model overrides.
The public service forcibly clears the MCP adapter's state/root options. It starts
each worker with a new HOME/config/data directory and in-memory storage. Workers
are application-state isolation, not separate VMs or per-request network namespaces.

Native SELECT execution is synchronous. A client timeout does not cancel a native
query already executing; small fixed templates and global limits reduce load but
do not solve engine cancellation. At substantially larger datasets, add native
cancellation or a separately maintained read replica before increasing traffic.

## Build, verification and deployment

Run `go test -race ./...` and `go vet ./...`. The actual-engine integration test
starts its own loopback native server and writes fixtures only there:

```sh
OO_PUBLIC_TEST_WORKER=/absolute/path/to/pinned-native go test -race ./...
```

It covers private-graph exclusion, query injection, fresh reads after fixture
changes, real format conversion/inference, malformed/relative/file IRIs, limits,
cancellation, scratch cleanup, OS memory/file limits and an unchanged complete
fixture dataset before/after calculations.

Use the example config and systemd unit in `deploy/`. Install the hash-verified
native worker in `/usr/local/libexec` and the public API binary in `/usr/local/bin`;
the service cannot access source/builds under `/data`. Keep the real configuration
and deployment evidence outside Git. Validate the hardened service with a private
calculation before adding Caddy's `open_ontologies_public_web` snippet. Publish the
static site from an immutable commit and retain prior releases for rollback.
Rollback only the new public service/site routing; do not change MCP source filters.
