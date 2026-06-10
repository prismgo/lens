# Database And Schema

## Use The Schema Builder In Migrations

Use `schema.Bind(db)` inside migration functions so schema changes use the migration connection or transaction.

Incorrect:

```go
func Up(db *gorm.DB) error {
	return db.Exec("CREATE TABLE reports (id bigint unsigned primary key)").Error
}
```

Correct:

```go
func Up(db *gorm.DB) error {
	return schema.Bind(db).Create("reports", func(table *schema.Blueprint) {
		table.ID()
		table.String("name", 191)
		table.Timestamps()
	})
}
```

## Set Global Schema Defaults In A Provider

Defaults such as string length, time precision, and morph key type belong in provider bootstrapping.

```go
func (p AppServiceProvider) Register(app providercontract.Application) error {
	schema.DefaultStringLength(191)
	schema.DefaultTimePrecision(nil)
	return nil
}
```

## Inspect Metadata With Read APIs

Use schema metadata helpers for inspection. Do not mutate schema from diagnostic or Agent-facing tools.

Incorrect:

```go
db.Exec("ALTER TABLE reports ADD COLUMN status varchar(32)")
```

Correct:

```go
func ReportColumns() ([]string, error) {
	return schema.GetColumnListing("reports")
}
```

## Preserve Driver Output Shape

MySQL, PostgreSQL, SQLite, and unsupported drivers may differ in available metadata, but tooling should keep the same top-level shape for tables, columns, indexes, and foreign keys.

```json
{
  "tables": [],
  "columns": [],
  "indexes": [],
  "foreign_keys": []
}
```
