# Architecture Best Practices

## Optimize For Readability First

Prefer code that reads in the order it runs: controller parses input, service applies use-case rules, repository performs storage access, and framework facades provide infrastructure.

Incorrect:

```go
func Save(c *gin.Context) {
	var req SaveRequest
	_ = c.ShouldBindJSON(&req)
	_ = database.DB().Create(&req).Error
	response.JSON(c, http.StatusOK, req)
}
```

Correct:

```go
func (c *ArticleController) Store(ctx *gin.Context) {
	var req StoreArticleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Fail(ctx, http.StatusBadRequest, err)
		return
	}

	article, err := c.service.Create(ctx.Request.Context(), req)
	if err != nil {
		response.Fail(ctx, http.StatusInternalServerError, err)
		return
	}

	response.JSON(ctx, http.StatusCreated, article)
}
```

Keep the successful path easy to scan, and keep framework wiring separate from use-case logic.

## Keep Responsibilities Separate At Every Level

Single responsibility applies to packages, files, structs, functions, and methods. Each unit should have one clear reason to change and one boundary that callers can understand.

- Packages group one capability, not unrelated helpers.
- Files group closely related types or lifecycle code, not every concern in the package.
- Structs hold one role's state and dependencies.
- Functions do one operation at one abstraction level.
- Controllers translate HTTP input and output.
- Services coordinate use cases, policies, and transactions.
- Repositories own query shape and persistence details.
- Jobs, commands, listeners, and schedules own asynchronous entry points.
- Providers register framework services, commands, routes, middleware, and configuration.

Incorrect:

```go
type ArticleController struct {
	db    *gorm.DB
	cache cache.Repository
	queue queue.Dispatcher
}

func (c *ArticleController) Publish(ctx *gin.Context) {
	var article Article
	c.db.First(&article, ctx.Param("id"))
	article.Status = "published"
	c.db.Save(&article)
	c.cache.Forget(ctx.Request.Context(), fmt.Sprintf("articles:%d", article.ID))
	c.queue.Dispatch(ctx.Request.Context(), ReindexArticleJob{ID: article.ID})
	response.JSON(ctx, http.StatusOK, article)
}
```

Correct:

```go
func (c *ArticleController) Publish(ctx *gin.Context) {
	article, err := c.service.Publish(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		response.Fail(ctx, http.StatusInternalServerError, err)
		return
	}

	response.JSON(ctx, http.StatusOK, article)
}
```

If a file, struct, or function needs the words "and", "or", "manager", "helper", or "common" to describe what it does, split the responsibility or give it a narrower name.

## Name Packages By Capability

Package names should be short, lower-case, and describe what the package provides to callers. Avoid vague package names such as `util`, `common`, `base`, or `helpers`.

Incorrect:

```go
package util

func ArticleCacheKey(id uint) string { return fmt.Sprintf("articles:%d", id) }
func ParseClock(value string) (time.Time, error) { return time.Parse("15:04", value) }
```

Correct:

```go
package articlecache

func Key(id uint) string {
	return fmt.Sprintf("articles:%d", id)
}
```

Keep package APIs cohesive enough that callers know where behavior belongs without reading the implementation.

## Reuse Through Focused Abstractions

Extract repeated behavior only after the responsibility is clear. Prefer small helpers with precise names over broad `util`, `common`, `manager`, or catch-all service packages.

Incorrect:

```go
package util

func DoEverything(ctx context.Context, db *gorm.DB, key string, value string) error {
	_ = cache.Put(ctx, key, value, time.Minute)
	return db.Exec("UPDATE articles SET status = ?", value).Error
}
```

Correct:

```go
func rememberArticleStatus(ctx context.Context, articleID uint, status string) error {
	key := fmt.Sprintf("articles:%d:status", articleID)
	return cache.Put(ctx, key, status, 5*time.Minute)
}
```

If a helper starts accepting unrelated dependencies, mode flags, or callbacks for unrelated behavior, split it by responsibility.

## Keep Public APIs Small

Expose stable package-level facades and contracts. Put detailed driver behavior behind package internals, repositories, stores, or connectors.

Correct:

```go
package service

import (
	"context"
	"time"

	"github.com/prismgo/framework/cache"
)

func RememberStatus(ctx context.Context, key string, value string) error {
	return cache.Put(ctx, key, value, 5*time.Minute)
}
```

Return concrete types from implementation packages unless callers need an interface. Define small interfaces at the consuming boundary, not in the package that implements the concrete type.

```go
type ArticleReader interface {
	Find(ctx context.Context, id uint) (Article, error)
}
```

## Make Control Flow And Dependencies Explicit

