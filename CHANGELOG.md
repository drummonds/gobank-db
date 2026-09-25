# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [0.2.0] - 2026-09-25

 - Expand/contract migration runner with contract views; research docs move here from gobank

### Added
- `Apply`: versioned migration runner with a `schema_migrations` table, one
  transaction per migration, and resume-after-failure.
- `Migration`, `Phase` (`Expand`, `Cutover`, `Contract`), `Validate`, `Lint`:
  expand and cutover migrations are refused if they contain destructive DDL.
- `SplitStatements`: statement splitter aware of string literals, quoted
  identifiers, dollar quoting, comments and `BEGIN ... END` bodies.
- Tests walk a full expand → cutover → contract cycle around a contract view,
  on pglike and on Postgres 17 (`GOBANK_TEST_DSN`).

- Research notes moved here from gobank: Blue-Green Schemas, Expand/Contract
  Migrations, and a new Contract Views page that walks the test suite's
  expand → cutover → contract cycle. Built from `research/*.md` by
  `task docs:build` and published at gobank-db.docs.bytestone.uk.

### Changed
- Docs deploy switched from statichost to rsync (`gobank-db.docs.bytestone.uk`).
- `Migrate` now uses `SplitStatements` instead of splitting on every `;`.
- README rewritten to describe what the package actually provides, with the
  contract-view rules and backend differences.
- go-postgres bumped to v0.5.13; v0.5.3 still declared the codeberg module
  path and broke the build after the forge migration.

## [0.1.1] - 2026-04-14

 - Move test schema to test code, add docs page setup

## [0.1.0] - 2026-04-14

 - gitignore

### Added
- Initial project scaffold
