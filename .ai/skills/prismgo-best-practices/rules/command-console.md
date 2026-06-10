# Command Console

## Define Static Command Metadata

Every command should expose a static `Definition` and keep execution in `Handle`.

Incorrect:

```go
func (c *SyncCommand) Handle(ctx console.CommandContext) error {
	ctx.IO().Line("sync --source=crm")
	return nil
}
```

Correct:

```go
type SyncCommand struct{}

func (c *SyncCommand) Definition() *console.Definition {
	return console.MustDefinition(
		"sync:records {source : Source name} {--dry-run : Print planned work only}",
		"Synchronize records from an external source.",
	)
}

func (c *SyncCommand) Handle(ctx console.CommandContext) error {
	if ctx.OptionBool("dry-run") {
		ctx.IO().Line("dry run")
		return nil
	}
	return syncRecords(ctx.Context(), ctx.Argument("source"))
}
```

## Use CommandContext APIs

Read arguments, options, output, cancellation, and nested command calls through `console.CommandContext`.

Incorrect:

```go
func (c *SyncCommand) Handle(ctx console.CommandContext) error {
	source := os.Args[2]
	return syncRecords(context.Background(), source)
}
```

Correct:

```go
func (c *SyncCommand) Handle(ctx console.CommandContext) error {
	source := ctx.Argument("source")
	ctx.IO().Info("sync started")
	return syncRecords(ctx.Context(), source)
}
```

## Never Execute Handlers To List Commands

Command listing is metadata, not execution.

Incorrect:

```go
for _, command := range commands {
	_ = command.Handle(ctx)
}
```

Correct:

```bash
go run . list --format=json
```

## Keep Isolation Explicit

Commands that must not overlap should implement the isolation contract instead of creating ad hoc lock files.

```go
func (c *SyncCommand) IsolationKey(ctx console.CommandContext) string {
	return c.Definition().Name + ":" + ctx.Argument("source")
}
```
