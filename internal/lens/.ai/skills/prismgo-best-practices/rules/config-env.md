# Config And Env

## Read Environment Variables During Config Registration

Environment reads belong in config registration. Application code should consume namespaced config values.

Incorrect:

```go
dsn := os.Getenv("DATABASE_DSN")
```

Correct:

```go
func init() {
	config.Add("database", func() map[string]interface{} {
		return map[string]interface{}{
			"default": config.Env("DB_CONNECTION", "mysql"),
		}
	})
}
```

Then read through config helpers:

```go
driver := config.GetString("database.default", "mysql")
```

## Keep Config Namespaced

Use top-level namespaces such as `cache`, `queue`, `session`, `filesystem`, `logger`, and `database`.

Incorrect:

```go
config.Add("default", func() map[string]interface{} {
	return map[string]interface{}{"driver": "redis"}
})
```

Correct:

```go
config.Add("cache", func() map[string]interface{} {
	return map[string]interface{}{
		"default": config.Env("CACHE_STORE", "memory"),
		"stores": map[string]interface{}{},
	}
})
```

## Redact Secret-Like Keys

Never log or return raw values for keys containing words such as `password`, `secret`, `token`, `credential`, or `key`.

```go
logger.WithField("config_path", "database.connections.mysql.password").
	Info("config value redacted")
```

## Prefer Independent Config Instances In Tests

Use isolated config instances or test binding helpers rather than mutating global process environment in unrelated tests.

```go
cfg := config.New()
cfg.Set("cache.default", "memory")
```
