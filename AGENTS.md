# Development

- This adapter changes the Open Ontologies MCP interface, not its Rust engine.
- Use a hash-pinned worker extracted from the selected backend image. Native
  fork adoption was authorized on 2026-11-05; backend and worker must match.
- Keep the general MCP protocol bridge in its own mcp-compat-proxy repository.
- Never clear, snapshot/restore, or temporarily load the shared store for a
  request-local analysis. Spawn an independent worker with a fresh data directory.
- Never retry an ambiguous shared-store write or manufacture a DL proof/trace.
- Preserve original file-path calls; bound inline input, output and concurrency.
- Keep secrets and real requests outside Git. Include all four user identifiers
  in copyright notices. License: MIT.
- Run go test -race ./..., go vet ./..., and actual upstream-worker integration
  before deploying. Record image and worker hashes outside source as appropriate.
