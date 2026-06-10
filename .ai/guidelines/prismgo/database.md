# Prismgo Lens Database

- Database tools are read-only.
- Schema changes must use migrations with rollback and follow the host project's database instructions.
- `database-query` only allows SELECT, SHOW, EXPLAIN, DESCRIBE, DESC, and WITH queries whose final statement is SELECT.
- Keep driver-specific schema introspection behind stable output contracts.
