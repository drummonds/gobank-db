# gobank-db

Core database abstraction for gobank — shared schema, multi-user base, built on [go-postgres](https://codeberg.org/hum3/go-postgres).

Part of the [gobank](https://codeberg.org/hum3/gobank) family of libraries.

## Overview

gobank-db provides:

- **Shared Schema** — common database schema used across gobank services
- **Multi-User Base** — abstracts DB technology so multiple services/users share one database
- **Migration Support** — schema versioning and migration management
- **Connection Management** — pooled connections with go-postgres (pglike for dev, Postgres/CockroachDB for prod)

## Status

Early development. See [issues](https://codeberg.org/hum3/gobank-db/issues) for planned work.

## Links

- **Source:** https://codeberg.org/hum3/gobank-db
- **Mirror:** https://github.com/drummonds/gobank-db
