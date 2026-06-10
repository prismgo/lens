# Repository Guidelines

## Karpathy Guidelines

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## Non-Negotiable Rules
1. Never modify production code solely for testing.
  - No test-only logic, APIs, helpers, fallbacks, or workarounds.
  - Production code may only change to fix genuine production bugs.
2. Never keep compatibility code after feature changes without approval.
3. Never ignore errors.
  - Return errors whenever possible.
4. Never change public APIs unless explicitly requested.

## Build, Test, and Development Commands

- `go test ./...`: run the full Go test suite.
- `make test`: run verbose tests with count coverage and write `coverage.out`.
- `make covdata`: run `./.github/scripts/coverage.sh` with `PACKAGES` support and write coverage artifacts under `.coverage/`.
- `make vet`: run `go vet ./...`.
- `make fmt`: format all Go files with `gofmt`, excluding `./tmp`.
- `make fmt-check`: verify formatting without modifying files.
- `make lint`: run `golangci-lint run`; pass extra options with `LINT_ARGS`, for example `make lint LINT_ARGS=--verbose`.
- `make ci`: run the local CI gate.

## Coding Style & Coding Standards

Follow standard Go conventions: `gofmt` formatting, tabs for indentation, short package names, exported identifiers in `PascalCase`, and unexported identifiers in `camelCase`. Keep package APIs idiomatic and consistent with nearby components. 

### General Principles
- Prefer code reuse over reimplementation
  - If existing code doesn't fit or has unclear boundaries, refactor properly
- Follow single responsibility principle
  - For functions, structs, interfaces, files, packages, etc.
  - Keep boundaries clear and reasonable
- Prefer Go standard library; minimize third-party dependencies
  - Adding a new library requires user approval
- Favor explicit logic over implicit behavior
- Use consistent and clear naming (classes, functions, variables, tables, fields)

### Code Comments
- Modified and newly added code must include comments
  - Explanation of the logic
  - Design rationale
  - Additional explanation for complex logic within functions
  - Description of function parameter purposes
  - Including unit test code

## Testing Guidelines

Add or update colocated `*_test.go` files for behavior changes. Use focused unit tests for package-level contracts and integration-style tests where external behavior crosses components, such as queue, Redis, RabbitMQ, Horizon, or filesystem flows. Run `make test` before submitting. Coverage is uploaded from `coverage.out` in CI, so avoid bypassing `make test` for final verification.

### Testing and Coverage
- Any changes to Go code, go.mod, go.sum, test files, or code generation logic must run tests and compute coverage.
- Coverage must be collected via the project script, selecting the script based on the OS:
  - Linux/macOS/Git Bash: `make covdata`
  - For narrow-scope validation, pass `PACKAGES`, e.g., `make covdata PACKAGES=./cache`, or pass a package path to the script, e.g., `./.github/scripts/coverage.sh ./cache`
- Coverage output is placed in `.coverage/`; Go build cache is fixed to `tmp/gocache` by the script to avoid writing to the user's global cache.
- Before final delivery, run the appropriate OS script based on the scope of changes, e.g., `make covdata PACKAGES=./cache`
- Required coverage for the changed scope is > `90%`. If not met, additional test code must be added.
- If full covdata is blocked by existing flaky tests (e.g., timer-sensitive tests), you must rerun the failing package(s) in isolation and explain in the results which tests failed and whether they are related to the current changes. You cannot treat a failed full coverage run as passing.

## Security & Configuration Tips

Do not commit secrets, local credentials, coverage files, or temporary runtime data. 

## Checklist
After completing a feature, the following must be performed:

1. Check for orphaned (dead) code
   - Report any findings first, then confirm whether to delete.

2. Check for compatibility/fallback code
   - Report any findings first, then confirm whether to delete.

3. Run static analysis only for packages containing changed code, e.g. `golangci-lint run --verbose ./cmd/prismgo-lens/...`

4. Run formatting: `gofmt`

5. Output a summary document: `docs/changes/v{next}-{function-description}.md`, with the following requirements:
- Written in Chinese
- `{next}` increments numerically
- Contains the following sections:
  - Feature overview and implementation goals
  - Requirements / business background
  - Impact scope
  - Which files were modified
  - What behavioral changes were made
  - Which checks were executed and a summary of the results
  - What logic is covered by unit tests (complex logic requires detailed explanation)
  - Risks and optimization suggestions
  - Orphaned/dead code
  - Compatibility/fallback code
  - Outstanding/incomplete items
  - If `docs/changes` is ignored, do not commit the related documents.

6. Final response requirements

For every Go code change task, the final response must report:

- The actual coverage command executed.
- Whether the collected coverage is from unit tests, integration tests, or both.
- Total statement coverage.
- Packages or functions with significantly low coverage.
- If coverage is skipped or only partially run, the exact reason must be stated.
