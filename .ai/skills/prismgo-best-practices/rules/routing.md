# Routing

## Register Routes Through The Route Facade

Use `route` so groups, names, middleware, resource routes, model binding, fallback routes, and runtime route listing stay consistent.

Incorrect:

```go
router.GET("/api/v1/profile", profileController.Show)
```

Correct:

```go
func RegisterAPI(auth gin.HandlerFunc, profileController *ProfileController) {
	route.Prefix("/api/v1").Middleware(auth).Group(func() {
		route.Get("/profile", profileController.Show).Name("profile.show")
	})
}
```

## Compose Middleware At The Right Level

Put shared middleware on the group and route-specific middleware beside the route.

```go
route.Prefix("/api/v1").
	Middleware(authRequired, requestID).
	Group(func() {
		route.Get("/users", canListUsers, userController.List)
		route.Post("/users", canCreateUsers, userController.Create)
	})
```

## Use Named Middleware When It Must Be Removed

`WithoutMiddleware` works on named middleware identifiers.

```go
skipCache := route.NamedMiddleware("cache.headers", cacheHeaders)

route.Middleware(skipCache).Group(func() {
	route.Get("/status", statusController.Show)
	route.WithoutMiddleware("cache.headers").Get("/stream", streamController.Show)
})
```

## Read Routes From Runtime

Providers and application boot can add routes. Lens and Agents should use runtime output.

Incorrect:

```go
files, _ := filepath.Glob("routes/*.go")
_ = files
```

Correct:

```bash
go run . route:list --json
```
