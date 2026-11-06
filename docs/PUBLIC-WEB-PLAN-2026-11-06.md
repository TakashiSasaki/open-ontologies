# Public read-only website: audit and implementation plan

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

The user requested current-code/log review and a public interactive website on
2026-11-06 (JST). The existing Caddy source filtering for MCP remains. Public
web operations must be read-only; isolated calculations may change only their
own disposable data. The website source stays in open-ontologies-site.

## Findings

- The deployed native a74f122c8826 and adapter ec346022ea8f remain active. The
  reviewed container state has zero restarts and no OOM. The last-day tunnel
  sample contains one 503 and two poll timeouts, followed by continued service.
  This does not establish a native crash. The reviewed current Caddy access log
  has no 5xx. Private samples/identities stay in the deployment evidence directory.
- Native upstream main still resolves to the fork base 375fbc4789de at audit time.
  No automatic upstream merge or native rebuild is justified by these findings.
- Full native responses can still exceed the adapter cap. The existing bounded
  MCP error is working. Native policy report allocation/cooperative query
  cancellation remain engine work; the proposed patch archive is retained.
- Several non-adapted native tool failures appear as unlisted_tool in diagnostic
  logs. That limits attribution; raw client arguments must still stay out of logs.
- Static RDF is necessarily an export snapshot. Five-minute service metadata
  does not make it a live data browser.
- MCP onto_query calls registry.ensure_loaded. Native REST /api/query directly
  calls GraphStore.sparql_select, without that cache reload path. That direct
  read is appropriate behind fixed, bounded public query templates. Native
  /api/update, /load, /save and /batch must never be exposed.
- Native queries collect their results synchronously. Public queries need fixed
  syntax, bounded rows/fields, bounded pagination and concurrency/rate limits.
  An HTTP deadline alone cannot cancel native work already running.

## Work

1. Add a separate loopback public web service to this dedicated adapter project.
   Expose only exact browsing/status/resource and private calculation routes.
   Permit only explicitly configured public graphs (initially the default graph).
2. Browse the current native store through fixed SELECT templates. User strings
   are serialized RDF terms, never SPARQL fragments. No arbitrary queries, URLs,
   backend methods, file paths or tool names are accepted.
3. Run conversion/validation/RDFS/OWL-RL/EL/defect operations in disposable pinned
   workers. Initially accept Turtle and N-Triples input, without remote contexts
   or imports. Bound bytes, triples, output, time, memory and parallel work.
   Keep this service under a distinct restricted OS identity without data mounts.
4. Build live browsing and a workbench in the site repository. Provide examples,
   cancellation, clear timestamps, errors, scope and downloadable output. Keep
   static exports available and keep API data out of PWA caches and robots.
5. Test rejected mutations/paths/query injection, malformed RDF, isolation,
   time/resource limits, real workers and unchanged shared-store content. Test
   browser navigation/mobile/keyboard and existing PWA upgrades. Publish atomic
   web releases and the separate service after validation; preserve MCP routing.

Private workers isolate application state. This does not claim a separate VM or
per-request network namespace. Public input formats and argument allowlists are
intentionally narrower than authenticated MCP. OS service restrictions and
resource limits apply to the separate public service. Shared editing, arbitrary
SPARQL, remote fetching, background export scheduling and tenant authorization
are outside this release.

## Qualification findings and disposition

The actual pinned engine integration passes with a private native server and
unchanged fixture dataset. Public query testing found two issues before release:
SPARQL STR(blank) is undefined (blank IDs now remain displayed values, not false
identity links); and DISTINCT with ORDER BY sorted all statements before deduplication.
Grouping subjects before sorting reduced the 141,005-triple live-store browse
probe from about 4.9 seconds to about 1.1 seconds; typed class lookup took 0.1 seconds.
These are measured point-in-time examples, not performance guarantees.

The restricted systemd service ran the real pinned worker successfully. Browser
checks cover search/details/paging, examples/local file input, private inference,
conversion/downloads, malformed inputs, cancellation, stale/failure/offline state,
360/390/768px layouts, existing navigation and an upgrade from the previous PWA.
The public unit is added to the existing uncompressed long-term log collector.

No observed native restart/OOM/crash requires a new engine build. Remaining
native synchronous-query cancellation and oversized report generation are not
fixed by this public interface. The earlier patch archive remains available.
The unlisted_tool diagnostic label remains a bounded privacy-preserving fallback;
expanding its allowlist is separate from this public web rollout.
