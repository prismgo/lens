# Translation

## Use Translation Keys

Framework-owned and package-owned messages should use translation keys rather than hard-coded strings.

Incorrect:

```go
return "The name field is required."
```

Correct:

```go
return translation.Get("validation.required", map[string]any{
	"attribute": "name",
})
```

## Register Package Paths In Providers

Packages with language files should add namespaces or paths during provider boot.

```go
func (p ServiceProvider) Boot(app providercontract.Application) error {
	translation.AddNamespace("billing", "lang/vendor/billing")
	return nil
}
```

## Use Choice For Pluralization

```go
message := translation.Choice("files.count", count, map[string]any{
	"count": count,
})
```

## Handle Missing Keys Deliberately

Use missing-key handlers for diagnostics or controlled fallback, not for hiding missing translations during development.

```go
translation.HandleMissingKeysUsing(func(ctx context.Context, key string, locale string) (string, bool) {
	logger.WithFields(map[string]any{"key": key, "locale": locale}).Warn("missing translation")
	return "", false
})
```
