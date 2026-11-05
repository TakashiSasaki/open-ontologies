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

## Diagnostic log acceptance

`diagnostics.py probe <modern-mcp-url> --output <private-file>` exercises rejected
arguments, size limits, malformed input, unsupported output and file-IRI checks,
plus a successful conversion and a legitimate SHACL negative finding. It never
loads/clears the shared store and verifies the full selected RDF before/after.
It writes expected error-text hashes rather than raw errors. Capture the relevant
adapter journal as JSON lines, then run:

```sh
python3 tests/diagnostics.py check --journal /private/adapter-journal.jsonl \
  --expected /private/probes.json --output /private/log-acceptance.json
```

The check matches error hashes to their real code/stage, verifies unique failure
references, observes non-error negative findings, and rejects the synthetic secret
marker anywhere in captured logs. Unit race tests additionally cover unwrapping,
unchanged HTTP/MCP error content, worker protocol/exit/start/output-limit/capacity
failures, real process deadline cleanup and concurrent logging. Tests use dummy
markers, not actual credentials or user ontology content.

For a store whose full RDF exceeds the MCP output cap, add
`--snapshot-api http://127.0.0.1:18080/api/query` to `probe`; this uses a bounded
128 MiB loopback administrative read for full-content comparison, without raising
the public MCP cap. The ordinary QA store can use the default MCP snapshot.

After completing read-only QA probes, `large-probe` can load an 11 MiB literal
from a new QA file and verify a bounded MCP size-limit error from the adapter. It
requires `--allow-qa-writes`, a disposable native/adapter pair and its mounted
`--data-dir`, and refuses the live ports. Capture that adapter's journal and use
`check --require-backend-limit` to assert the real `backend_output_limit` record.
Do not use this mutating large fixture against production.

`scalability.py <modern-url> --output <private-file>` tests bounded private defect
analysis, invalid scopes/budgets, clinical dependency errors and malformed RDF,
then verifies a subsequent status call and unchanged shared triple count. Add
`--expect-large` to require a quick `analysis_scope_required` result from a store
larger than 10,000 rows. These modes do not mutate shared data.

For a disposable chain only, add `--allow-qa-writes --native-url <qa-native-mcp>
--data-dir <qa-mounted-data> --work-root <qa-worker-scratch>`. This seeds 10,005
synthetic triples plus a small named graph with cycles and blank-node links.
It verifies exact graph scope, empty graphs and a real native one-second deadline
on a 1,000-class cycle. It refuses the host's live ports. Run this before the
11 MiB large-response fixture. No production clear/registration test is used.
