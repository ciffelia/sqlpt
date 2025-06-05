package sqlpt

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ciffelia/sqlpt/internal"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Ensure DATABASE_URL is set for tests
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Println("FAIL: DATABASE_URL environment variable must be set")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestRun_HappyPath(t *testing.T) {
	var executedPool *pgxpool.Pool
	var executed bool

	Run(t, func(pool *pgxpool.Pool) {
		ctx := t.Context()

		executed = true
		executedPool = pool
		require.NotNil(t, pool)

		// Test basic database operations
		_, err := pool.Exec(ctx, "CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)")
		require.NoError(t, err)

		_, err = pool.Exec(ctx, "INSERT INTO test_table (name) VALUES ($1)", "test_value")
		require.NoError(t, err)

		var name string
		err = pool.QueryRow(ctx, "SELECT name FROM test_table WHERE id = 1").Scan(&name)
		require.NoError(t, err)
		assert.Equal(t, "test_value", name)
	})

	assert.True(t, executed)
	assert.NotNil(t, executedPool)
}

func TestRun_MissingDatabaseURL(t *testing.T) {
	// Save original DATABASE_URL
	originalURL := os.Getenv("DATABASE_URL")
	defer func() {
		_ = os.Setenv("DATABASE_URL", originalURL)
	}()

	// Clear DATABASE_URL
	_ = os.Unsetenv("DATABASE_URL")

	// This should panic
	assert.PanicsWithValue(t, "DATABASE_URL environment variable must be set", func() {
		Run(t, func(pool *pgxpool.Pool) {
			t.Error("Should not reach this point")
		})
	})
}

func TestRun_InvalidDatabaseURL(t *testing.T) {
	// Save original DATABASE_URL
	originalURL := os.Getenv("DATABASE_URL")
	defer func() {
		_ = os.Setenv("DATABASE_URL", originalURL)
	}()

	// Set invalid DATABASE_URL
	_ = os.Setenv("DATABASE_URL", "invalid-url")

	// This should panic with a message containing "failed to parse DATABASE_URL"
	assert.PanicsWithError(t, "failed to parse DATABASE_URL: cannot parse `invalid-url`: failed to parse as keyword/value (invalid keyword/value)", func() {
		Run(t, func(pool *pgxpool.Pool) {
			t.Error("Should not reach this point")
		})
	})
}

func TestRun_DatabaseURLChangeDetection(t *testing.T) {
	// First run to initialize master pool
	Run(t, func(pool *pgxpool.Pool) {
		// Just verify pool works
		require.NotNil(t, pool)
	})

	// Save original DATABASE_URL
	originalURL := os.Getenv("DATABASE_URL")
	defer func() {
		_ = os.Setenv("DATABASE_URL", originalURL)
	}()

	// Change DATABASE_URL to a different but valid URL
	modifiedURL := strings.Replace(originalURL, "postgres://", "postgresql://", 1)
	_ = os.Setenv("DATABASE_URL", modifiedURL)

	// This should panic because DATABASE_URL changed
	assert.Panicsf(t, func() {
		Run(t, func(pool *pgxpool.Pool) {
			t.Error("Should not reach this point")
		})
	}, "DATABASE_URL changed at runtime, previous: %s, current: %s", originalURL, modifiedURL)
}

func TestRun_TestIsolation(t *testing.T) {
	// First test creates a table
	t.Run("CreateTable", func(t *testing.T) {
		Run(t, func(pool *pgxpool.Pool) {
			ctx := t.Context()
			_, err := pool.Exec(ctx, "CREATE TABLE isolation_test (id SERIAL PRIMARY KEY, name TEXT)")
			require.NoError(t, err)

			_, err = pool.Exec(ctx, "INSERT INTO isolation_test (name) VALUES ('first_test')")
			require.NoError(t, err)
		})
	})

	// Second test should not see the table from the first test (tests cleanup between tests)
	t.Run("VerifyIsolation", func(t *testing.T) {
		Run(t, func(pool *pgxpool.Pool) {
			ctx := t.Context()

			// This table should not exist in the new database
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'isolation_test')").Scan(&exists)
			require.NoError(t, err)
			assert.False(t, exists, "Table from previous test should not exist (tests cleanup between sequential tests)")

			// Create the same table name - should succeed without conflicts
			_, err = pool.Exec(ctx, "CREATE TABLE isolation_test (id SERIAL PRIMARY KEY, name TEXT)")
			require.NoError(t, err)

			_, err = pool.Exec(ctx, "INSERT INTO isolation_test (name) VALUES ('second_test')")
			require.NoError(t, err)
		})
	})
}

