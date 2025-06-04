package sqlpt_test

import (
	"testing"

	"github.com/ciffelia/sqlpt"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDifferentDatabase(t *testing.T) {
	ctx := t.Context()

	var name1, name2 string

	sqlpt.WithTestDB(t, func(pool *pgxpool.Pool) {
		err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name1)
		if err != nil {
			t.Fatalf("failed to get current database name: %v", err)
		}
	})
	sqlpt.WithTestDB(t, func(pool *pgxpool.Pool) {
		err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name2)
		if err != nil {
			t.Fatalf("failed to get current database name: %v", err)
		}
	})

	if name1 == name2 {
		t.Errorf("expected different database names, got %s and %s", name1, name2)
	}
}

func TestCreateAndQueryTable(t *testing.T) {
	ctx := t.Context()

	sqlpt.WithTestDB(t, func(pool *pgxpool.Pool) {
		// Create a test table
		_, err := pool.Exec(ctx, `
			CREATE TABLE users (
				id SERIAL PRIMARY KEY,
				name VARCHAR(100) NOT NULL,
				email VARCHAR(100) UNIQUE NOT NULL
			)
		`)
		if err != nil {
			t.Fatalf("failed to create users table: %v", err)
		}

		// Insert test data
		_, err = pool.Exec(ctx,
			"INSERT INTO users (name, email) VALUES ($1, $2)",
			"John Doe", "john@example.com")
		if err != nil {
			t.Fatalf("failed to insert user: %v", err)
		}

		// Query and verify data
		var id int
		var name, email string
		err = pool.QueryRow(ctx,
			"SELECT id, name, email FROM users WHERE email = $1",
			"john@example.com").Scan(&id, &name, &email)
		if err != nil {
			t.Fatalf("failed to query user: %v", err)
		}

		if name != "John Doe" {
			t.Errorf("expected name 'John Doe', got '%s'", name)
		}
		if email != "john@example.com" {
			t.Errorf("expected email 'john@example.com', got '%s'", email)
		}
	})
}

func TestConcurrentDatabases(t *testing.T) {
	// Run parallel tests to verify isolation
	t.Run("test1", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		sqlpt.WithTestDB(t, func(pool *pgxpool.Pool) {
			_, err := pool.Exec(ctx, "CREATE TABLE test1_table (id INT)")
			if err != nil {
				t.Fatalf("failed to create test1_table: %v", err)
			}

			// Verify the table exists in this database
			var exists bool
			err = pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'test1_table')").
				Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check table existence: %v", err)
			}
			if !exists {
				t.Error("test1_table should exist")
			}
		})
	})

	t.Run("test2", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		sqlpt.WithTestDB(t, func(pool *pgxpool.Pool) {
			// Verify that test1_table does not exist in this separate database
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'test1_table')").
				Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check table existence: %v", err)
			}
			if exists {
				t.Error("test1_table should not exist in this isolated database")
			}
		})
	})
}
