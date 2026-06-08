# Lens Core

- Run `application-info` before broad changes so the agent sees the Go version, modules, and detected PrismGo features.
- Use `search-docs` before guessing framework behavior.
- Use read-only database and log tools for diagnosis; do not add write-capable MCP tools.
- Keep Lens config in `.prismgo-lens.json` and machine state in `.prismgo-lens.local.json`.
