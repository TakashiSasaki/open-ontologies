# Open Ontologies MCP adapter

Keep remote-client interface improvements outside the Open Ontologies engine.
This Go HTTP adapter sits between the generic MCP compatibility proxy and an
**unmodified upstream** Open Ontologies HTTP server. It adds explicit inline and
request-local input forms without patching or rebuilding the Rust application.

Author: 佐々木隆志 (Takashi Sasaki), <takashi316@gmail.com>,
[https://x.com/TakashiSasaki](https://x.com/TakashiSasaki). [MIT License](LICENSE).

The [2026-11-04 verification record](docs/compatibility/2026-11-04.md) fixes the
tested image, source revisions, binaries, protocols and actual verification
boundary. See [integration instructions](tests/README.md) for reproducible checks.

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
| `onto_ingest` | `inline_data` with `format` (`csv`, `json`, `ndjson`, `xml`, `yaml`); `dry_run` in a private worker. Normal inline writes publish generated RDF once through upstream `onto_load`. |
| `onto_diff` | `old_content` / `new_content` Turtle; exactly one inline content or server path per side. Existing file/file calls pass through unchanged. |
| `onto_dl_check`, `onto_dl_explain` | Full absolute class IRIs with or without angle brackets. CURIE prefixes are not expanded. |
| `onto_reason`, `onto_defects`, `onto_dl_check`, `onto_dl_explain`, `onto_justify`, `onto_shacl` | Optional `ontology_turtle` in a fresh process. Successful JSON objects include `execution_scope: request` and `persistent_store_modified: false`. Non-JSON reports stay unchanged. |

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

Go 1.25+ (preferred toolchain 1.26.10), Git, and an unmodified Linux executable
matching host architecture/libraries are required. Go code uses only stdlib.

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
  "max_workers": 2,
  "timeout_seconds": 75
}
```

Point the generic proxy at `http://127.0.0.1:18084/mcp`; client/tunnel URLs stay
on the generic proxy. Initialization, sessions, prompts and unadapted calls pass
through. `/health` reports adapter/worker identity and capacity; the generic
proxy's ping reports upstream connectivity.

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

The conservative 38-tool audit concerns published/shared data. Inline diff uses
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
