# Provider And Facade

## Bind In Register, Integrate In Boot

`Register` should bind services into the container. `Boot` should register listeners, commands, routes, publishing metadata, and other integrations that depend on registered services.

Incorrect:

```go
func (p ServiceProvider) Register(app providercontract.Application) error {
	event.ListenFunc("user.created", listener)
	return nil
}
```

Correct:

```go
type ServiceProvider struct{}

func (p ServiceProvider) Register(app providercontract.Application) error {
	return app.Container().Singleton("report.generator", func() (*Generator, error) {
		return NewGenerator(), nil
	})
}

func (p ServiceProvider) Boot(app providercontract.Application) error {
	event.ListenFunc("user.created", listener)
	return nil
}
```

## Use Deferrable Providers For Lazy Services

Services that are expensive or optional should declare the service keys they provide.

```go
func (p ServiceProvider) Provides() []string {
	return []string{"report.generator"}
}
```

Do not open network connections in `Register` just because a provider exists. Bind a factory and let the facade/container resolve it when needed.

## Declare Commands From Providers

Use `provider.Commands` in `Boot`. It defers command mounting to the console kernel starting phase, so HTTP-only boots do not construct command dependencies.

Incorrect:

```go
func (p ServiceProvider) Boot(app providercontract.Application) error {
	return cmd.NewSyncCommand().Handle(nil)
}
```

Correct:

```go
func (p ServiceProvider) Boot(app providercontract.Application) error {
	return provider.Commands(
		cmd.NewSyncCommand(),
		func() (console.Command, error) {
			return cmd.NewRepairCommand(), nil
		},
	)
}
```

## Keep Contracts Abstract

Contracts should describe behavior, not concrete driver state.

Incorrect:

```go
type RedisQueue struct {
	Client any
}
```

Correct:

```go
type Queue interface {
	Push(ctx context.Context, payload []byte) error
}
```

Concrete implementations belong in packages such as `queue`, `cache`, `filesystem`, or application infrastructure packages.