func TestRun_ConcurrentTestIsolation(t *testing.T) {
	var wg sync.WaitGroup
	createTableReady := make(chan struct{})
	verifyIsolationDone := make(chan struct{})

	// First test creates a table and waits
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.Run("CreateTableConcurrent", func(t *testing.T) {
			Run(t, func(pool *pgxpool.Pool) {
				ctx := t.Context()
				_, err := pool.Exec(ctx, "CREATE TABLE concurrent_isolation_test (id SERIAL PRIMARY KEY, name TEXT)")
				require.NoError(t, err)

				_, err = pool.Exec(ctx, "INSERT INTO concurrent_isolation_test (name) VALUES ('concurrent_test')")
				require.NoError(t, err)

				// Signal that table is created and wait for verification to complete
				close(createTableReady)
				<-verifyIsolationDone
			})
		})
	}()

	// Second test waits for first test to create table, then verifies isolation
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.Run("VerifyConcurrentIsolation", func(t *testing.T) {
			// Wait for the first test to create its table
			<-createTableReady

			Run(t, func(pool *pgxpool.Pool) {
				ctx := t.Context()

				// This table should not exist in the new database (true isolation during concurrent execution)
				var exists bool
				err := pool.QueryRow(ctx,
					"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'concurrent_isolation_test')").Scan(&exists)
				require.NoError(t, err)
				assert.False(t, exists, "Table from concurrent test should not exist due to true database isolation")

				// Create the same table name - should succeed without conflicts
				_, err = pool.Exec(ctx, "CREATE TABLE concurrent_isolation_test (id SERIAL PRIMARY KEY, name TEXT)")
				require.NoError(t, err)

				_, err = pool.Exec(ctx, "INSERT INTO concurrent_isolation_test (name) VALUES ('isolated_test')")
				require.NoError(t, err)
			})

			// Signal that verification is done
			close(verifyIsolationDone)
		})
	}()

	wg.Wait()
}

func TestRun_MultipleConnectionsAcquire(t *testing.T) {
	Run(t, func(pool *pgxpool.Pool) {
		ctx := t.Context()

		const numConns = 3

		// Acquire multiple connections from the pool
		conns := make([]*pgxpool.Conn, numConns)
		for i := range len(conns) {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err, "Failed to acquire connection")
			defer conn.Release()
			conns[i] = conn
		}

		// Simple query to verify connection works
		for _, conn := range conns {
			var result int
			err := conn.QueryRow(ctx, "SELECT 1").Scan(&result)
			assert.NoError(t, err, "Failed to execute query on acquired connection")
			assert.Equal(t, 1, result, "Query result should be 1")
		}
	})
}

func TestRun_ConnectionPoolConcurrency(t *testing.T) {
	Run(t, func(pool *pgxpool.Pool) {
		ctx := t.Context()

		// Create a table for the test
		_, err := pool.Exec(ctx, "CREATE TABLE concurrency_test (id SERIAL PRIMARY KEY, worker_id INT)")
		require.NoError(t, err)

		const numWorkers = 10
		var wg sync.WaitGroup
		errors := make(chan error, numWorkers)

		// Spawn multiple goroutines that use the pool concurrently
		for workerID := range numWorkers {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Each worker inserts multiple records
				for range 5 {
					_, err := pool.Exec(ctx, "INSERT INTO concurrency_test (worker_id) VALUES ($1)", workerID)
					if err != nil {
						errors <- err
						return
					}
				}
			}()
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Errorf("Concurrent operation failed: %v", err)
		}

		// Verify all records were inserted
		var count int
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM concurrency_test").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, numWorkers*5, count)
	})
}

