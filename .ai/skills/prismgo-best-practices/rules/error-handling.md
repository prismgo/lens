# Error Handling

## Return Boundary Errors Immediately

Controllers, commands, diagnostics, and MCP tools should stop at the first invalid boundary input.

Incorrect:

```go
if err := c.ShouldBindJSON(&req); err != nil {
	logger.Warn("bad request")
}
_ = service.Create(c.Request.Context(), req)
```

Correct:

```go
if err := c.ShouldBindJSON(&req); err != nil {
	c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	return
}
if err := service.Create(c.Request.Context(), req); err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
	return
}
```

## Do Not Hide Runtime Discovery Failures

Lens tools should return explicit errors when runtime listing, diagnostics, or docs lookup fails. Silent empty results teach the Agent false facts.

Incorrect:

```go
routes, err := runRouteList()
if err != nil {
	return []RouteInfo{}, nil
}
```

Correct:

```go
routes, err := runRouteList()
if err != nil {
	return nil, fmt.Errorf("list routes: %w", err)
}
```

## Use The Exception Handler For HTTP Boundaries

Prefer the framework exception middleware and configured handler instead of one-off panic recovery inside every controller.

```go
foundation.Configure().
	WithExceptions(func(e *foundation.Exceptions) {
		e.DontReport(ErrNotFound)
	})
```

## Use Safe Routines For Background Work

Use `routine.Task` or `routine.Go` so panics are recovered and reported consistently.

```go
routine.Task(ctx, func(ctx context.Context) error {
	return worker.RunOnce(ctx)
}).Component("queue").Name("worker.run-once").Go()
```
