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

## Prior art

None of this is new; gobank-db combines established ideas. Grouped by the question each answers:

**Why a shared database needs a contract at all**

- Fowler, Martin. "[IntegrationDatabase](https://martinfowler.com/bliki/IntegrationDatabase.html)".
  *martinfowler.com*, 2004. The shared-database integration style and why its schema becomes
  impossible to change.
- Wright, Hyrum. "[Hyrum's Law](https://www.hyrumslaw.com/)". Every observable behaviour gets
  depended on — which is what happens to a table any service may query.
- Parnas, D. L. "[On the Criteria To Be Used in Decomposing Systems into
  Modules](https://doi.org/10.1145/361598.361623)". *CACM* 15(12), 1972. Information hiding:
  the table is the secret, the view is the interface.

**Views as the stable surface**

- ANSI/X3/SPARC three-schema architecture (1975–78). External schemas (views) over a conceptual
  schema give logical data independence — the original form of the idea.
- Ambler, Scott W. and Sadalage, Pramod J. *Refactoring Databases: Evolutionary Database
  Design*. Addison-Wesley, 2006. The "[Encapsulate Table With
  View](https://databaserefactoring.com/EncapsulateTableWithView.html)" refactoring is exactly
  the first migration in the cycle above.
- Sadalage, Pramod and Fowler, Martin. "[Evolutionary Database
  Design](https://martinfowler.com/articles/evodb.html)". *martinfowler.com*, 2016.
- Newman, Sam. *Monolith to Microservices*. O'Reilly, 2019, ch. 4. The "Database View" and
  "Database-as-a-Service Interface" patterns for letting other services read a schema you are
  about to change.
- PostgREST. "[Schema Isolation](https://docs.postgrest.org/en/stable/explanations/schema_isolation.html)".
  Expose only a schema of views and functions; keep tables in a private schema. This is the
  `contract` schema form gobank-db will use once go-postgres supports schemas.

**Swapping what is behind the view**

- Oracle. "[Edition-Based Redefinition](https://docs.oracle.com/en/database/oracle/oracle-database/19/adfns/editions.html)".
  Applications reach tables only through *editioning views*, so a new edition can restructure
  them online — the industrial version of the blue/green view swap.
- Hodgson, Pete. "[Parallel Change](https://martinfowler.com/bliki/ParallelChange.html)" — see
  [expand/contract](expand-contract.html).

**Ownership and enforcing it**

- Richardson, Chris. "[Database per service](https://microservices.io/patterns/data/database-per-service.html)".
  *microservices.io*. Names *private-tables-per-service* as the lowest-overhead variant — one
  database, each table owned by one service.
- Grzybek, Kamil. "[Modular Monolith: Integration
  Styles](https://www.kamilgrzybek.com/blog/posts/modular-monolith-integration-styles)". The
  shared-database style inside a modular monolith, with per-module ownership.
- Shopify's [Packwerk](https://github.com/Shopify/packwerk) and
  [ArchUnit](https://www.archunit.org/). Module boundaries checked by static analysis in the
  test suite, with a recorded list of existing violations — the model for gobank's
  `TestContractViewRule` ([ADR-0001](https://git.bytestone.uk/hum3/gobank/src/branch/main/adr/0001-contract-views.md)).
- qntm. "[Ratchets in software development](https://qntm.org/ratchet)". A count of known
  violations that may only go down — the shape of gobank's contract-debt baseline.
