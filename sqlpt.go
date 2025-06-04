package sqlpt

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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

// WithTestDB creates an isolated test database and runs the test function
func WithTestDB(t *testing.T, testFunc TestFunc) {
	ctx := t.Context()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatalf("DATABASE_URL environment variable must be set")
	}

	masterConf, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("failed to parse DATABASE_URL: %v", err)
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
		t.Fatalf("failed to initialize master pool: %v", masterPoolErr)
	}
	if previouslyInitialized {
		if masterPool.Config().ConnString() != masterConf.ConnString() {
			t.Fatalf("DATABASE_URL changed at runtime, previous: %s, current: %s",
				masterPool.Config().ConnString(), masterConf.ConnString())
		}
	}

	initializeTestSchema(ctx, masterPool)

	// Generate unique database name based on test path
	testPath := fmt.Sprintf("%s/%s", t.Name(), getTestPath())
	dbName := generateDBName(testPath)

	// Setup test database
	if err := setupTestDatabase(ctx, masterPool, dbName, testPath); err != nil {
		t.Fatalf("failed to setup test database: %v", err)
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
		cleanupTestDatabase(ctx, masterPool, dbName)
		t.Fatalf("failed to connect to test database: %v", err)
	}
	defer pool.Close()

	// Run the test
	func() {
		defer func() {
			if r := recover(); r != nil {
				cleanupTestDatabase(ctx, masterPool, dbName)
				panic(r)
			}
		}()

		testFunc(pool)
	}()

	// Cleanup test database
	if err := cleanupTestDatabase(ctx, masterPool, dbName); err != nil {
		t.Logf("warning: failed to cleanup test database %s: %v", dbName, err)
	}
}

// initializeTestSchema creates the _sqlx_test schema and tables
func initializeTestSchema(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Create schema and tables with advisory lock (similar to sqlx implementation)
	query := `
		SELECT pg_advisory_xact_lock(8318549251334697844);
		
		CREATE SCHEMA IF NOT EXISTS _sqlx_test;
		
		CREATE TABLE IF NOT EXISTS _sqlx_test.databases (
			db_name text PRIMARY KEY,
			test_path text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		);
		
		CREATE INDEX IF NOT EXISTS databases_created_at 
			ON _sqlx_test.databases(created_at);
		
		CREATE SEQUENCE IF NOT EXISTS _sqlx_test.database_ids;
	`

	_, err = conn.Exec(ctx, query)
	return err
}

// generateDBName generates a unique database name based on test path (mimics sqlx logic)
func generateDBName(testPath string) string {
	hasher := sha512.New()
	hasher.Write([]byte(testPath))
	hash := hasher.Sum(nil)

	// Use first 39 bytes to match sqlx behavior
	encoded := base64.URLEncoding.EncodeToString(hash[:39])
	dbName := fmt.Sprintf("_sqlx_test_%s", encoded)

	// Replace dashes with underscores for PostgreSQL compatibility
	dbName = strings.ReplaceAll(dbName, "-", "_")

	// Ensure length is 63 characters (PostgreSQL identifier limit)
	if len(dbName) > 63 {
		dbName = dbName[:63]
	}

	return dbName
}

// setupTestDatabase creates a new test database
func setupTestDatabase(ctx context.Context, pool *pgxpool.Pool, dbName, testPath string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Clean up any existing database with the same name
	if err := doCleanup(ctx, conn.Conn(), dbName); err != nil {
		return fmt.Errorf("failed to cleanup existing database: %w", err)
	}

	// Insert database record
	_, err = conn.Exec(ctx,
		"INSERT INTO _sqlx_test.databases(db_name, test_path) VALUES ($1, $2)",
		dbName, testPath)
	if err != nil {
		return fmt.Errorf("failed to insert database record: %w", err)
	}

	// Create the test database
	createQuery := fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{dbName}.Sanitize())
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
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{dbName}.Sanitize())
	_, err := conn.Exec(ctx, dropQuery)
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Remove from tracking table
	_, err = conn.Exec(ctx,
		"DELETE FROM _sqlx_test.databases WHERE db_name = $1",
		dbName)
	if err != nil {
		return fmt.Errorf("failed to remove database record: %w", err)
	}

	return nil
}

// getTestPath returns a test path identifier (simplified version)
func getTestPath() string {
	// This is a simplified implementation. In a real scenario,
	// you might want to extract more detailed path information
	return "test"
}

// CleanupTestDatabases removes old test databases (utility function)
func CleanupTestDatabases(ctx context.Context) (int, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return 0, fmt.Errorf("DATABASE_URL environment variable must be set")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return 0, fmt.Errorf("failed to parse DATABASE_URL: %w", err)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)

	// Get list of test databases
	rows, err := conn.Query(ctx, "SELECT db_name FROM _sqlx_test.databases")
	if err != nil {
		return 0, fmt.Errorf("failed to query test databases: %w", err)
	}
	defer rows.Close()

	var dbNames []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return 0, fmt.Errorf("failed to scan database name: %w", err)
		}
		dbNames = append(dbNames, dbName)
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating rows: %w", err)
	}

	// Delete databases
	deletedCount := 0
	for _, dbName := range dbNames {
		if err := doCleanup(ctx, conn, dbName); err == nil {
			deletedCount++
		}
	}

	return deletedCount, nil
}
