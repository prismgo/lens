# Migration Best Practices

## Generate Migrations with Artisan-Style Commands

Always use the Prismgo migration generator when available so names, timestamps, packages, and registration code stay consistent.

Incorrect (manually created file):

```go
// database/migrations/articles_migration.go  <- wrong naming, no timestamp
```

Correct (Prismgo-generated):

```bash
go run ./ make:migration create_articles_table
go run ./ make:migration add_slug_to_articles_table
```

## Use `Constrained()` for Foreign Keys

Use Prismgo schema foreign key helpers for automatic naming and referential integrity.

```go
table.ForeignId("account_id").Constrained("accounts").CascadeOnDelete()

// Non-standard names
table.ForeignId("author_id").Constrained("accounts")
```

## Never Modify Deployed Migrations

Once a migration has run in production, treat it as immutable. Create a new migration to change the table.

Incorrect (editing a deployed migration):

```go
// 202604280001_create_articles_table.go - already in production
table.String("slug").Unique() // <- added after deployment
```

Correct (new migration to alter):

```go
// 202606040001_add_slug_to_articles_table.go
func Up(db *gorm.DB) error {
	return schema.Bind(db).Table("articles", func(table *schema.Blueprint) {
		table.String("slug").Unique().After("title")
	})
}
```

## Add Indexes in the Migration

Add indexes when creating the table, not as an afterthought. Columns used in `WHERE`, `ORDER BY`, and `JOIN` clauses need indexes.

Incorrect:

```go
return schema.Bind(db).Create("orders", func(table *schema.Blueprint) {
	table.ID()
	table.ForeignId("account_id").Constrained("accounts")
	table.String("status", 32)
	table.Timestamps()
})
```

Correct:

```go
return schema.Bind(db).Create("orders", func(table *schema.Blueprint) {
	table.ID()
	table.ForeignId("account_id").Constrained("accounts")
	table.IndexNamed("idx_orders_account_id", "account_id")
	table.String("status", 32).Index()
	table.Timestamp("shipped_at").Nullable().Index()
	table.Timestamps()
	table.IndexNamed("idx_orders_status_created_at", "status", "created_at")
})
```

## Mirror Defaults in Model Fields

When a column has a database default, mirror it in the model tags or constructor path so new instances have correct values before saving.

```go
// Migration
table.String("status", 32).Default("pending")

// Model
type Article struct {
	Status string `gorm:"size:32;not null;default:pending" json:"status"`
}
```

## Write Reversible Down Migrations by Default

Implement rollback functions for schema changes that can be safely reversed so rollback works in CI and failed deployments.

```go
func Down(db *gorm.DB) error {
	return schema.Bind(db).Table("articles", func(table *schema.Blueprint) {
		table.DropColumn("slug")
	})
}
```

For intentionally irreversible migrations, such as destructive data backfills, return a clear error and require a forward fix migration instead of pretending rollback is supported.

```go
func Down(*gorm.DB) error {
	return fmt.Errorf("migration 202606040002_backfill_article_status does not support rollback")
}
```

## Keep Migrations Focused

One concern per migration. Do not mix DDL schema changes and DML data manipulation in the same migration unless the change is an explicitly documented, one-off data migration.

Incorrect (partial failure creates unrecoverable state):

```go
func Up(db *gorm.DB) error {
	if err := schema.Bind(db).Create("settings", func(table *schema.Blueprint) {
		table.ID()
		table.String("key", 191).Unique()
		table.String("value")
	}); err != nil {
		return err
	}

	return db.Table("settings").Create(map[string]any{
		"key": "version", "value": "1.0",
	}).Error
}
```

Correct (separate migrations):

```go
// Migration 1: create_settings_table
func Up(db *gorm.DB) error {
	return schema.Bind(db).Create("settings", func(table *schema.Blueprint) {
		table.ID()
		table.String("key", 191).Unique()
		table.String("value")
	})
}

// Migration 2: seed_default_settings
func Up(db *gorm.DB) error {
	return db.Table("settings").Create(map[string]any{"key": "version", "value": "1.0"}).Error
}
```
