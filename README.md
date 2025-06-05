# sqlpt

[![CI status](https://github.com/ciffelia/sqlpt/actions/workflows/ci.yaml/badge.svg)](https://github.com/ciffelia/sqlpt/actions/workflows/ci.yaml)

**SQL Parallel Testing** - A Go library for running database-dependent tests in parallel with full isolation.

## Overview

`sqlpt` enables true parallel testing for PostgreSQL applications by creating isolated test databases for each test. Each test runs in its own dedicated database, eliminating the need for complex transaction rollbacks or data cleanup strategies.

## Features

- **True Test Isolation**: Each test gets its own PostgreSQL database
- **Parallel Execution**: Tests can run concurrently without interference
- **Automatic Cleanup**: Databases are created and destroyed automatically
- **Easy Debugging**: Failed tests leave their database intact for inspection
- **pgx/v5 Integration**: Built specifically for the pgx PostgreSQL driver
- **Simple API**: Just wrap your test logic with `sqlpt.Run()`

## Installation

```bash
go get github.com/ciffelia/sqlpt
```

## Usage

Set the `DATABASE_URL` environment variable to your PostgreSQL connection string:

```bash
export DATABASE_URL="postgres://user:password@localhost/dbname"
```

Then use `sqlpt.Run()` in your tests:

```go
package main

import (
    "testing"
    sqlpt "github.com/ciffelia/sqlpt/pgx/v5"
)

func TestUserCreation(t *testing.T) {
    ctx := t.Context()
    
    sqlpt.Run(t, func(pool *pgxpool.Pool) {
        // Create test schema
        _, err := pool.Exec(ctx, `
            CREATE TABLE users (
                id SERIAL PRIMARY KEY,
                name VARCHAR(100) NOT NULL,
                email VARCHAR(100) UNIQUE NOT NULL
            )
        `)
        if err != nil {
            t.Fatalf("failed to create table: %v", err)
        }
        
        // Insert test data
        _, err = pool.Exec(ctx,
            "INSERT INTO users (name, email) VALUES ($1, $2)",
            "John Doe", "john@example.com")
        if err != nil {
            t.Fatalf("failed to insert user: %v", err)
        }
        
        // Verify data
        var name string
        err = pool.QueryRow(ctx,
            "SELECT name FROM users WHERE email = $1",
            "john@example.com").Scan(&name)
        if err != nil {
            t.Fatalf("failed to query user: %v", err)
        }
        
        if name != "John Doe" {
            t.Errorf("expected 'John Doe', got '%s'", name)
        }
    })
}
```

## How It Works

`sqlpt` creates a unique database for each test using `CREATE DATABASE`, runs your test with a dedicated connection pool, then automatically cleans up the database when the test completes successfully.

## Comparison with go-txdb

[go-txdb](https://github.com/DATA-DOG/go-txdb) is another popular library for database testing in Go, but uses a different approach:

### sqlpt advantages:

- **Multiple connections**: Tests can use connection pools and concurrent database operations, while go-txdb is limited to a single connection per test
- **Real transaction behavior**: Tests actual commit/rollback semantics, deadlock handling, and transaction isolation levels that go-txdb cannot test since everything runs in one transaction
- **Production-like environment**: Tests run against actual separate databases, catching issues that only appear with real database operations
- **Better debugging**: When tests fail, the database remains intact for inspection, while go-txdb rolls back all data
- **No test interference**: Each test runs in a completely separate database, eliminating any possibility of resource contention or locking between concurrent tests

### go-txdb advantages:

- **Faster execution**: No database creation overhead, just transaction rollback
- **Lower resource usage**: Uses less memory and storage since no new databases are created
- **Simpler setup**: Works with any existing database without special permissions

### When to choose sqlpt:

- Your application uses connection pools or multiple concurrent connections
- You need to test transaction boundaries, deadlocks, or isolation levels  
- You want to test against the exact same database setup as production
- Test execution time is less critical than test accuracy

## Acknowledgements

This library is inspired by Rust's [`sqlx::test`](https://docs.rs/sqlx/latest/sqlx/attr.test.html) macro, with much of the implementation adapted from the sqlx crate's approach to parallel testing.
