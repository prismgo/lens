---
name: prismgo-best-practices
description: PrismGo framework best practices for architecture, providers, console, routing, database, queue, cache, filesystem, logging, security, and framework style.
---

# PrismGo Best Practices

Use this skill when changing PrismGo framework code, framework-adjacent application code, or Prismgo Lens developer tooling.

## Consistency First

Before writing code, confirm the active framework surface instead of relying on memory:

1. Read `application-info` for Go, PrismGo, Lens, database, queue, frontend, and feature roster context.
2. Search the affected PrismGo docs with `search-docs`; prefer module-filtered results when available.
3. Read project instructions such as `CLAUDE.md`, `AGENTS.md`, `CONTEXT.md`, and project `.ai/guidelines` if present.
4. Inspect the relevant PrismGo package source before writing examples, adding APIs, or changing conventions.

Keep this skill framework-level. Do not encode host application domains, product workflows, reports, account plans, or authorization models here.

## Quick Reference

- Providers: bind services in `Register`; register routes, events, commands, and publishable assets in `Boot`.
- Facades: use package-level facades such as `cache`, `queue`, `route`, `filesystem`, `logger`, `event`, `translation`, and `database/schema` instead of reaching into drivers from application code.
- Runtime metadata: list commands and routes from runtime registries; do not infer them from source text.
- Configuration: read environment variables only while registering config, then consume namespaced config values.
- Safety: Agent-facing tools are read-only by default; diagnostics are named, schema-bound, timeout-bound, and never arbitrary Go evaluation.
- Testing: run the narrow PrismGo package tests affected by your change; use the host project's documented coverage command when the project requires one.

## Rule Files

Load only the files relevant to the task:

- `rules/architecture.md` - package boundaries, framework/application separation, and Lens isolation.
- `rules/provider-facade.md` - service providers, facades, contracts, deferrable providers, and commands from providers.
- `rules/command-console.md` - command definitions, input/output, runtime listing, and command metadata.
- `rules/routing.md` - route registration, groups, middleware, names, resources, and runtime route discovery.
- `rules/database-schema.md` - migrations, schema builder, metadata inspection, and read-only database tooling.
- `rules/migrations.md` - migration generation, foreign keys, immutable deployed migrations, indexes, defaults, rollback, and focused changes.
- `rules/db-performance.md` - eager loading, column selection, batching, indexes, aggregate counts, streaming, and query placement.
- `rules/queue-jobs.md` - jobs, dispatch options, middleware, batches, failures, and queue diagnostics.
- `rules/horizon.md` - Horizon configuration, supervisors, observability, dashboard, and process safety.
- `rules/scheduling.md` - scheduler overlap locks, multi-server coordination, queued long work, environment gates, timeouts, and shared registration helpers.
- `rules/caching.md` - cache repositories, typed reads, stores, locks, tags, memoization, and stampede control.
- `rules/config-env.md` - config registration, env boundaries, config helpers, and secret redaction.
- `rules/error-handling.md` - boundary errors, exception handling, panic recovery, and diagnostic failures.
- `rules/logging.md` - channels, stacks, structured fields, context fields, and secret-safe logs.
- `rules/events-notifications.md` - events, listeners, subscribers, queued listeners, and side-effect isolation.
- `rules/filesystem.md` - disks, logical keys, uploads, URLs, temporary URLs, and custom drivers.
- `rules/rate-limiting.md` - named limiters, middleware, manual counters, key design, and reset behavior.
- `rules/translation.md` - translation keys, namespaces, locale resolution, pluralization, and missing keys.
- `rules/session-cookie.md` - session middleware, request stores, flash data, cookie queues, signing, and encryption.
- `rules/validation.md` - Gin request binding, boundary validation, and validation tests.
- `rules/frontend-vue-vite.md` - Vue/Vite conventions for PrismGo-served frontend assets.
- `rules/testing-coverage.md` - package-focused tests, Lens tests, coverage expectations, and regression shape.
- `rules/security.md` - read-only SQL, diagnostics, browser logs, secrets, and generated AI assets.
- `rules/style.md` - local Go style, comments, public APIs, and small focused changes.
