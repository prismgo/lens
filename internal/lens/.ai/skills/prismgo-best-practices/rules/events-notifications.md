# Events And Notifications

## Dispatch Small Domain Events

Events should carry identifiers and small payloads. Keep heavy data loading inside listeners.

```go
type UserRegistered struct {
	UserID uint
}

func (e UserRegistered) Name() string {
	return "user.registered"
}

func RegisterUser(ctx context.Context, id uint) {
	event.Dispatch(ctx, UserRegistered{UserID: id})
}
```

Incorrect:

```go
type UserRegistered struct {
	RawRequest *http.Request
	Rows       []LargeRow
}
```

## Register Listeners Explicitly

Register listeners in providers or a central listener registration function.

```go
func (p EventServiceProvider) Boot(app providercontract.Application) error {
	event.ListenFunc("user.registered", func(ctx context.Context, ev event.Event) error {
		return sendWelcomeMessage(ctx, ev)
	})
	return nil
}
```

## Use Queued Listeners For Reliable Async Work

Use `ShouldQueue` or `event.Queued` when listeners need retry, delay, worker isolation, or failure tracking.

```go
type SendWelcomeMessage struct{}

func (SendWelcomeMessage) Handle(ctx context.Context, ev event.Event) error {
	return sendWelcomeMessage(ctx, ev)
}

func (SendWelcomeMessage) ShouldQueue() bool { return true }
func (SendWelcomeMessage) QueueName() string { return "messages" }
```

## Keep Primary Services Focused

Primary services should update state and dispatch events. Notification delivery, projections, and external calls belong in listeners.

Incorrect:

```go
func Complete(ctx context.Context, id uint) error {
	updateStatus(ctx, id)
	sendEmail(ctx, id)
	writeProjection(ctx, id)
	return nil
}
```

Correct:

```go
func Complete(ctx context.Context, id uint) error {
	if err := updateStatus(ctx, id); err != nil {
		return err
	}
	event.Dispatch(ctx, RecordCompleted{ID: id})
	return nil
}
```
