# Production adoption — 2026-11-05 UTC

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

The user explicitly authorized replacing the production engine. The selected
native source is `a74f122c88269ec3b2300aae952d52e7d7befa94`, version
`2.1.0+local.a74f122c8826`. Image and executable identities remain those in the
[qualification record](NATIVE-MCP-VERIFICATION.md).

The application adapter was updated to `234dff0ea4d68fa2a468ed87a6ca4d28c2401de6`,
version `0.2.0+local.234dff0ea4d6`. Its executable SHA-256 is
`c36b41e0fc34e1cd5e7a6538530d86f536695b1c10d4d7cbf1cc33c4ca9a31a2`.
`native_action_semantics: true` delegates policy checking, action applicability,
plan validation and PDDL compilation to the pinned engine rather than applying
legacy engine workarounds. Inline inputs and explicit private analyses remain
in the adapter. Its worker is the exact extracted native executable, pinned by
version and SHA-256. Generic protocol bridging remains unchanged.

## Verification

- Adapter race tests, vet and new native-routing regressions passed.
- Disposable native/adapter/modern-proxy chain passed inline formats, private
  reasoning/SHACL/DL, invalid inputs and 16 concurrent private requests.
- Tunnel dispatch was idle before stopping the existing owning service.
- A cold backup verified all 31 file hashes and SQLite integrity before changing
  the image. Existing action templates required no migration and were preserved.
- The complete 141-quad production graph and all rows of the policy, action and
  invariant registries matched before/after; one entry in each registry.
- Production integration passed: 125 tools, 39 read-only hints, four existing
  profiles, private/inline calls and unchanged shared triple count.
- Native PDDL output, including reverse symbol maps, reaches the client.
  Execution readiness correctly remains false with existing unencoded native
  policies/invariants; the adapter does not substitute a legacy stub verdict.
- The owning tunnel service resumed with successful control-plane polling.
  Connected-app `onto_status` returned the new engine and adapter versions and
  141 triples. This does not claim a separate Gemini UI refresh or exhaustive
  execution of all advertised tools.
- Docker restart policy and enabled systemd services were retained. No host
  reboot was performed. Tunnel identity, credentials, Caddy access rules,
  frontend identity/icons and existing endpoints were preserved.

External planners/proof checkers and embedding assets were not installed by
this deployment. The qualification record's finite planning, trusted policy
administration and dependency-advisory limits still apply.

## Local evidence and rollback

Current state: `/home/ubuntu/mcp-services/open-ontologies/ACTIVE-SOURCE.json`.
Cold backup: `/data/service-backups/open-ontologies/20261105T065324Z-native-cutover`.
Evidence: `/data/service-builds/open-ontologies-cutover-20261105/evidence/`.
The evidence contains exact graph/registry comparisons and private connection
records; credentials and live graph payloads are not committed here.

To roll back, drain and stop the existing tunnel/proxy/adapter, stop the native
container, restore the saved Compose file, adapter executable/config and
capability inventory. Restore the complete cold data copy if required, keeping
original ownership/permissions. Start native, adapter and proxy; wait for
readiness before starting the same owning tunnel. The previous unmodified
image and worker are retained. Logs remain uncompressed in persistent archives.
