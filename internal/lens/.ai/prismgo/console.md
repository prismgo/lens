# PrismGo Console

- Console commands should validate input, return explicit errors, and avoid silent side effects.
- Reuse application services instead of duplicating business logic inside command handlers.
- Prefer dry-run output for destructive or bulk operations.
