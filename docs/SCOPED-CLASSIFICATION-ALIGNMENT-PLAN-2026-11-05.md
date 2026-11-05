# Scoped classification, alignment evidence and explicit tool profiles

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

The follow-up report identifies two remaining interface gaps: EL classification
still invokes the shared native algorithm directly, and alignment decisions use
confidence/ambiguity without requiring evidence coverage. Native informative
scoring defaults to high=0.95; confidence 0.846 is only auto-eligible under a lower
caller threshold. Even confidence 1 can come from labels alone.

1. Reuse bounded request-local dataset capture for onto_classify_el: optional
   ontology_turtle or exact graph_iri, 10,000 rows/4 MiB, 15-second budget (1–20).
   Preserve native complete/incompleteness; add analysis_complete separately.
   Shared cache reads retain their existing behavior; withdraw the old EL
   readOnly grant rather than letting a changed schema inherit approval.
2. Evaluate onto_align with native dry_run=true, preserving native score,
   cardinality, ambiguity and review decisions. Add an eligibility gate requiring
   evidence_coverage>=0.4 and at least two distinct known informative signals.
   Missing/unrecognized evidence and label-only matches require review. Never
   merely relabel rows after native auto-application.
3. For requested live alignment, publish only eligible native relation triples
   through one existing inline native onto_load transaction. Preserve native
   policy enforcement; fail the whole publication on a denial. Report unknown
   publication outcomes honestly and never retry. Dry runs never publish RDF.
   The native dry-run call may still record operational lineage.
4. Classify known missing embedding model/index errors with resolution hints.
   Keep optional functionality visible instead of assuming file snapshots prove
   runtime availability. Do not install arbitrary models, clinical dictionaries
   or solvers, or treat absent verified checkers as proof success.
5. Add an optional /mcp/core profile (17 tools), retain /mcp and the existing four
   profiles, and expose operator endpoint choices in capability metadata. Do not
   switch existing client catalogs silently or dynamically hide mixed-mode tools.
6. Exercise failure/positive publication, policy denial, no retries, native EL
   incompleteness, graph scope, guard refusal, concurrency, real workers and
   client error projections in isolated QA. Verify full RDF and fixed registries
   before/after authorized adapter/config cutover and preserve rollback evidence.

Named graphs provide dataset scope, not tenant authorization. Existing mixed
default-graph triples cannot be partitioned reliably without import provenance.
Inline onto_load deliberately bypasses the file cache; active:null does not by
itself indicate data loss. Additive target_graph imports, transactional registry
ownership, automatic locality-module pipelines and asynchronous native jobs need
separate engine/API design. Never implement named loading by using the native
file loader to replace/restore the live store. No such migration is included.
