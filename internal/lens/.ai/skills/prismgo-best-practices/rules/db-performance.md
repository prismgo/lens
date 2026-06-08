# Database Performance Best Practices

## Always Eager Load Relationships

Lazy loading causes N+1 query problems - one query per loop iteration. Always use `Preload()` or explicit bulk queries to load relationships upfront.

Incorrect (N+1 - executes 1 + N queries):

```go
var articles []models.Article
db.Find(&articles)
for _, article := range articles {
	var author models.Author
	db.First(&author, article.AuthorID)
	fmt.Println(author.Name)
}
```

Correct (2 queries total):

```go
var articles []models.Article
db.Preload("Author").Find(&articles)
for _, article := range articles {
	fmt.Println(article.Author.Name)
}
```

Constrain eager loads to select only needed columns. Always include the foreign key or primary key needed to match the relationship:

```go
var articles []models.Article
db.Select("id", "author_id", "code", "created_at").
	Preload("Author", func(tx *gorm.DB) *gorm.DB {
		return tx.Select("id", "name", "phone").
			Where("is_enabled = ?", true).
			Order("id DESC").
			Limit(10)
	}).
	Find(&articles)
```

## Prevent Lazy Loading in Development

Catch N+1 issues during development with repository tests and query logging. Prismgo applications should keep relationship loading explicit at the repository boundary.

```go
func TestArticleListDoesNotQueryAuthorPerRow(t *testing.T) {
	// Use the repository list method and assert it preloads or bulk-loads related rows.
	items, err := repo.List(ctx, scopeID, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 0 && items[0].Author == nil {
		t.Fatal("expected author relation to be loaded by the repository")
	}
}
```

Development logging should make unexpected repeated SQL visible, but the fix belongs in repository query shape, not in handlers or templates.

## Select Only Needed Columns

Avoid `SELECT *` - especially when tables have large text or JSON columns.

Incorrect:

```go
var articles []models.Article
db.Preload("Author").Find(&articles)
```

Correct:

```go
var articles []models.Article
db.Select("id", "author_id", "code", "status", "created_at").
	Preload("Author", func(tx *gorm.DB) *gorm.DB {
		return tx.Select("id", "name", "phone")
	}).
	Find(&articles)
```

When selecting columns on preloaded relationships, always include the key columns or the relationship will not match.

## Chunk Large Datasets

Never load thousands of records at once. Use batching for batch processing.

Incorrect:

```go
var accounts []models.Account
db.Find(&accounts)
for _, account := range accounts {
	notifyWeeklyDigest(ctx, account.ID)
}
```

Correct:

```go
const batchSize = 200
for lastID := uint(0); ; {
	var accounts []models.Account
	if err := db.Where("id > ? AND subscribed = ?", lastID, true).
		Order("id ASC").
		Limit(batchSize).
		Find(&accounts).Error; err != nil {
		return err
	}
	if len(accounts) == 0 {
		break
	}
	for _, account := range accounts {
		notifyWeeklyDigest(ctx, account.ID)
		lastID = account.ID
	}
}
```

Use ID-based batching when modifying records during iteration. Offset pagination can skip or repeat rows when records change.

## Add Database Indexes

Index columns that appear in `WHERE`, `ORDER BY`, `JOIN`, and `GROUP BY` clauses.

Incorrect:

```go
schema.Bind(db).Create("orders", func(table *schema.Blueprint) {
	table.ID()
	table.ForeignId("account_id").Constrained("accounts")
	table.String("status", 32)
	table.Timestamps()
})
```

Correct:

```go
schema.Bind(db).Create("orders", func(table *schema.Blueprint) {
	table.ID()
	table.ForeignId("account_id").Constrained("accounts")
	table.String("status", 32).Index()
	table.Timestamps()
	table.IndexNamed("idx_orders_status_created_at", "status", "created_at")
})
```

Add composite indexes for common query patterns, such as `WHERE status = ? ORDER BY created_at`.

## Use Count Aggregates for Counting Relations

Never load entire collections just to count them.

Incorrect:

```go
var articles []models.Article
db.Preload("Comments").Find(&articles)
for _, article := range articles {
	fmt.Println(len(article.Comments))
}
```

Correct:

```go
type ArticleWithCommentCount struct {
	models.Article
	CommentsCount int64 `gorm:"column:comments_count"`
}

var articles []ArticleWithCommentCount
db.Table("articles").
	Select("articles.*, COUNT(comments.id) AS comments_count").
	Joins("LEFT JOIN comments ON comments.article_id = articles.id").
	Group("articles.id").
	Scan(&articles)
```

Conditional counting:

```go
db.Table("articles").
	Select(`
		articles.*,
		COUNT(comments.id) AS comments_count,
		SUM(CASE WHEN comments.approved = ? THEN 1 ELSE 0 END) AS approved_comments_count
	`, true).
	Joins("LEFT JOIN comments ON comments.article_id = articles.id").
	Group("articles.id").
	Scan(&articles)
```

## Use `Rows()` for Memory-Efficient Iteration

For read-only iteration over large result sets, stream rows instead of loading all records into memory.

Incorrect:

```go
var accounts []models.Account
db.Where("active = ?", true).Find(&accounts)
```

Correct:

```go
rows, err := db.Model(&models.Account{}).
	Select("id").
	Where("active = ?", true).
	Rows()
if err != nil {
	return err
}
defer rows.Close()

for rows.Next() {
	var accountID uint
	if err := rows.Scan(&accountID); err != nil {
		return err
	}
	queue.Dispatch(ctx, ProcessAccountJob{AccountID: accountID})
}
```

Use row streaming for read-only iteration. Use ID-based batches when modifying records.

## No Queries in Templates Or Handlers

Never execute queries in frontend templates or view rendering code. Pass data from controllers through services and repositories.

Incorrect:

```go
// Handler builds a response row by querying related data inside a render loop.
for _, account := range accounts {
	var accountDetail models.AccountDetail
	db.Where("account_id = ?", account.ID).First(&accountDetail)
	rows = append(rows, rowFrom(account, accountDetail))
}
```

Correct:

```go
// Repository
var accounts []models.Account
db.Preload("AccountDetail").Find(&accounts)

// Handler
response.JSON(c, http.StatusOK, accounts)
```
