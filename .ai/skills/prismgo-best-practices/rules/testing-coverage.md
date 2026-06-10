# Testing And Coverage

## Test The Public Contract You Changed

Use narrow package tests for PrismGo framework packages and Lens module tests for Lens assets or MCP behavior.

```bash
go test ./prismgo/cache -run TestManager -count=1
go -C tools/prismgo-lens test ./...
```

## Add Regression Tests For Tooling Contracts

When changing embedded AI assets, MCP primitives, Agent writers, or config migration, test the generated file tree or JSON-RPC behavior instead of only checking internal helpers.

```go
func TestEmbeddedSkillContainsRules(t *testing.T) {
	rules, err := fs.Glob(builtinAIAssets, ".ai/skills/prismgo-best-practices/rules/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("expected embedded rules")
	}
}
```

## Keep Tests Focused

Do not add production APIs, exported helpers, or fallback behavior only to make tests easier.

Incorrect:

```go
func DebugInternalStateForTests() map[string]any {
	return internalState
}
```

Correct:

```go
result, err := Install(root, InstallOptions{Agents: []string{"codex"}})
if err != nil {
	t.Fatal(err)
}
assertWritten(result, ".ai/skills/prismgo-best-practices/SKILL.md")
```

## Run The Host Project's Documented Coverage Command

When the host project defines a coverage workflow, use that workflow for affected production code. If the change only touches `tools/prismgo-lens`, the Lens module test command is usually the relevant verification.

```bash
go -C tools/prismgo-lens test ./... -count=1
```
