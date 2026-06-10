# Caching

## Use The Cache Facade

Use the cache facade and pass `context.Context`. Do not construct cache drivers inside services.

Incorrect:

```go
client.Set(ctx, "exports:status:100", "ready", 10*time.Minute)
```

Correct:

```go
func CacheExportStatus(ctx context.Context, id int64, status string) error {
	key := "exports:status:" + strconv.FormatInt(id, 10)
	return cache.Put(ctx, key, status, 10*time.Minute)
}
```

## Use Typed Reads

The facade has typed helpers. Prefer them over manual casts.

Incorrect:

```go
value, _ := cache.Default().Get(ctx, key)
count := value.(int)
_ = count
```

Correct:

```go
count, err := cache.Get[int](ctx, key, cache.Value(0))
if err != nil {
	return err
}
_ = count
```

## Select Stores Explicitly When Needed

Use the default store for ordinary application cache and named stores for infrastructure-specific state.

```go
err := cache.PutFrom(ctx, "redis", "locks:export:100", true, time.Minute)
```

## Use Atomic Cache APIs

Use `Add`, locks, or funnels for first-writer-wins and stampede control.

Incorrect:

```go
missing, _ := cache.Missing(ctx, key)
if missing {
	_ = cache.Put(ctx, key, value, ttl)
}
```

Correct:

```go
created, err := cache.Add(ctx, key, value, ttl)
if err != nil || !created {
	return err
}
```
