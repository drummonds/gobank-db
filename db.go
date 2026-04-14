// Package db provides shared database infrastructure for gobank services —
// statement execution, schema management, and migration orchestration.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "codeberg.org/hum3/go-postgres"  // registers "pglike" driver
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
)

// Schema is the SQL to create the core gobank tables.
// Uses VARCHAR(36) for UUID columns and CURRENT_TIMESTAMP for defaults
// so the same DDL works on both pglike and real Postgres.
const Schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id VARCHAR(36) PRIMARY KEY,
	name VARCHAR(100) NOT NULL,
	currency VARCHAR(3) NOT NULL DEFAULT 'GBP',
	balance BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ledger_entries (
	id VARCHAR(36) PRIMARY KEY,
	account_id VARCHAR(36) NOT NULL REFERENCES accounts(id),
	amount BIGINT NOT NULL,
	description VARCHAR(255) NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// Open opens a database connection using the given DSN.
// Use ":memory:" for pglike in-memory, or a postgres:// URL for real Postgres.
func Open(dsn string) (*sql.DB, error) {
	driver := "pglike"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		driver = "pgx"
	}
	return sql.Open(driver, dsn)
}

// Migrate runs the schema DDL against the given database.
// Statements are split on ";" and executed individually for pglike compatibility.
func Migrate(ctx context.Context, d *sql.DB) error {
	for _, stmt := range strings.Split(Schema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.Migrate: %w", err)
		}
	}
	return nil
}
