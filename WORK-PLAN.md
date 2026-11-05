# MCP audit remediation — 2026-11-05

Baseline: unmodified Open Ontologies 2.1.0, source
`cefe5997d04553ac495873e8aa1046c1d6b85f7d`. Keep the native image and existing
graph, policy, action and invariant registries unchanged. Private evidence and
runtime configuration belong outside Git.

1. **Reproduce in isolation.** Start the same image with a fresh data mount and
   loopback-only QA endpoints. Seed minimal Organization, SHACL and action cases.
   Record the production graph/registry baseline using read-only inspection.
2. **Policy and errors.** Validate target IRIs, bind whole RDF terms safely in
   both `{target}` and `<{target}>` templates, and report query failures as
   indeterminate/errors. Preserve valid-rule deny-overrides/default-allow
   semantics. Mark actual upstream tool error objects as MCP `isError`, without
   treating a valid negative validation verdict as a protocol failure.
3. **SHACL preflight.** Inspect all shapes and referenced class/property IRIs,
   independently of target type. Make coverage and unsupported paths explicit.
   Retain native parse/bound checks and the distinction from data validation.
4. **Actions and planning.** Expose action details without modifying registries;
   validate bindings consistently. Diagnose legacy template incompatibilities;
   test any sandbox adaptation only in an independent process. Normalize the
   SPARQL predicate `a` where it can be translated faithfully, and mark incomplete
   PDDL as unsuitable for execution. Do not bypass live preconditions.
5. **Operational truth.** Add explicit optional dependency readiness, truthful
   dry-run alignment summaries and named-graph-only temporal scope metadata.
   Missing optional binaries do not mean that every ordinary tool is unavailable.
6. **Qualify and deploy.** Run Go race/vet checks and actual pinned-worker tests,
   QA regression including invalid input, and read-only production verification.
   Re-pin reviewed annotation schemas; drain/restart only the adapter/proxy/tunnel
   owners. Record exact commits, image/binary hashes, limitations and rollback.

Atomic authorization of every mutation, correct native effect semantics for all
RDF terms, and complete SPARQL-to-PDDL compilation require engine support. A
proxy policy check is an advisory point-in-time evaluation, not a transaction
or universal access-control boundary. Preserve existing upstream patch candidates
and document any additional engine work with its validation limits.
