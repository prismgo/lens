# Prismgo Lens Core

- Prismgo Lens is a development-only tool and must not be imported by production application code.
- Prefer PrismGo facades and existing project patterns before adding new abstractions.
- Call `application-info` and `search-docs` before changing PrismGo framework behavior.
- Production goroutines must use the project routine helper or recover explicitly.
