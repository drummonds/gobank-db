# Contract Views: Blue-Green and Expand/Contract in One Database

The [blue-green schemas](blue-green-schemas.html) and
[expand/contract](expand-contract.html) notes describe two mechanisms. This page is what
gobank-db actually does with them, and it is the story the test suite tells.

## The idea

A service never names a physical table. It reads a **view** whose column list is its contract
with the database. The tables behind the view can be built, filled and swapped without the
service noticing, and the swap is one transactional statement pair.

![Contract view swap: services read the accounts view; the view is repointed from accounts_blue to accounts_green](contract-view-swap.svg)

That gives blue-green without `search_path`, and expand/contract without a second database.
Both backends in use (pglike for development and the browser, Postgres in production) support
every statement involved:

| Capability | pglike | Postgres |
|---|---|---|
| Transactional DDL (migration rollback) | yes | yes |
| `CREATE VIEW`, `DROP VIEW`, `pg_views` | yes | yes |
| `CREATE OR REPLACE VIEW` | no | yes |
| `CREATE SCHEMA`, `SET search_path` | no | yes |

## The cycle, as migrations

This is `testMigrations` from `migrate_test.go`, lightly trimmed:

```go
var migrations = []db.Migration{
    {1, "accounts table and contract view", db.Expand, `
        CREATE TABLE accounts_blue (
            id VARCHAR(36) PRIMARY KEY, name VARCHAR(100) NOT NULL,
            currency VARCHAR(3) NOT NULL DEFAULT 'GBP', balance BIGINT NOT NULL DEFAULT 0);
        CREATE VIEW accounts AS
            SELECT id, name, currency, balance FROM accounts_blue;`},

    {2, "add opened_on", db.Expand, `
        ALTER TABLE accounts_blue ADD COLUMN opened_on DATE;`},

    {3, "green copy with opened_on backfilled", db.Expand, `
        CREATE TABLE accounts_green (
            id VARCHAR(36) PRIMARY KEY, name VARCHAR(100) NOT NULL,
            currency VARCHAR(3) NOT NULL DEFAULT 'GBP', balance BIGINT NOT NULL DEFAULT 0,
            opened_on DATE NOT NULL DEFAULT '2000-01-01');
        INSERT INTO accounts_green (id, name, currency, balance, opened_on)
            SELECT id, name, currency, balance, COALESCE(opened_on, '2000-01-01')
            FROM accounts_blue;`},

    {4, "flip accounts view to green", db.Cutover, `
        DROP VIEW accounts;
        CREATE VIEW accounts AS
            SELECT id, name, currency, balance, opened_on FROM accounts_green;`},

    {5, "retire blue", db.Contract, `
        DROP TABLE accounts_blue;`},
}
```

What the test checks at each step:

1. After v1 and v2 the view still has its original four columns. The additive change did not
   leak into the contract.
2. Running `Apply` again applies nothing.
3. A row read through the view before v4 reads back with the same balance after v4, now with
   `opened_on` alongside. The view's column list changed; the data did not.
4. After v5, `accounts_blue` is gone and `schema_migrations` records five versions with their
   phases.

Two further tests pin the safety properties. A list containing `DROP COLUMN` in an `Expand`
migration is refused before the migrations table is even created. A migration that fails on its
second statement is rolled back whole, the earlier one stays applied, and fixing the SQL and
re-running continues from there.

## Rollback

Reverting the cutover is the same pair pointed back, as a new migration:

```sql
DROP VIEW accounts;
CREATE VIEW accounts AS SELECT id, name, currency, balance FROM accounts_blue;
```

It is sub-second and it is why `Contract` comes last: while blue exists, rollback is free.

## Rules that keep it honest

**Name your columns.** This is the one lesson that only appeared on real Postgres. pgx caches
prepared statements per connection. After v4 changed the view's column set, the cached plan for
`SELECT * FROM accounts` failed once:

```
ERROR: cached plan must not change result type (SQLSTATE 0A000)
```

pgx evicts the entry on that error, so the retry succeeded. Queries that list their columns
were never affected, and the test records the retry deliberately so the behaviour stays
visible.

**Read through views, write to tables you own.** Postgres auto-updates simple views; SQLite
(pglike) needs `INSTEAD OF` triggers. Keeping contract views read-only avoids a backend
difference.

**Design for one retry around the cutover.** Transactions open when the view flips may fail.
This is the same caveat both original notes carry; the view swap does not remove it, it just
makes the window one statement wide.

**Never name a physical table.** `accounts_blue` and `accounts_green` appear only in
migrations. The moment a service queries one directly, the swap stops being safe.

## Running it yourself

```bash
task test                                  # pglike, in memory
podman run -d --rm --name pg -e POSTGRES_PASSWORD=test -e POSTGRES_DB=gobank_test \
  -p 54329:5432 docker.io/library/postgres:17-alpine
GOBANK_TEST_DSN="postgres://postgres:test@127.0.0.1:54329/gobank_test?sslmode=disable" task test
podman stop pg
```

Run the Postgres form with `-v -run TestApplyFullCycle` to see the 0A000 retry logged.

## Not yet done

- No advisory lock, so two replicas running `Apply` at once would race. Postgres only; run one
  at a time for now.
- The `search_path` form of blue-green waits on schema support in go-postgres.
