# Style

## Match Local Go Style

Follow existing package style, naming, error wrapping, and facade patterns before introducing new abstractions.

Incorrect:

```go
func HandleJSON(c *gin.Context, req any, action func() (any, error)) {
	_ = c.ShouldBindJSON(req)
	resp, _ := action()
	c.JSON(http.StatusOK, resp)
}
```

Correct:

```go
if err := c.ShouldBindJSON(&req); err != nil {
	c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	return
}
resp, err := service.Create(c.Request.Context(), req)
if err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
	return
}
c.JSON(http.StatusOK, resp)
```

## Prefer Explicit Small Functions

Small explicit functions are easier for Agents and maintainers to verify than broad generic helpers.

```go
func writeManagedSkillMarker(path string) error {
	return os.WriteFile(path, []byte("managed by prismgo-lens\n"), 0o644)
}
```

## Comment Boundaries And Non-Obvious Contracts

Use comments for lifecycle boundaries, safety constraints, design background, and exported API intent. Do not narrate obvious assignments.

Incorrect:

```go
// Set name.
name := strings.TrimSpace(input)
```

Correct:

```go
// Agent-facing diagnostics stay read-only; mutation belongs in explicit console commands.
if !diagnostic.ReadOnly {
	return ErrUnsafeDiagnostic
}
```

## Keep Changes Surgical

Do not refactor adjacent packages, rename public APIs, or rewrite generated outputs unless the task requires it.

```bash
git diff -- tools/prismgo-lens/internal/lens/.ai
```
