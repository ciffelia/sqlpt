package sqlpt

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ciffelia/sqlpt/internal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestFunc represents a test function that receives a database pool
type TestFunc func(conn *pgxpool.Pool)

var (
	masterPool     *pgxpool.Pool
	masterPoolOnce sync.Once
	masterPoolErr  error
)

// Run creates an isolated test database and runs the test function
func Run(t *testing.T, testFunc TestFunc) {
	ctx := t.Context()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		panic("DATABASE_URL environment variable must be set")
	}

	masterConf, err := pgxpool.ParseConfig(url)
	if err != nil {
		panic(fmt.Errorf("failed to parse DATABASE_URL: %w", err))
	}

	// Postgres' normal connection limit is 100 plus 3 superuser connections
	// We don't want to use the whole cap and there may be fuzziness here due to
	// concurrently running tests anyway.
	masterConf.MaxConns = 20
	// Immediately close master connections.
	masterConf.AfterRelease = func(conn *pgx.Conn) bool {
		return false
	}

	// Initialize master pool
	previouslyInitialized := true
	masterPoolOnce.Do(func() {
		masterPool, masterPoolErr = pgxpool.NewWithConfig(ctx, masterConf)
		previouslyInitialized = false
	})
	if masterPoolErr != nil {
		panic(fmt.Errorf("failed to initialize master pool: %w", masterPoolErr))
	}
	if previouslyInitialized {
		if masterPool.Config().ConnString() != masterConf.ConnString() {
			panic(fmt.Sprintf("DATABASE_URL changed at runtime, previous: %s, current: %s",
				masterPool.Config().ConnString(), masterConf.ConnString()))
		}
	}

	// Generate unique database name based on test path
	testPath := internal.GetTestPath(t)
	dbName := internal.GenerateDBName(testPath)
	t.Logf("Using test database: %s", dbName)

	// Setup test database
	if err := setupTestDatabase(ctx, masterPool, dbName, testPath); err != nil {
		panic(fmt.Errorf("failed to setup test database: %w", err))
	}

	// Create connection config for test database
	testConfig := masterConf.Copy()
	testConfig.ConnConfig.Database = dbName
	// Don't allow a single test to take all the connections.
	// Most tests shouldn't require more than 5 connections concurrently,
	// or else they're likely doing too much in one test.
	testConfig.MaxConns = 5
	// Close connections ASAP if left in the idle queue.
	testConfig.MaxConnIdleTime = 1 * time.Second

	// Connect to test database
	pool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		panic(fmt.Errorf("failed to connect to test database: %w", err))
	}
	defer func() {
		done := make(chan struct{})
		go func() {
			pool.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Logf("warning: test %s held onto pool after exiting", t.Name())
		case <-ctx.Done():
		}
	}()

	// Run the test
	testFunc(pool)

	// Cleanup test database
	if err := cleanupTestDatabase(ctx, masterPool, dbName); err != nil {
		t.Logf("warning: failed to cleanup test database %s: %v", dbName, err)
	}
}

// setupTestDatabase creates a new test database
func setupTestDatabase(ctx context.Context, pool *pgxpool.Pool, dbName, testPath string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// create the _sqlpt schema and tables
	query := `
    select pg_advisory_xact_lock(32494332878806879);

    create schema if not exists _sqlpt;

    create table if not exists _sqlpt.databases (
        db_name text primary key,
        test_path text not null,
        created_at timestamptz not null default now()
    );

    create index if not exists databases_created_at 
        on _sqlpt.databases(created_at);

    create sequence if not exists _sqlpt.database_ids;
	`
	_, err = conn.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to initialize test schema: %w", err)
	}

	// Clean up any existing database with the same name
	if err := doCleanup(ctx, conn.Conn(), dbName); err != nil {
		return fmt.Errorf("failed to cleanup existing database: %w", err)
	}

	// Insert database record
	_, err = conn.Exec(ctx,
		"insert into _sqlpt.databases(db_name, test_path) values ($1, $2)",
		dbName, testPath)
	if err != nil {
		return fmt.Errorf("failed to insert database record: %w", err)
	}

	// Create the test database
	createQuery := fmt.Sprintf("create database %s", pgx.Identifier{dbName}.Sanitize())
	_, err = conn.Exec(ctx, createQuery)
	if err != nil {
		return fmt.Errorf("failed to create test database: %w", err)
	}

	return nil
}

// cleanupTestDatabase removes a test database
func cleanupTestDatabase(ctx context.Context, pool *pgxpool.Pool, dbName string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	return doCleanup(ctx, conn.Conn(), dbName)
}

// doCleanup performs the actual database cleanup
func doCleanup(ctx context.Context, conn *pgx.Conn, dbName string) error {
	// Drop the database if it exists
	dropQuery := fmt.Sprintf("drop database if exists %s", pgx.Identifier{dbName}.Sanitize())
	_, err := conn.Exec(ctx, dropQuery)
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Remove from tracking table
	_, err = conn.Exec(ctx, "delete from _sqlpt.databases where db_name = $1", dbName)
	if err != nil {
		return fmt.Errorf("failed to remove database record: %w", err)
	}

	return nil
}
