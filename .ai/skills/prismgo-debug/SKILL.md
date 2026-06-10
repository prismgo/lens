---
name: prismgo-debug
description: Debug PrismGo applications with Prismgo Lens MCP tools before changing code.
---

# PrismGo Debug

Use Prismgo Lens MCP tools before changing code. Prefer `application-info`, `search-docs`, `list-routes`, `list-console-commands`, logs, browser logs, and read-only database tools.

Rules:

- Reproduce and locate the failing behavior before editing.
- Prefer runtime metadata over source-text guessing when route or command registries are involved.
- Use `run-diagnostic` instead of arbitrary Go evaluation.
- Keep diagnostics read-only and bounded.
