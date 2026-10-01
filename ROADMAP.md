# Roadmap

## v0.2 — Migrations and contract views

- [x] Connection opening: pglike (dev/WASM) and Postgres via pgx
- [x] Versioned migration runner (`Apply`) with `schema_migrations`
- [x] Expand / cutover / contract phases with destructive-DDL lint
- [x] Contract views with in-database blue/green swap, tested on pglike and Postgres 17
- [ ] Advisory lock so concurrent `Apply` calls from several replicas serialise (Postgres only)
- [ ] Shared core schema (tables common to gobank services) expressed as migrations here

## Future

- Component catalog in the database: migrations record which component owns
  each table and contract view, so a gobank-db database describes its own
  structure. Satisfies go-dbexplorer's `Catalog` interface (via a host
  adapter — the explorer doesn't import gobank-db) and lets its standalone CLI
  show components with no host code. RBAC stays out of this module.
- `search_path`-based blue/green once go-postgres gains schema support
- CockroachDB backend
- Tenant isolation for multi-bank scenarios
- Connection pool tuning and health checks
- Read replica routing
- Audit logging at the DB layer
