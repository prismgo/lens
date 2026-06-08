# Go Core

- Keep exported interfaces small and make errors explicit.
- Prefer standard library APIs unless the host project already depends on a focused package.
- Run narrow package tests after Go code changes, then the repository coverage workflow when the host project requires it.
- Avoid hidden fallback behavior that makes production failures harder to see.
