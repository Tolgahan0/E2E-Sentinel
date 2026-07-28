// Package db provides PostgreSQL and Redis connectivity plus a minimal,
// dependency-light SQL migration runner. See ADR 0001 for why a custom
// runner was chosen over an external migration tool for Phase 0.
package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationFile is a single versioned SQL migration on disk.
type MigrationFile struct {
	// Version is the filename's sortable prefix, e.g. "0001".
	Version string
	// Name is the full filename, e.g. "0001_init.sql".
	Name string
	// Path is the absolute or relative path to the file.
	Path string
}

// LoadMigrationFiles reads all "*.sql" files from dir and returns them
// sorted lexicographically by filename. It does not read file contents.
func LoadMigrationFiles(dir string) ([]MigrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("db: reading migrations dir %q: %w", dir, err)
	}

	var files []MigrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := strings.SplitN(entry.Name(), "_", 2)[0]
		files = append(files, MigrationFile{
			Version: version,
			Name:    entry.Name(),
			Path:    filepath.Join(dir, entry.Name()),
		})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// splitStatements splits a SQL file's contents into individual statements
// on top-level semicolons — i.e. semicolons outside single-quoted string
// literals and outside "--" line comments or "/* */" block comments. This
// is a deliberately narrow tool: it does not support dollar-quoted
// function bodies. Phase 0 migrations are plain DDL, so this is
// sufficient; a real migration tool should replace it before any
// migration needs a stored procedure.
func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	inString := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		current.WriteByte(ch)

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			if ch == '/' && i > 0 && sql[i-1] == '*' {
				inBlockComment = false
			}
		case inString:
			if ch == '\'' {
				inString = false
			}
		case ch == '\'':
			inString = true
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			inLineComment = true
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			inBlockComment = true
		case ch == ';':
			stmt := strings.TrimSpace(current.String())
			if stmt != "" && stmt != ";" {
				statements = append(statements, stmt)
			}
			current.Reset()
		}
	}

	if rest := strings.TrimSpace(current.String()); rest != "" {
		statements = append(statements, rest)
	}

	return statements
}

// PendingMigrations returns the subset of files whose Name is not present
// in applied, preserving order. It is a pure function so the ordering and
// filtering logic can be unit tested without a database.
func PendingMigrations(files []MigrationFile, applied map[string]bool) []MigrationFile {
	var pending []MigrationFile
	for _, f := range files {
		if !applied[f.Name] {
			pending = append(pending, f)
		}
	}
	return pending
}

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name        TEXT PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

// Migrate applies all pending .sql files in dir against pool, each inside
// its own transaction, recording success in schema_migrations. It is
// idempotent: re-running it after a full success applies nothing further.
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) ([]string, error) {
	if _, err := pool.Exec(ctx, createMigrationsTableSQL); err != nil {
		return nil, fmt.Errorf("db: ensuring schema_migrations table: %w", err)
	}

	rows, err := pool.Query(ctx, "SELECT name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: listing applied migrations: %w", err)
	}
	applied := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("db: scanning applied migration: %w", err)
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: reading applied migrations: %w", err)
	}

	files, err := LoadMigrationFiles(dir)
	if err != nil {
		return nil, err
	}

	pending := PendingMigrations(files, applied)
	var appliedNow []string

	for _, f := range pending {
		contents, err := os.ReadFile(f.Path)
		if err != nil {
			return appliedNow, fmt.Errorf("db: reading migration %q: %w", f.Name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return appliedNow, fmt.Errorf("db: beginning transaction for %q: %w", f.Name, err)
		}

		for _, stmt := range splitStatements(string(contents)) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				_ = tx.Rollback(ctx)
				return appliedNow, fmt.Errorf("db: applying migration %q: %w", f.Name, err)
			}
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", f.Name); err != nil {
			_ = tx.Rollback(ctx)
			return appliedNow, fmt.Errorf("db: recording migration %q: %w", f.Name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return appliedNow, fmt.Errorf("db: committing migration %q: %w", f.Name, err)
		}

		appliedNow = append(appliedNow, f.Name)
	}

	return appliedNow, nil
}
