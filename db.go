// Package db provides shared database infrastructure for gobank services —
// statement execution, schema management, and migration orchestration.
package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ExecStatements splits a multi-statement SQL string on semicolons and
// executes each non-empty statement individually. This is required because
// the pglike (SQLite) driver does not support multiple statements in a
// single Exec call.
func ExecStatements(db *sql.DB, sql string) error {
	for stmt := range strings.SplitSeq(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", FirstLine(stmt), err)
		}
	}
	return nil
}

// FirstLine returns the first line of s, for use in error messages.
func FirstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
