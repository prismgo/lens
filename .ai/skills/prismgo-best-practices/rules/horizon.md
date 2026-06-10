# Horizon

## Treat Horizon As A Queue Observability Layer

Horizon reads queue runtime state, supervisor state, worker heartbeats, and metrics. It should not become the place where jobs are defined or business decisions are made.

Incorrect:

```go
func (m *Manager) MakeJob() queue.Job {
	return ExportJob{ID: 1}
}
```

Correct:

```go
func HorizonStoreHealth() (string, error) {
	cfg, err := horizon.LoadConfig()
	if err != nil {
		return "", err
	}
	return cfg.Store, nil
}
```

## Use Presets And Feature Gates

Horizon observability has explicit cost controls. Check capability flags through `ObservabilityConfig.Enabled` instead of scattering raw boolean checks.

```go
cfg, err := horizon.LoadConfig()
if err != nil {
	return err
}
if cfg.Observability.Enabled(horizon.ObservabilityProcessHealth) {
	collectProcessHealth()
}
```

## Keep Dashboard Routes Read-Only

Dashboard HTTP handlers and MCP diagnostics should return read models. Mutating commands such as pause, continue, terminate, purge, and clear belong in console commands with explicit operator intent.

Incorrect:

```go
func dashboardStatus(c *gin.Context) {
	_ = runHorizonControlCommand(c.Request.Context(), "terminate")
}
```

Correct:

```go
func dashboardStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
```

## Bound High-Value Details

High-value details for failed, poisoned, or slow jobs must stay sampled, size-limited, and secret-redacted.

```json
{
  "observability": {
    "preset": "production_light",
    "high_value_detail_sample_rate": 0.1,
    "sample_reservoir_size": 512
  }
}
```
