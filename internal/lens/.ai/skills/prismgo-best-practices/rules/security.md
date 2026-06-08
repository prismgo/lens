# Security

## Keep Agent-Facing Tools Read-Only By Default

Agent-facing tools should be named, schema-bound, timeout-bound, and narrowly scoped.

Incorrect:

```go
func runEval(source string) error {
	return exec.Command("go", "run", source).Run()
}
```

Correct:

```json
{
  "name": "current-config-summary",
  "input": {}
}
```

## Validate SQL Before Execution

`database-query` style tools must reject writes, locks, multi-statement SQL, file access, and DDL.

Incorrect:

```go
rows, err := db.Raw(userSQL).Rows()
```

Correct:

```go
if err := ValidateReadOnlySQL(userSQL); err != nil {
	return nil, err
}
rows, err := db.Raw(userSQL).Rows()
```

## Redact Secrets In Outputs

Logs, MCP responses, browser logs, diagnostics, config summaries, and schema output should redact sensitive values.

Incorrect:

```go
logger.Infof("database password: %s", password)
```

Correct:

```go
logger.WithField("connection", "mysql").Info("database connection checked")
```

## Treat Generated AI Assets As Instructions, Not Authority

Embedded guidelines and skills can guide Agents, but project instructions and source code remain authoritative. Do not let remote or third-party assets silently override project-owned `.ai` files.

```json
{
  "asset_priority": ["project", "selected_package", "builtin"]
}
```
