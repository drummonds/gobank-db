# Blue-Green Schemas in PostgreSQL

A pattern for zero-downtime data swaps using two parallel schemas within a single database.

## Concept

Two schemas (`blue`, `green`) coexist in one database. Traffic routes to one via
`search_path`. The inactive schema is rebuilt while the active one serves queries. At time T,
the switch is made.

**Key constraint:** app code must never schema-qualify table names. All routing is via
`search_path`.

## Setup

```sql
CREATE SCHEMA blue;
CREATE SCHEMA green;
GRANT USAGE ON SCHEMA blue, green TO app_role;
```

## Switching

### Ad-hoc

```sql
ALTER DATABASE mydb SET search_path TO green, public;
```

### Scheduled (pg_cron)

```sql
SELECT cron.schedule('flip-schema', '0 0 1 4 *',
  $$ALTER DATABASE mydb SET search_path TO green, public$$);
```

### Application-controlled (recommended)

The app evaluates a switch time on each connection checkout:

```go
func schemaForNow(switchAt time.Time) string {
    if time.Now().After(switchAt) {
        return "green"
    }
    return "blue"
}

// On connection checkout:
conn.Exec("SET search_path TO " + schemaForNow(switchAt) + ", public")
```

The switch time can be read from config or the database so it is adjustable without
redeployment.

## Production pattern

Combine both layers:

1. `pg_cron` flips the database-level default at T
2. The app re-evaluates `search_path` on each connection checkout
3. A pool drain at T flushes stale connections

This gives automatic cutover, no stale connections, and instant rollback.

## Rollback

```sql
ALTER DATABASE mydb SET search_path TO blue, public;
```

Sub-second. The old schema is untouched until explicitly dropped.

## Retire and rebuild cycle

```
[active: blue] → build green → flip to green → drain pool
                                              → blue now idle
                                              → rebuild blue for next cycle
```

Drop when no longer needed as a rollback target:

```sql
DROP SCHEMA blue CASCADE;
```

## Caveats

| Concern | Notes |
|---|---|
| Sequences | Schema-local sequences reset on rebuild; use shared sequences in `public` if continuity matters |
| Foreign keys | Cross-schema FKs work but complicate `DROP SCHEMA`; avoid |
| Connection pooling | PgBouncer caches `search_path`; must flush the pool or use a startup query |
| In-flight transactions | Transactions open at T straddle the switch; design for idempotency around cutover |
| Schema-qualified queries | Any hardcoded `blue.tablename` in app or migrations breaks the abstraction |

## Best suited for

- ETL / bulk data refresh (load into the inactive schema, flip)
- Read-heavy datasets with periodic full rebuilds
- Time-triggered data releases

Less suited to high-write OLTP, where coordinating in-flight writes across the cutover adds
complexity.

## How gobank-db applies this

The `search_path` form needs `CREATE SCHEMA`, which the pglike development backend does not
support (see [contract views](contract-views.html) for the backend table). gobank-db therefore
uses the same idea one level down: blue and green are **tables** (`accounts_blue`,
`accounts_green`) in one schema, and the switch is a **view** (`accounts`) that is dropped and
recreated inside one transaction. The rule "never schema-qualify a table" becomes "never name a
physical table", which is the contract-view rule anyway.

That swap is exercised end to end, on pglike and on Postgres 17, by `TestApplyFullCycle` in
`migrate_test.go`. The `search_path` variant stays on the roadmap for when go-postgres gains
schema support.
