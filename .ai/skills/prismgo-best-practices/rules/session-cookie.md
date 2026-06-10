# Session And Cookie

## Use Session Middleware And Request Stores

Read and write session values through the request store exposed on `gin.Context`.

Incorrect:

```go
c.SetCookie("session", id, 3600, "/", "", false, false)
```

Correct:

```go
func RememberLocale(c *gin.Context) {
	if err := session.Put(c, "locale", "en"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"locale": session.Get(c, "locale", "en")})
}
```

## Rotate Session IDs On Privilege Changes

Regenerate or invalidate sessions after login, logout, or privilege changes.

```go
if err := session.Regenerate(c); err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
	return
}
```

## Use Flash APIs For One-Request Data

```go
_ = session.Flash(c, "status", "saved")
_ = session.Keep(c, "status")
```

## Queue Cookies Through Cookie Helpers

Use cookie queue helpers and configured options instead of direct `http.SetCookie` calls spread through handlers.

Incorrect:

```go
http.SetCookie(w, &http.Cookie{Name: "remember", Value: raw, HttpOnly: false})
```

Correct:

```go
_, err := cookie.QueueMakeFrom(c, "remember", value, 60, cookie.HTTPOnly(true))
if err != nil {
	return err
}
```

Never log raw session identifiers or cookie values.
