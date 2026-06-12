# Queue Jobs

## Define Jobs Around `Handle(context.Context)`

Queue jobs should be small serializable values with explicit runtime behavior.

```go
type ExportJob struct {
	ID uint
}

func (j ExportJob) Handle(ctx context.Context) error {
	return buildExport(ctx, j.ID)
}
```

Avoid storing database handles, channels, HTTP requests, or function values inside jobs.

Incorrect:

```go
type ExportJob struct {
	DB *gorm.DB
	Fn func() error
}
```

## Dispatch Through The Queue Facade

Use `queue.Dispatch`, `queue.Later`, `queue.Chain`, or `queue.Batch` with explicit options.

Incorrect:

```go
_ = amqpChannel.PublishWithContext(ctx, exchange, key, false, false, msg)
```

Correct:

```go
id, err := queue.Dispatch(
	ctx,
	ExportJob{ID: 100},
	queue.OnConnection("redis"),
	queue.OnQueue("exports"),
	queue.Tries(3),
	queue.Timeout(30*time.Second),
)
_ = id
```

## Use Queue Middleware For Cross-Cutting Behavior

Locks, rate limits, timeouts, and exception throttling belong in queue middleware or dispatch options, not duplicated inside every job.

```go
queue.UseMiddleware(
	queue.WithoutOverlapping("exports", time.Minute),
	queue.ThrottlesExceptions(5, time.Minute),
)
```

## Keep Diagnostics Read-Only

Queue diagnostics may read configuration, connection names, queue sizes, failed job summaries, and worker health. They must not dispatch jobs, retry jobs, clear queues, or request worker restarts.

Incorrect:

```go
func queueHealth(ctx context.Context) error {
	_, err := queue.Dispatch(ctx, ExportJob{ID: 1})
	return err
}
```

Correct:

```go
func queueHealth() string {
	cfg := queue.BuildConfig()
	return cfg.Default
}
```
