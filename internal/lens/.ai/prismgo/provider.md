# PrismGo Providers

- Register framework services in provider code, not at random call sites.
- Keep provider boot methods focused on binding, event registration, and startup configuration.
- Do not make providers depend on request-specific state.
