// Package database opens a pooled PostgreSQL connection and applies a service's embedded migrations.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq" // postgres driver
)

// Open connects to PostgreSQL, retrying for up to 30s so services survive starting before the DB is ready.
func Open(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	deadline := time.Now().Add(30 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return db, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			db.Close()
			return nil, fmt.Errorf("database not reachable: %w", err)
		}
		time.Sleep(time.Second)
	}
}

// Migrate applies every "*.up.sql" file in dir that has not been applied yet, in filename order,
// each in its own transaction. Services share one database, so each tracks its own history in
// migrationsTable (e.g. "account_schema_migrations"). An advisory lock serialises concurrent
// replicas starting at the same time.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, dir, migrationsTable string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	h := fnv.New64a()
	h.Write([]byte(migrationsTable))
	lockID := int64(h.Sum64() >> 1)
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID) //nolint:errcheck // released on disconnect anyway

	// migrationsTable is a constant chosen by each service, never user input.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		migrationsTable)); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	for _, name := range files {
		version := strings.TrimSuffix(name, ".up.sql")
		var exists bool
		if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE version = $1)`, migrationsTable),
			version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback() //nolint:errcheck // returning the exec error
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (version) VALUES ($1)`, migrationsTable), version); err != nil {
			tx.Rollback() //nolint:errcheck // returning the insert error
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
