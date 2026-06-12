# Repository Guidelines

## Karpathy Guidelines

These rules reduce common LLM coding mistakes. Merge them with project-specific instructions as needed. They intentionally favor caution over speed; use judgment for trivial tasks.

### 1. Think Before Coding

Do not assume, hide confusion, or skip tradeoffs.

- State assumptions before implementation; ask when uncertain.
- If there are multiple valid interpretations, present them instead of silently choosing.
- Prefer the simpler approach when it solves the problem; push back when scope looks unnecessary.
- If requirements are unclear, stop, name the ambiguity, and ask.

### 2. Simplicity First

Write the minimum code that solves the request.

- Do not add unrequested features, abstractions, flexibility, configurability, or impossible-case handling.
- If the solution is much larger than necessary, simplify it.

### 3. Surgical Changes

Touch only what the request requires.

- Do not improve, refactor, reformat, or delete adjacent code unless required.
- Match existing style.
- Mention unrelated dead code; do not remove it unless asked.
- Remove only imports, variables, functions, or other orphans created by your own changes.
- Every changed line must trace directly to the request.

### 4. Goal-Driven Execution

Define success criteria and verify them.

- Convert tasks into testable goals, e.g. validation gets invalid-input tests, bug fixes get reproduction tests, refactors preserve passing tests before and after.
- For multi-step work, state a brief plan:

```text
1. [Step] -> verify: [check]
2. [Step] -> verify: [check]
3. [Step] -> verify: [check]
```

Strong success criteria enable independent loops; weak criteria such as "make it work" require clarification.

These guidelines are working when diffs are smaller, rewrites are rarer, and clarifying questions happen before implementation mistakes.

## Hard Rules

1. Never modify production code solely for testing.
   - No test-only logic, APIs, helpers, fallbacks, or workarounds.
   - Production code may change only to fix genuine production bugs.
2. Never keep compatibility code after feature changes without approval.
3. Never ignore errors; return errors whenever possible.
4. Never change public APIs unless explicitly requested.

## Build, Test, and Development Commands

- `go test ./...`: run the full Go test suite.
- `make test`: run verbose tests with count coverage and write `coverage.out`.
- `make covdata`: run `./.github/scripts/coverage.sh` with `PACKAGES` support and write coverage artifacts under `.coverage/`.
- `make vet`: run `go vet ./...`.
- `make fmt`: format all Go files with `gofmt`, excluding `./tmp`.
- `make fmt-check`: verify formatting without modifying files.
- `make lint`: run `golangci-lint run`; pass extra options with `LINT_ARGS`, e.g. `make lint LINT_ARGS=--verbose`.
- `make ci`: run the local CI gate.

## Coding Style and Standards

Follow standard Go conventions: `gofmt`, tabs, short package names, exported `PascalCase`, unexported `camelCase`, and APIs consistent with nearby code.

General principles:

- Reuse existing code; if boundaries are unclear, refactor properly.
- Keep functions, structs, interfaces, files, and packages single-purpose with clear boundaries.
- Prefer the Go standard library, minimize third-party dependencies, and get user approval before adding one.
- Favor explicit logic over implicit behavior.
- Use consistent, clear names for classes, functions, variables, tables, and fields.

Comments:

- Modified and newly added code, including unit tests, must include comments explaining logic, design rationale, parameter purposes, and complex logic where relevant.

## Testing Guidelines

Add or update colocated `*_test.go` files for behavior changes. Use focused unit tests for package contracts and integration-style tests for cross-component behavior such as queue, Redis, RabbitMQ, or filesystem flows. Run `make test` before submitting; CI uploads `coverage.out`, so do not bypass `make test` for final verification.

### Testing and Coverage

- Any change to Go code, `go.mod`, `go.sum`, tests, or code generation logic must run tests and collect coverage.
- Collect coverage via the OS-appropriate project script:
  - Linux/macOS/Git Bash: `make covdata`.
  - Narrow scope: `make covdata PACKAGES=./cache` or `./.github/scripts/coverage.sh ./cache`.
- Coverage output goes under `.coverage/`; the script fixes Go build cache to `tmp/gocache`.
- Before final delivery, run the script for the changed scope, e.g. `make covdata PACKAGES=./cache`.
- Changed-scope coverage must exceed `90%`; add tests if it does not.
- If full `covdata` is blocked by existing flaky tests, rerun failing packages in isolation and explain which tests failed, whether they relate to the change, and why the full run is not considered passing.

## Security and Configuration

Do not commit secrets, local credentials, coverage files, or temporary runtime data.

## Completion Checklist

After completing a feature:

1. Check for orphaned/dead code. Report findings first, then confirm before deleting.
2. Check for compatibility/fallback code. Report findings first, then confirm before deleting.
3. Run static analysis only for packages containing changed code, e.g. `golangci-lint run --verbose ./cmd/prismgo-lens/...`.
4. Run formatting with `gofmt`.
5. Write `docs/changes/v{next}-{function-description}.md` in Chinese, incrementing `{next}` numerically, with these sections:
   - Feature overview and implementation goals
   - Requirements / business background
   - Impact scope
   - Which files were modified
   - What behavioral changes were made
   - Which checks were executed and a summary of the results
   - What logic is covered by unit tests, with detailed explanation for complex logic
   - Risks and optimization suggestions
   - Orphaned/dead code
   - Compatibility/fallback code
   - Outstanding/incomplete items
   - If `docs/changes` is ignored, do not commit the document.

## Final Response for Go Code Changes

For every Go code change task, report:

- The actual coverage command executed.
- Whether coverage came from unit tests, integration tests, or both.
- Total statement coverage.
- Packages or functions with significantly low coverage.
- If coverage was skipped or partial, the exact reason.
