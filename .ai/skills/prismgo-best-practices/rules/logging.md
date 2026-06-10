# Logging

## Use PrismGo Logger Facades

Use `logger` package facades or injected `*logger.Manager`. Do not spread new global logger instances through application code.

Incorrect:

```go
logrus.New().Info("job started")
```

Correct:

```go
logger.WithField("job", "export").Info("job started")
```

## Use Structured Fields

Fields make logs searchable and keep messages stable.

```go
logger.WithFields(map[string]any{
	"job":    "export",
	"job_id": id,
}).WithError(err).Error("job failed")
```

## Use Named Channels For Subsystems

Use configured channels for dedicated output. Do not create unregistered file writers in services.

Incorrect:

```go
file, _ := os.OpenFile("storage/logs/export.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
_, _ = file.WriteString("started\n")
```

Correct:

```go
logger.Channel("jobs").WithField("job", "export").Info("started")
```

## Never Log Secrets

Redact credentials and raw session or cookie values before logging.

```go
logger.WithField("connection", "mysql").Info("database connection checked")
```
