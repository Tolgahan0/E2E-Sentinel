package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigrationFiles_SortsAndFiltersNonSQL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "0002_second.sql", "-- second")
	writeFile(t, dir, "0001_first.sql", "-- first")
	writeFile(t, dir, "README.md", "not a migration")

	files, err := LoadMigrationFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2 (README.md must be excluded): %+v", len(files), files)
	}
	if files[0].Name != "0001_first.sql" || files[1].Name != "0002_second.sql" {
		t.Errorf("files not sorted: %+v", files)
	}
	if files[0].Version != "0001" {
		t.Errorf("Version = %q, want 0001", files[0].Version)
	}
}

func TestPendingMigrations_ExcludesApplied(t *testing.T) {
	files := []MigrationFile{
		{Name: "0001_init.sql"},
		{Name: "0002_add_column.sql"},
		{Name: "0003_add_index.sql"},
	}
	applied := map[string]bool{"0001_init.sql": true}

	pending := PendingMigrations(files, applied)
	if len(pending) != 2 {
		t.Fatalf("got %d pending, want 2: %+v", len(pending), pending)
	}
	if pending[0].Name != "0002_add_column.sql" || pending[1].Name != "0003_add_index.sql" {
		t.Errorf("unexpected pending order: %+v", pending)
	}
}

func TestPendingMigrations_AllAppliedMeansNonePending(t *testing.T) {
	files := []MigrationFile{{Name: "0001_init.sql"}}
	applied := map[string]bool{"0001_init.sql": true}

	pending := PendingMigrations(files, applied)
	if len(pending) != 0 {
		t.Fatalf("expected no pending migrations, got %+v", pending)
	}
}

func TestSplitStatements_IgnoresSemicolonsInsideStrings(t *testing.T) {
	sql := `
CREATE TABLE t (id INT);
INSERT INTO t (label) VALUES ('a;b;c');
CREATE INDEX idx ON t (id);
`
	statements := splitStatements(sql)
	if len(statements) != 3 {
		t.Fatalf("got %d statements, want 3: %#v", len(statements), statements)
	}
	if !strings.Contains(statements[1], "'a;b;c'") {
		t.Errorf("statement with quoted semicolons was split incorrectly: %q", statements[1])
	}
}

func TestSplitStatements_IgnoresSemicolonsInsideLineComments(t *testing.T) {
	// Regression test: a semicolon inside a "--" comment (e.g. an English
	// sentence in a migration docstring) must not be treated as a
	// statement terminator.
	sql := `
-- before this file runs; it is not created here.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE t (id INT);
`
	statements := splitStatements(sql)
	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2: %#v", len(statements), statements)
	}
	if !strings.Contains(statements[0], "CREATE EXTENSION") {
		t.Errorf("statement[0] should contain the CREATE EXTENSION statement (comment should not split it): %q", statements[0])
	}
}

func TestSplitStatements_IgnoresSemicolonsInsideBlockComments(t *testing.T) {
	sql := `
/* multi-line; comment; with semicolons */
CREATE TABLE t (id INT);
`
	statements := splitStatements(sql)
	if len(statements) != 1 {
		t.Fatalf("got %d statements, want 1: %#v", len(statements), statements)
	}
}

func TestSplitStatements_WhitespaceOnlyYieldsNoStatements(t *testing.T) {
	if got := splitStatements("   \n\t\n"); len(got) != 0 {
		t.Fatalf("expected 0 statements for whitespace-only input, got %#v", got)
	}
}

func writeFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing fixture file %q: %v", name, err)
	}
}
