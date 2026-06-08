# Rate Limiting

## Register Named Limiters

Register named limiters during boot, then attach them to route groups or routes.

```go
func RegisterRateLimiters() {
	ratelimit.For("api", func(c *gin.Context) []ratelimit.Limit {
		key := ratelimit.CleanRateLimiterKey("api:" + c.ClientIP())
		return []ratelimit.Limit{
			ratelimit.PerMinute(60).By(key),
		}
	})
}
```

## Attach Limiters Through Middleware

```go
route.Prefix("/api").
	Middleware(route.Throttle("api")).
	Group(func() {
		route.Get("/profile", profileHandler)
	})
```

## Avoid Over-Broad Keys

Keys should include the right boundary for the protected action.

Incorrect:

```go
key := "api"
```

Correct:

```go
key := ratelimit.CleanRateLimiterKey("api:" + c.ClientIP())
```

## Use Manual Counters For Non-HTTP Flows

Use the facade for flows that are not Gin middleware.

```go
limited, err := ratelimit.TooManyAttempts(ctx, key, 5)
if err != nil || limited {
	return err
}
if _, err := ratelimit.Hit(ctx, key, time.Minute); err != nil {
	return err
}
```

Reset or clear through the same facade after a successful guarded flow.

```go
return ratelimit.Clear(ctx, key)
```
