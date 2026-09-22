# Expand/Contract Database Migrations

Zero-downtime schema evolution using the expand/contract pattern, with separate databases and
logical replication for the cases a single database cannot cover.

## Origin

The **expand/contract** pattern (also called *parallel change*) was articulated by
[Pete Hodgson](https://martinfowler.com/bliki/ParallelChange.html) on martinfowler.com. The core
idea: first *expand* the system so it supports both old and new simultaneously, then *contract*
by removing the old once everything has migrated.

Applied to databases, schema changes are split into two releases: an additive expand migration
(new columns, tables, indexes, no drops or renames) and a later contract migration that removes
deprecated structures after cutover.

**Reference:** Hodgson, Pete. "Parallel Change". *martinfowler.com*, 2014.
<https://martinfowler.com/bliki/ParallelChange.html>

## How gobank-db implements it

The `db.Apply` runner in this repository is the expand/contract discipline as code. Every
migration carries a phase, and the phase decides what the SQL may contain:

| Phase | May contain | Lint forbids |
|---|---|---|
| `Expand` | New tables, columns, indexes, views, backfills | `DROP …`, `RENAME`, `ALTER COLUMN … TYPE`, `TRUNCATE`, `DELETE` |
| `Cutover` | `DROP VIEW` + `CREATE VIEW` | Everything above except `DROP VIEW` |
| `Contract` | Anything | Nothing |

`Apply` validates the whole list before touching the database, so a destructive statement in an
expand migration fails the deploy, not the rollback. Each migration then runs in its own
transaction with its `schema_migrations` row. The full cycle is walked by `TestApplyFullCycle`
in `migrate_test.go` and described on the [contract views](contract-views.html) page.

Within one database that is the whole story. The rest of this page covers the case the single
database cannot: moving to a different Postgres version, zone or provider.

## Why separate databases?

The [blue-green schemas](blue-green-schemas.html) approach (same database, two schemas or two
table sets) works well for data swaps but does not cover PostgreSQL version upgrades, zone
failovers, or cross-provider migrations. Using **separate database instances** makes all of
these routine:

- PG version upgrades (e.g. 15 → 17): green runs the new version
- Zone/region failover: green is provisioned in a different availability zone
- Cloud provider migration: green can be on a different provider entirely
- Major schema refactors: green gets the target schema from scratch

The same pipeline handles all cases. Routine becomes boring, which is the goal.

## Workflow overview

![Expand/Contract workflow: blue DB, application with feature flag, green DB with replication](expand-contract-workflow.svg)

## Release phases

![Six phases: Provision, Expand, Replicate, Cutover, Contract, Teardown](expand-contract-phases.svg)

| Phase | Action | Constraint |
|---|---|---|
| 1. Provision | Create green DB instance | Can be a different PG version, zone, or provider |
| 2. Expand | Apply additive-only schema to green | No drops, no renames; replication requires schema compatibility |
| 3. Replicate | Start logical replication blue → green; wait for lag → 0 | PG 10+ for logical replication; PG 17+ for `pg_createsubscriber` |
| 4. Cutover | Flip feature flag / connection string to green | Design for idempotency around the switch point |
| 5. Contract | Stop replication; drop deprecated columns/tables on green | Only safe after replication is stopped and blue traffic is zero |
| 6. Teardown | Remove blue DB; green becomes next cycle's blue | Keep blue as rollback target until confident |

## Replication

PostgreSQL logical replication copies row-level changes from a publication on blue to a
subscription on green. The expand migration must be applied to green *before* replication
starts, since logical replication transfers data, not DDL.

### Setup (PG 10+)

```sql
-- On blue
CREATE PUBLICATION blue_pub FOR ALL TABLES;

-- On green (after expand migration)
CREATE SUBSCRIPTION blue_to_green
  CONNECTION 'host=blue-db dbname=mydb ...'
  PUBLICATION blue_pub;
```

### PG 17+ shortcut

`pg_createsubscriber` converts a physical standby into a logical replica, creating publications
and subscriptions without copying initial table data.

### Teardown

```sql
-- After cutover
DROP SUBSCRIPTION blue_to_green;  -- on green
DROP PUBLICATION blue_pub;        -- on blue
```

## Go migration tooling

Several Go tools can manage the expand and contract migrations. The key criterion is whether
the tool can enforce "expand only" (no destructive operations) during phase 2.

| Tool | Approach | Expand/Contract fit |
|---|---|---|
| **gobank-db `Apply`** (this repo) | Versioned SQL in Go, phase per migration, one transaction each | Built for it. `Lint` refuses destructive DDL in `Expand` and `Cutover`. No declarative diffing; you write the SQL. |
| [Atlas](https://atlasgo.io/) | Declarative schema-as-code. Diffs desired state vs live DB, generates a migration plan. Also supports versioned SQL files. | Strong. `atlas schema diff` generates the expand migration; migration linting catches destructive ops; two schema directories (`schema/`, `schema-final/`) formalise the boundary. |
| [golang-migrate/migrate](https://github.com/golang-migrate/migrate) | SQL up/down files. CLI + Go library. Supports `io/fs` embedding. | Good. Expand and contract as separate migration pairs. No built-in lint. |
| [pressly/goose](https://github.com/pressly/goose) | SQL files + Go-function migrations. CLI + library. | Good. Go-function migrations useful for backfills. Same manual discipline as migrate. |
| [dbmate](https://github.com/amacneil/dbmate) | Language-agnostic CLI. Pure SQL migrations. | Adequate. Lightweight; no lint or declarative diffing. |

Atlas remains the choice if declarative diffing is wanted. Its schema layout formalises the
same split gobank-db expresses with phases:

```
schema/
  schema.hcl          # expand-safe target (additive only)
schema-final/
  schema.hcl          # clean final state (old stuff removed)
scripts/
  create_publication.sql
  create_subscription.sql
  wait_for_sync.sh
```

```bash
# Preview expand migration
atlas schema diff --from "$BLUE_DB_URL" --to file://schema --dev-url "docker://postgres"

# Apply expand to green
atlas schema apply --url "$GREEN_DB_URL" --to file://schema --dev-url "docker://postgres"

# Apply contract after cutover
atlas schema apply --url "$GREEN_DB_URL" --to file://schema-final --dev-url "docker://postgres"
```

## Taskfile integration

```yaml
tasks:
  green:migrate-expand:
    desc: Apply additive-only schema to green
    cmds:
      - go run ./cmd/migrate -dsn "{{.GREEN_DB_URL}}" -upto expand
  green:replicate:
    desc: Start logical replication blue→green
    cmds:
      - psql "{{.BLUE_DB_URL}}" -f scripts/create_publication.sql
      - psql "{{.GREEN_DB_URL}}" -f scripts/create_subscription.sql
  green:wait-sync:
    desc: Wait for replication lag to reach zero
    cmds:
      - scripts/wait_for_sync.sh "{{.GREEN_DB_URL}}"
  green:cutover:
    desc: Switch feature flag to green
    cmds:
      - echo "flip feature flag / update connection string"
  green:contract:
    desc: Drop replication, then the deprecated structures
    cmds:
      - psql "{{.GREEN_DB_URL}}" -c "DROP SUBSCRIPTION blue_to_green;"
      - psql "{{.BLUE_DB_URL}}" -c "DROP PUBLICATION blue_pub;"
      - go run ./cmd/migrate -dsn "{{.GREEN_DB_URL}}" -upto contract
  deploy:
    desc: Full blue→green cutover
    cmds:
      - task: green:migrate-expand
      - task: green:replicate
      - task: green:wait-sync
      - task: green:cutover
      - task: green:contract
```

## Comparison with blue-green schemas

| | Blue-green schemas (same DB) | Expand/contract (separate DBs) |
|---|---|---|
| Scope | Data swaps, ETL refresh | Schema evolution, PG upgrades, zone failover |
| Mechanism | `search_path` switch, or view swap | Connection string switch + logical replication |
| PG version change | Not possible | Native: green runs the new version |
| Cross-zone/provider | No | Yes |
| Rollback speed | Sub-second | Seconds (flip connection string back) |
| Complexity | Low | Medium: replication setup and monitoring |
| Best for | Periodic full data rebuilds | Routine release-cycle schema changes |

## Caveats

| Concern | Notes |
|---|---|
| Logical replication limits | DDL not replicated; sequences not synced; large objects not supported. All tables need a replica identity (primary key or `REPLICA IDENTITY FULL`). |
| Expand discipline | Expand migrations must be additive only. Any drop or rename breaks replication or loses data. `db.Lint` enforces this. |
| Replication lag | High-write workloads take time to sync. Monitor `pg_stat_subscription` and wait for lag → 0 before cutover. |
| Sequence continuity | Sequences are not replicated. If using sequences (not UUIDs), set green sequences above blue's current max before cutover. |
| In-flight transactions | Transactions open at cutover may fail. Design for retry/idempotency at the application level. |
| Cached plans | pgx caches prepared statements per connection. A cutover that changes a view's column set makes `SELECT *` fail once with SQLSTATE `0A000`; named columns are unaffected. See [contract views](contract-views.html). |
