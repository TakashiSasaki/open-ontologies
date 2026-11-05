# Gemini report: bounded analysis and explicit failures

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

The deployed native container has zero restarts/OOM events; conversion and
crosswalk handlers return error JSON. CONNECTION_CLOSED does not prove a panic.
A real onto_defects timeout is logged at 10:23:19 UTC after 90 seconds. Its
native limit applies after all findings are collected, not to CPU work.

1. Translate the adapter's known 10 MiB response-limit failure into a bounded
   MCP tool error, preserving the request ID, marking execution outcome unknown
   and never replaying a mutation. Keep other transport/session failures intact.
2. Run onto_defects over explicit inline Turtle or one bounded snapshot in a
   private pinned worker. Add graph_iri for an exact named graph. Bound snapshots
   to 10,000 selected rows/4 MiB Turtle and the entire operation to 15 seconds by
   default (max_seconds: 1–20). On failure report complete:false, never a clean
   verdict or invented partial native findings. Named-graph results apply only
   to the selected dataset; external dependencies are not implicitly included.
3. Add structured clinical dependency errors and inspect the fixed
   data/crosswalks.parquet path inside the actual container working directory.
   Distinguish missing, present-but-unverified and unknown. Do not install sample
   dictionaries/solvers as though presence established validated results.
4. Test bounds, named graphs/blank nodes, cancellation/reaping, errors followed by
   successful calls, shared-state preservation and no retries. Run race tests,
   vet and real disposable-chain integration before adapter-only adoption.
5. Review/re-pin only changed annotation schemas, preserve profiles, full RDF
   and registries, and drain/reuse the owning tunnel. Freeze source/binary/image
   identities and rollback artifacts. Explain client schema-refresh requirements.

Native-only follow-up remains: cooperative query cancellation, partial per-kind
progress, faster defect algorithms and bounded policy-report construction.
The adapter cannot make full 140k-triple analysis complete or bound computation
that is already running inside the shared native server. Preserve the existing
native patch archive; unapplied proposals are not deployed fixes.
