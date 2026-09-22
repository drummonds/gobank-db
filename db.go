// Package db provides shared database infrastructure for gobank services —
// connection opening, versioned expand/contract migrations, and the
// contract-view conventions that let services share one database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "git.bytestone.uk/hum3/go-postgres" // registers "pglike" driver
	_ "github.com/jackc/pgx/v5/stdlib"    // registers "pgx" driver
)

// Open opens a database connection using the given DSN.
// Use ":memory:" for pglike in-memory, or a postgres:// URL for real Postgres.
func Open(dsn string) (*sql.DB, error) {
	driver := "pglike"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		driver = "pgx"
	}
	return sql.Open(driver, dsn)
}

// Migrate runs the given schema DDL against the database, statement by
// statement, with no version tracking. It is for fixed, idempotent schemas
// (CREATE TABLE IF NOT EXISTS ...). For evolving schemas use Apply.
func Migrate(ctx context.Context, d *sql.DB, schema string) error {
	for _, stmt := range SplitStatements(schema) {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.Migrate: %w", err)
		}
	}
	return nil
}
