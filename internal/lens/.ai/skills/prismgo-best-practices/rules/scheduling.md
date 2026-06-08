# Task Scheduling Best Practices

## Use `WithoutOverlapping()` on Variable-Duration Tasks

Without it, a long-running task spawns a second instance on the next tick, causing double-processing or resource exhaustion.

```go
func Register(s *timer.Schedule) {
	s.Command("reports:rebuild").
		EveryTenMinutes().
		WithoutOverlapping(15)
}
```

For `Call` tasks, set a stable `Name(...)` so the overlap lock key is stable across process restarts.

```go
s.Call(rebuildDashboard).
	Name("dashboard_rebuild").
	EveryTenMinutes().
	WithoutOverlapping(15)
```

## Use `WithoutOverlapping()` With Shared Cache on Multi-Server Deployments

Without a shared lock, every server runs the same task simultaneously. Prismgo scheduled tasks should use a shared cache driver, such as Redis, when `WithoutOverlapping()` must coordinate across servers.

```go
func Register(s *timer.Schedule) {
	s.Command("billing:charge").
		Monthly().
		Name("billing_charge").
		WithoutOverlapping(120)
}
```

The lock is only cross-server when the configured cache store is shared by all scheduler processes. Do not rely on in-memory or file-only stores for multi-server coordination.

## Use Queue Jobs for Concurrent Long Tasks

By default, each scheduled task is responsible for its own execution time. A slow task can hold its own overlap lock and consume scheduler resources. Dispatch long work to the queue and keep the scheduled command short.

```go
func (c SendReportsCommand) Handle(ctx console.CommandContext) error {
	_, err := queue.Dispatch(
		ctx.Context(),
		SendReportsJob{},
		queue.OnQueue("reports"),
		queue.Tries(3),
	)
	return err
}
```

```go
func Register(s *timer.Schedule) {
	s.Command("reports:send").EveryFiveMinutes().WithoutOverlapping(10)
}
```

## Use Environment Checks to Restrict Tasks

Prevent accidental execution of production-only tasks such as billing or reporting on staging.

```go
func Register(s *timer.Schedule) {
	if config.GetString("app.env", "production") != "production" {
		return
	}

	s.Command("billing:charge").
		Monthly().
		Name("billing_charge").
		WithoutOverlapping(120)
}
```

Keep environment gating near schedule registration so unsupported environments never register the task.

## Use Context Timeouts for Time-Bounded Processing

A task running every 15 minutes that processes an unbounded cursor can overlap with the next run. Bound execution time.

```go
func (c ProcessInvoicesCommand) Handle(ctx console.CommandContext) error {
	runCtx, cancel := context.WithTimeout(ctx.Context(), 14*time.Minute)
	defer cancel()

	return processInvoicesUntilDone(runCtx)
}
```

Pair the timeout with `WithoutOverlapping()` so a timed-out run releases the lock before the next window.

```go
s.Command("invoices:process").EveryFifteenMinutes().WithoutOverlapping(15)
```

## Use Schedule Registration Helpers for Shared Configuration

Avoid repeating shared configuration such as overlap locks, names, and environment gates across many tasks.

```go
func Register(s *timer.Schedule) {
	registerProductionBillingTasks(s)
	registerMaintenanceTasks(s)
}

func registerProductionBillingTasks(s *timer.Schedule) {
	if config.GetString("app.env", "production") != "production" {
		return
	}

	s.Command("billing:charge").
		Monthly().
		Name("billing_charge").
		WithoutOverlapping(120)

	s.Command("billing:prune").
		DailyAt("03:00").
		Name("billing_prune").
		WithoutOverlapping(30)
}
```
