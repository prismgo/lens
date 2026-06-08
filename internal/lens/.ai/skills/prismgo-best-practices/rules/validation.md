# Validation

## Validate At The Boundary

Controllers should bind request DTOs and return before calling services when input is invalid.

Incorrect:

```go
func Create(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	_ = createFromRaw(c.Request.Context(), body)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

Correct:

```go
type CreateUserRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

func Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	resp, err := createUser(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
```

## Keep Authorization Separate

Validation confirms shape and format. Authorization belongs in middleware, policies, or service-level permission checks.

```go
route.Post("/users", canCreateUsers, createUserHandler)
```

## Test Accepted And Rejected Inputs

Test the public HTTP or service boundary. Do not add production-only getters or helper methods just to inspect private validation state.

```go
w := httptest.NewRecorder()
req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{}`))
router.ServeHTTP(w, req)
```