func TestRun_TransactionBehavior(t *testing.T) {
	Run(t, func(pool *pgxpool.Pool) {
		ctx := t.Context()

		// Create a test table
		_, err := pool.Exec(ctx, "CREATE TABLE transaction_test (id SERIAL PRIMARY KEY, value TEXT)")
		require.NoError(t, err)

		// Test transaction commit
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, "INSERT INTO transaction_test (value) VALUES ('committed')")
		require.NoError(t, err)

		err = tx.Commit(ctx)
		require.NoError(t, err)

		// Verify committed data exists
		var value string
		err = pool.QueryRow(ctx, "SELECT value FROM transaction_test WHERE value = 'committed'").Scan(&value)
		require.NoError(t, err)
		assert.Equal(t, "committed", value)

		// Test transaction rollback
		tx, err = pool.Begin(ctx)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, "INSERT INTO transaction_test (value) VALUES ('rolled_back')")
		require.NoError(t, err)

		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Verify rolled back data doesn't exist
		var count int
		err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM transaction_test WHERE value = 'rolled_back'").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestRun_SchemaAndTableCreation(t *testing.T) {
	Run(t, func(pool *pgxpool.Pool) {
		ctx := t.Context()

		// Test schema creation
		_, err := pool.Exec(ctx, "CREATE SCHEMA test_schema")
		require.NoError(t, err)

		// Test table creation in schema
		_, err = pool.Exec(ctx, `
			CREATE TABLE test_schema.test_table (
				id SERIAL PRIMARY KEY,
				name VARCHAR(100) NOT NULL,
				email VARCHAR(100) UNIQUE,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`)
		require.NoError(t, err)

		// Test index creation
		_, err = pool.Exec(ctx, "CREATE INDEX idx_test_email ON test_schema.test_table(email)")
		require.NoError(t, err)

		// Verify schema and table exist
		var schemaExists bool
		err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'test_schema')").Scan(&schemaExists)
		require.NoError(t, err)
		assert.True(t, schemaExists)

		var tableExists bool
		err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'test_schema' AND table_name = 'test_table')").Scan(&tableExists)
		require.NoError(t, err)
		assert.True(t, tableExists)
	})
}

func TestSetupTestDatabase(t *testing.T) {
	ctx := t.Context()
	url := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, url)

	masterConf, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	masterConf.MaxConns = 20

	masterPool, err := pgxpool.NewWithConfig(ctx, masterConf)
	require.NoError(t, err)
	defer masterPool.Close()

	testPath := "test/setup"
	dbName := internal.GenerateDBName(testPath)

	// Test setup
	err = setupTestDatabase(ctx, masterPool, dbName, testPath)
	require.NoError(t, err)

	// Verify database was created
	conn, err := masterPool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "Test database should exist")

	// Verify record was inserted in tracking table
	var recordExists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM _sqlpt.databases WHERE db_name = $1)", dbName).Scan(&recordExists)
	require.NoError(t, err)
	assert.True(t, recordExists, "Database record should exist in tracking table")

	// Cleanup
	err = cleanupTestDatabase(ctx, masterPool, dbName)
	require.NoError(t, err)
}

func TestCleanupTestDatabase(t *testing.T) {
	ctx := t.Context()
	url := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, url)

	masterConf, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	masterConf.MaxConns = 20

	masterPool, err := pgxpool.NewWithConfig(ctx, masterConf)
	require.NoError(t, err)
	defer masterPool.Close()

	testPath := "test/cleanup"
	dbName := internal.GenerateDBName(testPath)

	// Setup database first
	err = setupTestDatabase(ctx, masterPool, dbName, testPath)
	require.NoError(t, err)

	// Test cleanup
	err = cleanupTestDatabase(ctx, masterPool, dbName)
	require.NoError(t, err)

	// Verify database was dropped
	conn, err := masterPool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "Test database should not exist after cleanup")

	// Verify record was removed from tracking table
	var recordExists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM _sqlpt.databases WHERE db_name = $1)", dbName).Scan(&recordExists)
	require.NoError(t, err)
	assert.False(t, recordExists, "Database record should not exist in tracking table after cleanup")
}

func TestCleanupTestDatabase_NonexistentDatabase(t *testing.T) {
	ctx := t.Context()
	url := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, url)

	masterConf, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	masterConf.MaxConns = 20

	masterPool, err := pgxpool.NewWithConfig(ctx, masterConf)
	require.NoError(t, err)
	defer masterPool.Close()

	// Use a database name that doesn't exist
	dbName := internal.GenerateDBName("test/nonexistent")

	// cleanupTestDatabase should not error even if database doesn't exist
	err = cleanupTestDatabase(ctx, masterPool, dbName)
	assert.NoError(t, err, "cleanupTestDatabase should handle nonexistent databases gracefully")
}
