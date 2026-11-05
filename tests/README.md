# Upstream integration verification

`integration.py` uses the stdlib HTTP test client from the independent
`mcp-compat-proxy` repository. Its path is configurable with `--client-script`.
Default checks use only read-only tools and explicit private inputs; the shared
graph count must stay the same. Output reports belong outside Git.

```sh
python3 tests/integration.py http://127.0.0.1:18081/mcp --output /private/evidence/adapter.json
```

For a **disposable QA backend only**, copy `tests/fixtures/` to an external
runtime data directory and mount it as `/data` in an unmodified upstream
container. Configure an adapter/worker and generic proxy on separate loopback
ports, with `/data` mapped to that disposable directory. Never mount the source
fixtures directory writable by the backend: its DB/store must stay outside Git.

```sh
python3 tests/integration.py http://127.0.0.1:18083/mcp --allow-qa-writes --output /private/evidence/adapter-qa.json
```

The explicit QA option additionally tests shared writes, stable inline
provenance, ordinary server files, dry-run files, and mixed file/inline diff.
Each write run uses a unique test namespace; it is not a cleanup operation.
All modes test five inline formats, six private analysis tools, class IRI
normalization, blank nodes, explicit null paths, invalid/ambiguous/oversized
inputs, prohibited file-output options and 16 concurrent private calls.

The full adapted catalog now has 125 tools, with 39 reviewed read-only grants.
`onto_action_info` is the additional tool. For the 2026-11-05 audit regressions,
use the **disposable QA chain only**:

```sh
python3 tests/audit.py http://127.0.0.1:18083/mcp \
  --native-url http://127.0.0.1:18082/mcp --allow-qa-writes \
  --output /private/evidence/audit-qa.json
```

This script deliberately registers/replaces synthetic QA policies/actions and
loads synthetic data. Its explicit flag is mandatory. It covers bare/bracketed
IRIs, prefixed queries, invalid/empty/injected targets, policy query failures,
all four SHACL target types, untargeted PropertyShape, nested class references,
inverse/sequence/alternative/cyclic paths, action inspection and private plans,
goal injection, PDDL guards, errors versus negative findings, alignment and
temporal scope. Private simulations must leave the QA graph count unchanged.
Use `--legacy` for the direct adapter; omit it for the modern generic proxy.

`make check` adds race detection, vet, path/symlink escape checks, worker hash
and engine-version mismatch rejection, and real process deadline/cleanup tests.

## Conversion and refusal follow-up

`followup.py` defaults to inline/private/read-only tests, including seven RDF/OWL
formats in both directions, dataset graph names, OFN warnings, relative/file IRI
rejection, explicit bases and malformed/oversize/ambiguous requests. It compares
the shared graph before and after and can check worker scratch cleanup.

```sh
python3 tests/followup.py http://127.0.0.1:18083/mcp \
  --native-url http://127.0.0.1:18082/mcp \
  --allow-qa-writes --data-dir /private/qa/data --work-root /private/qa/requests \
  --output /private/qa/followup.json
```

The explicit QA switch additionally registers a deny policy, loads a synthetic
2,102-subject dataset, attempts native and adapted clears, and verifies the
refusals and unchanged RDF. It also verifies repository-configuration guidance
and ordinary file-mode base semantics. It refuses this host's production ports.
Never run QA-write cases against a live user's store. Omit the switch and both
directory flags for production read-only checks. `--legacy` targets the adapter
directly; the default targets the modern generic proxy.
