# Roadmap

## v0.1 — Foundation

- Core schema definition (tables shared across gobank services)
- Connection factory: pglike (dev/WASM), Postgres, CockroachDB
- Schema migration runner (expand/contract pattern)
- Multi-service table namespace isolation

## Future

- Tenant isolation for multi-bank scenarios
- Connection pool tuning and health checks
- Read replica routing
- Audit logging at the DB layer
