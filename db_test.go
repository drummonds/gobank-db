package db

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// openTestDB returns a database connection for testing.
// By default uses pglike (:memory:). Set GOBANK_TEST_DSN to a postgres:// URL
// to test against real Postgres.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOBANK_TEST_DSN")
	if dsn == "" {
		dsn = ":memory:"
	}
	d, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open(%q): %v", dsn, err)
	}
	t.Cleanup(func() {
		if dsn != ":memory:" {
			d.Exec("DROP TABLE IF EXISTS ledger_entries")
			d.Exec("DROP TABLE IF EXISTS accounts")
		}
		d.Close()
	})
	return d
}

func TestMigrate(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Running twice should be idempotent (IF NOT EXISTS).
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("Migrate (idempotent): %v", err)
	}
}

func TestInsertAndQueryAccount(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var id string
	err := d.QueryRowContext(ctx,
		"INSERT INTO accounts (id, name, currency) VALUES (gen_random_uuid(), $1, $2) RETURNING id",
		"Cash", "GBP",
	).Scan(&id)
	if err != nil {
		t.Fatalf("INSERT account: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty UUID")
	}

	var name, currency string
	var balance int64
	err = d.QueryRowContext(ctx,
		"SELECT name, currency, balance FROM accounts WHERE id = $1", id,
	).Scan(&name, &currency, &balance)
	if err != nil {
		t.Fatalf("SELECT account: %v", err)
	}
	if name != "Cash" || currency != "GBP" || balance != 0 {
		t.Fatalf("got (%q, %q, %d), want (Cash, GBP, 0)", name, currency, balance)
	}
}

func TestLedgerEntry(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var accountID string
	err := d.QueryRowContext(ctx,
		"INSERT INTO accounts (id, name) VALUES (gen_random_uuid(), $1) RETURNING id", "Current",
	).Scan(&accountID)
	if err != nil {
		t.Fatalf("INSERT account: %v", err)
	}

	var entryID string
	err = d.QueryRowContext(ctx,
		"INSERT INTO ledger_entries (id, account_id, amount, description) VALUES (gen_random_uuid(), $1, $2, $3) RETURNING id",
		accountID, 1500, "Opening deposit",
	).Scan(&entryID)
	if err != nil {
		t.Fatalf("INSERT ledger_entry: %v", err)
	}

	var amount int64
	var desc string
	err = d.QueryRowContext(ctx,
		"SELECT amount, description FROM ledger_entries WHERE account_id = $1", accountID,
	).Scan(&amount, &desc)
	if err != nil {
		t.Fatalf("SELECT ledger_entry: %v", err)
	}
	if amount != 1500 || desc != "Opening deposit" {
		t.Fatalf("got (%d, %q), want (1500, Opening deposit)", amount, desc)
	}
}

func TestMultipleAccounts(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, name := range []string{"Cash", "Savings", "Credit"} {
		_, err := d.ExecContext(ctx,
			"INSERT INTO accounts (id, name) VALUES (gen_random_uuid(), $1)", name,
		)
		if err != nil {
			t.Fatalf("INSERT %s: %v", name, err)
		}
	}

	var count int
	err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count)
	if err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if count != 3 {
		t.Fatalf("got %d accounts, want 3", count)
	}
}
