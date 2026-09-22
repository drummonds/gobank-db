# gobank-db

Shared database layer for the [gobank](https://git.bytestone.uk/hum3/gobank) family: one
database, many services, evolved with expand/contract migrations and contract views. Runs
on [go-postgres](https://git.bytestone.uk/hum3/go-postgres) (pglike, in-memory) for
development and tests, and on PostgreSQL (via pgx) in production.

## What it provides

- **`Open(dsn)`** — a `*sql.DB` on pglike (`:memory:` or a file) or Postgres (`postgres://` URL).
- **`Migrate(ctx, db, ddl)`** — run a fixed, idempotent schema script with no version tracking.
- **`Apply(ctx, db, migrations)`** — a versioned migration runner with a `schema_migrations`
  table, one transaction per migration, and phase-aware linting.
- **`Lint(phase, sql)`** — refuses destructive DDL in migrations that must stay reversible.
- **`SplitStatements(script)`** — splits SQL on top-level semicolons, respecting string
  literals, quoted identifiers, dollar quoting, comments and `BEGIN ... END` trigger bodies.

## Expand / contract migrations

Every migration carries a version, a name, a phase and a SQL script:

```go
var migrations = []db.Migration{
    {1, "accounts table and contract view", db.Expand, `
        CREATE TABLE accounts_blue (id VARCHAR(36) PRIMARY KEY, name VARCHAR(100) NOT NULL, balance BIGINT NOT NULL DEFAULT 0);
        CREATE VIEW accounts AS SELECT id, name, balance FROM accounts_blue;`},
    {2, "green copy with opened_on", db.Expand, `
        CREATE TABLE accounts_green (id VARCHAR(36) PRIMARY KEY, name VARCHAR(100) NOT NULL, balance BIGINT NOT NULL DEFAULT 0, opened_on DATE NOT NULL);
        INSERT INTO accounts_green SELECT id, name, balance, '2000-01-01' FROM accounts_blue;`},
    {3, "flip accounts view to green", db.Cutover, `
        DROP VIEW accounts;
        CREATE VIEW accounts AS SELECT id, name, balance, opened_on FROM accounts_green;`},
    {4, "retire blue", db.Contract, `DROP TABLE accounts_blue;`},
}

applied, err := db.Apply(ctx, d, migrations)
```

The phases follow the [expand/contract](https://gobank-db.docs.bytestone.uk/expand-contract.html)
pattern, with the [blue-green](https://gobank-db.docs.bytestone.uk/blue-green-schemas.html)
switch done by repointing a view rather than `search_path`, so it works in one database on
both backends:

| Phase      | May contain                                   | Lint forbids                                                   |
|------------|-----------------------------------------------|----------------------------------------------------------------|
| `Expand`   | New tables, columns, indexes, views, backfills | `DROP …`, `RENAME`, `ALTER COLUMN … TYPE`, `TRUNCATE`, `DELETE` |
| `Cutover`  | `DROP VIEW` + `CREATE VIEW`                    | Everything above except `DROP VIEW`                            |
| `Contract` | Anything                                       | Nothing                                                        |

`Apply` validates the whole list (`Validate`) before touching the database, so a destructive
statement in an expand migration fails the deploy rather than the rollback. Each migration
then runs in its own transaction together with its `schema_migrations` row. A failure leaves
earlier migrations applied and the failing one rolled back; fixing it and re-running `Apply`
continues from there.

## Contract views

Services never name a physical table. They read and write a **view** whose column list is the
contract, and the physical tables behind it (`accounts_blue`, `accounts_green`, …) can be
rebuilt and swapped underneath. The `DROP VIEW; CREATE VIEW` pair in a `Cutover` migration is
atomic on both backends, and rollback is the same pair pointed back.

Rules that keep this honest:

- **Name your columns.** After a cutover changes a view's column set, pgx's cached prepared
  statement for `SELECT *` fails once with SQLSTATE `0A000` ("cached plan must not change
  result type"); pgx evicts it and the next call succeeds. Queries that list their columns
  are unaffected.
- **Read through views, write to tables you own.** Postgres auto-updates simple views; SQLite
  (pglike) needs `INSTEAD OF` triggers. Keeping contract views read-only avoids the
  difference.
- **Design for one retry around the cutover.** Transactions open when the view flips may fail.

## Backend differences

| Capability                                   | pglike | Postgres |
|----------------------------------------------|--------|----------|
| Transactional DDL (migration rollback)       | yes    | yes      |
| `CREATE VIEW`, `DROP VIEW`, `pg_views`       | yes    | yes      |
| `CREATE OR REPLACE VIEW`                     | no     | yes      |
| `CREATE SCHEMA`, `SET search_path`           | no     | yes      |
| Concurrent `Apply` from several processes    | n/a    | no lock yet — run one at a time |

## Testing

Tests run against **pglike** by default, so no server is needed:

```bash
task test
```

To run the same suite against a real PostgreSQL instance, set `GOBANK_TEST_DSN`:

```bash
podman run -d --rm --name pg -e POSTGRES_PASSWORD=test -e POSTGRES_DB=gobank_test -p 54329:5432 docker.io/library/postgres:17-alpine
GOBANK_TEST_DSN="postgres://postgres:test@127.0.0.1:54329/gobank_test?sslmode=disable" task test
podman stop pg
```

## Documentation

Published at <https://gobank-db.docs.bytestone.uk>. The research notes that motivated this
package live here:

- [Contract Views](https://gobank-db.docs.bytestone.uk/contract-views.html)
- [Expand/Contract Migrations](https://gobank-db.docs.bytestone.uk/expand-contract.html)
- [Blue-Green Schemas](https://gobank-db.docs.bytestone.uk/blue-green-schemas.html)

## Status

Early development. See [issues](https://git.bytestone.uk/hum3/gobank-db/issues) and the
[roadmap](ROADMAP.md).

## Links

- **Source:** https://git.bytestone.uk/hum3/gobank-db
- **Mirror:** https://github.com/drummonds/gobank-db
- **Documentation:** https://gobank-db.docs.bytestone.uk
