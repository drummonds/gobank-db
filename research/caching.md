# Caching: Data Classes Decide What May Be Cached

Services on the shared database want to avoid repeated lookups of data that
does not change. go-luca, for example, reads both accounts' exponents on every
movement. A cache that each service invents for itself gets invalidation wrong
in ways the database cannot see: go-luca's first exponent cache kept entries
written inside a transaction that was later rolled back. This page proposes
that gobank-db owns caching, and that **what may be cached follows from a
class recorded against each contract-view column**. The same class carries
retention and PII, which the family needs for other reasons.

Status: design, not built.

## Vocabulary

| Term | Meaning |
|---|---|
| contract column | A column of a contract view. Classes attach here, not to physical tables, because physical tables are swapped behind the view. |
| kind | `value`: the state of something at a time (a balance, an exponent). `delta`: a change to a value (a posting). |
| mutability | `immutable`: fixed once the row exists. `append-only`: rows are added, never changed. `mutable`: may be updated. |
| derived from | For a value maintained from deltas: the delta view it summarises (balance ← postings for the same account). |
| retention | How long the row is kept, as an ISO 8601 duration (`P7Y`). |
| pii | The personal-data category, or none. |
| data class | The combination of the above for one contract column. |
| cacheable | Derived from the data class (decision table below); never declared directly. |
| budget | The maximum number of entries a cache may hold. |
| registry | The process-wide set of caches, which can shrink them all under memory pressure. |

A class belongs to a column rather than a table because mutability differs
within a row: `accounts.exponent` is immutable, `accounts.name` is mutable.

## When a column may be cached

One row per case. v1 builds only the first row as cacheable. The derived-value
row is the planned next step; mutable values and PII stay uncached.

| kind | mutability | pii | Example | Cache? | Invalidation |
|---|---|---|---|---|---|
| value | immutable | none | account exponent, commodity code | yes (v1) | none for correctness; evict on budget |
| delta | append-only | none | postings | no — individual rows are not re-read | – |
| value | mutable, derived from deltas | none | balance | later (v2) | stale once a delta for the same key has a higher sequence than the entry's watermark |
| value | mutable | none | account name | no | would need cross-process notification |
| any | any | any category | customer address | never | a cached copy escapes erasure and retention |

A cache over several columns is cacheable only if every column is.

## Metadata table

Classes live in a table written by migrations, so any service (and any SQL
client) can read them, and a cutover that changes a column's meaning changes
its class in the same transaction.

```sql
CREATE TABLE data_classes (
    view_name    VARCHAR(100) NOT NULL,
    column_name  VARCHAR(100) NOT NULL,
    kind         VARCHAR(10)  NOT NULL CHECK (kind IN ('value', 'delta')),
    mutability   VARCHAR(20)  NOT NULL CHECK (mutability IN ('immutable', 'append-only', 'mutable')),
    derived_from VARCHAR(100),          -- delta view name; NULL unless derived
    retention    VARCHAR(20),           -- ISO 8601 duration; NULL = indefinite
    pii          VARCHAR(30),           -- category; NULL = not personal data
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (view_name, column_name)
);
```

The key is the natural pair rather than a UUID: like `schema_migrations`, this
is a small catalogue with no hotspot risk. gobank-db creates the table
alongside `schema_migrations`; services fill it with ordinary `INSERT`s in
their Expand migrations. A migration that adds a column to a contract view
should add its class in the same migration — `Validate` can enforce that once
views are introspectable on both backends (open question below).

Retention and PII are **recorded, not enforced** by this design. Enforcement
(purging, erasure) is a separate story that reads the same table.

## Go surface (sketch)

```go
classes, err := db.LoadClasses(ctx, d)            // reads data_classes

reg := db.NewRegistry()
exps, err := db.NewCache[string, int](reg, classes,
    "accounts", []string{"exponent"}, 10_000)      // errors if any column is not cacheable

v, ok := exps.Get(id)                              // miss → caller reads the DB
exps.Put(id, exponent)                             // outside a transaction

tx, err := db.Begin(ctx, d)                        // *db.Tx wraps *sql.Tx
exps.Stage(tx, id, exponent)                       // visible to this tx only
tx.Commit()                                        // staged entries published on success; dropped on rollback
```

`NewCache` refuses columns that the decision table says are not cacheable, so
the contract is checked when the service starts, not discovered in
production. Budget is in entries, not bytes: v1 caches only small scalar
values, so entries are uniform and counting bytes would be speculative.

## Transactions

`database/sql` has no commit hook, and a ledger handed a `*sql.Tx` never
learns whether it committed. `db.Tx` wraps `*sql.Tx` with commit/rollback
callbacks. A cache write made inside a transaction is staged against that
transaction and reaches the shared cache only after a successful commit.

![Cache write inside a transaction reaches the shared cache only on commit](cache-tx-commit.svg)

This removes go-luca's rollback defect by construction. go-luca's `WithTx`
then takes a `*db.Tx` rather than a `*sql.Tx` — a breaking change to its API.

## Memory pressure

The requirement is *slower but stable*: under memory stress the system gives
up cache hits, never correctness or the process.

- **Budget per cache** with least-recently-used eviction. A miss always falls
  back to the database, so eviction only costs a query.
- **Registry shrink.** `reg.Shrink(fraction)` evicts that share of every
  cache's entries; `reg.Purge()` empties them all.
- **Watcher.** `db.WatchMemory(ctx, reg, high)` polls `runtime/metrics` and,
  when heap use crosses `high` × the soft limit (`GOMEMLIMIT`, read with
  `debug.SetMemoryLimit(-1)`), shrinks the registry and halves the effective
  budgets until use falls back; budgets recover gradually. No soft limit set
  means no watcher action — budgets alone bound the caches.

Go gives no memory-pressure signal, and `weak` pointers are cleared at every
garbage collection regardless of pressure, so neither replaces an explicit
budget.

## Across processes

Many services share one database, so a cache can only see its own process's
writes. v1 sidesteps this by caching only immutable values: no other process
can change them. Mutable values would need Postgres `LISTEN/NOTIFY` (not
available on pglike) or a time-to-live (which accepts stale reads); both are
out of scope.

## Open questions

- **Immutable is not eternal.** A retention purge or erasure deletes an
  immutable row; another process's cache still holds it. Foreign keys make
  this harmless for accounts (an account with movements cannot be deleted
  before them), but the general rule needs deciding before purging is built.
- **Enforcing "every contract column has a class"** needs view-column
  introspection that works on pglike as well as Postgres.
- **go-luca's `full_path`** appears in the exponent-mismatch error. If paths
  can be renamed it is mutable and must not be cached; the error path can read
  it from the database instead, since mismatches are rare.
- **Watermark design for derived values** (v2): which delta column is the
  sequence, and how the key links value to deltas.