Pass `context.Context`, request data, dependencies, and options explicitly. Avoid hidden work through package-level mutable state, implicit query execution, or boolean flags whose meaning is unclear.

`context.Context` should be the first parameter of request-scoped functions. Do not store it in structs; pass it through the call chain.

Incorrect:

```go
type ArticleRepository struct {
	ctx context.Context
	db  *gorm.DB
}

func LoadArticle(id uint, includeAuthor bool) (Article, error) {
	query := database.DB().WithContext(context.Background()).Model(&Article{})
	if includeAuthor {
		query = query.Preload("Author")
	}
	var article Article
	return article, query.First(&article, id).Error
}
```

Correct:

```go
type ArticleQuery struct {
	WithAuthor bool
}

func (r *ArticleRepository) Find(ctx context.Context, id uint, query ArticleQuery) (Article, error) {
	db := r.db.WithContext(ctx).Model(&Article{})
	if query.WithAuthor {
		db = db.Preload("Author")
	}

	var article Article
	err := db.First(&article, id).Error
	return article, err
}
```

Avoid mutable globals for dependencies or test seams. Prefer dependency injection through constructors or providers.

Incorrect:

```go
var now = time.Now

func expiresAt() time.Time {
	return now().Add(time.Hour)
}
```

Correct:

```go
type Clock interface {
	Now() time.Time
}

type Expirer struct {
	clock Clock
}

func (e Expirer) ExpiresAt() time.Time {
	return e.clock.Now().Add(time.Hour)
}
```

## Handle Errors Once And Explicitly

Never discard errors with `_`. Handle an error, wrap and return it, or intentionally translate it at a boundary. Keep the normal path at low indentation by checking errors and returning early.

Incorrect:

```go
func (s *ArticleService) Create(ctx context.Context, req StoreArticleRequest) Article {
	article, _ := s.repo.Create(ctx, req)
	return article
}
```

Correct:

```go
func (s *ArticleService) Create(ctx context.Context, req StoreArticleRequest) (Article, error) {
	article, err := s.repo.Create(ctx, req)
	if err != nil {
		return Article{}, fmt.Errorf("create article: %w", err)
	}
	return article, nil
}
```

Log or render errors at process and transport boundaries. Do not log and return the same error from every internal layer unless the extra log adds distinct operational context.

Incorrect:

```go
func (s *ArticleService) Publish(ctx context.Context, id uint) (Article, error) {
	article, err := s.repo.Find(ctx, id)
	if err != nil {
		logger.Errorf("find article: %v", err)
		return Article{}, err
	} else {
		article.Status = "published"
		return s.repo.Save(ctx, article)
	}
}
```

Correct:

```go
func (s *ArticleService) Publish(ctx context.Context, id uint) (Article, error) {
	article, err := s.repo.Find(ctx, id)
	if err != nil {
		return Article{}, fmt.Errorf("find article: %w", err)
	}

	article.Status = "published"
	return s.repo.Save(ctx, article)
}
```

## Design Query Shape Deliberately

Repositories should minimize database round trips and make query cost visible. Avoid N+1 queries, select only needed columns, and use bulk reads or `Preload` for relationships.

Incorrect:

```go
for i := range articles {
	if err := db.First(&articles[i].Author, articles[i].AuthorID).Error; err != nil {
		return err
	}
}
```

Correct:

```go
err := db.
	Select("id", "author_id", "title", "published_at").
	Preload("Author", func(tx *gorm.DB) *gorm.DB {
		return tx.Select("id", "name")
	}).
	Find(&articles).Error
```

For large data sets, use indexed filters, ID-based batching, streaming rows, and aggregate queries instead of loading full tables into memory. Add composite indexes that match frequent `WHERE`, `JOIN`, `GROUP BY`, and `ORDER BY` patterns.

## Prefer Low-Complexity Data Structures

Choose data structures that match access patterns. Use maps for repeated lookups, specify slice and map capacity on hot paths, and avoid nested scans when one pass can build an index.

Incorrect:

```go
for i := range articles {
	for _, author := range authors {
		if articles[i].AuthorID == author.ID {
			articles[i].Author = author
		}
	}
}
```

Correct:

```go
authorsByID := make(map[uint]Author, len(authors))
for _, author := range authors {
	authorsByID[author.ID] = author
}

for i := range articles {
	articles[i].Author = authorsByID[articles[i].AuthorID]
}
```

When the final size is known or reasonably bounded, preallocate the output container.

```go
summaries := make([]ArticleSummary, 0, len(articles))
for _, article := range articles {
	summaries = append(summaries, ArticleSummary{
		ID:    article.ID,
		Title: article.Title,
	})
}
```

Do not trade readability for micro-optimizations on cold paths. Optimize hot paths with benchmarks, profiles, or clear query evidence.
