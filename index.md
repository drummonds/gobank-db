# gobank-db — Documentation

Shared database layer for [gobank](https://git.bytestone.uk/hum3/gobank) — expand/contract migrations, contract views, and dual-backend support (pglike + Postgres).

## Pages

- [README](README.html) — project overview and testing instructions
- [CHANGELOG](CHANGELOG.html) — release history
- [ROADMAP](ROADMAP.html) — planned work

## Research

These notes moved here from the gobank project because this is the package that implements
them.

- [Contract Views](contract-views.html) — blue-green and expand/contract in one database, as walked by the test suite
- [Expand/Contract Migrations](expand-contract.html) — the pattern, the `Apply` runner, and the separate-database form with logical replication
- [Blue-Green Schemas](blue-green-schemas.html) — the `search_path` form in PostgreSQL and how the view swap stands in for it

## Links

- **Source:** [git.bytestone.uk/hum3/gobank-db](https://git.bytestone.uk/hum3/gobank-db)
- **Mirror:** [github.com/drummonds/gobank-db](https://github.com/drummonds/gobank-db)
- **Documentation:** [gobank-db.docs.bytestone.uk](https://gobank-db.docs.bytestone.uk)
