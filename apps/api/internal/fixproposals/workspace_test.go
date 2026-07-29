package fixproposals

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

func TestApplyToWorkspace_CopiesRepoAndAppliesPatchWithoutTouchingOriginal(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc old() {}\n")

	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-func old() {}\n+func new() {}\n"
	workspaceBase := t.TempDir()

	workspaceDir, results, err := ApplyToWorkspace(repo, diff, workspaceBase)
	if err != nil {
		t.Fatalf("ApplyToWorkspace() error: %v", err)
	}
	if len(results) != 1 || !results[0].Applied {
		t.Fatalf("results = %+v, want one applied result", results)
	}

	patched, err := os.ReadFile(filepath.Join(workspaceDir, "main.go"))
	if err != nil {
		t.Fatalf("reading patched workspace file: %v", err)
	}
	if string(patched) != "package main\n\nfunc new() {}\n" {
		t.Errorf("patched content = %q", patched)
	}

	original, err := os.ReadFile(filepath.Join(repo, "main.go"))
	if err != nil {
		t.Fatalf("reading original repo file: %v", err)
	}
	if string(original) != "package main\n\nfunc old() {}\n" {
		t.Errorf("original repository file was modified: %q", original)
	}
}

func TestApplyToWorkspace_SkipsGitAndNodeModules(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteFile(t, filepath.Join(repo, "node_modules", "pkg", "index.js"), "module.exports = {}\n")
	mustWriteFile(t, filepath.Join(repo, "main.go"), "package main\n")

	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,1 @@\n-package main\n+package main // patched\n"
	workspaceDir, _, err := ApplyToWorkspace(repo, diff, t.TempDir())
	if err != nil {
		t.Fatalf("ApplyToWorkspace() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspaceDir, ".git")); !os.IsNotExist(err) {
		t.Error(".git should not be copied into the workspace")
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "node_modules")); !os.IsNotExist(err) {
		t.Error("node_modules should not be copied into the workspace")
	}
}

func TestApplyToWorkspace_RejectsPathEscapingDiff(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "main.go"), "package main\n")

	diff := "--- a/../../../../etc/passwd\n+++ b/../../../../etc/passwd\n@@ -1,1 +1,1 @@\n-root:x:0:0\n+pwned:x:0:0\n"
	_, results, err := ApplyToWorkspace(repo, diff, t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a path-escaping diff")
	}
	if len(results) != 1 || results[0].Applied {
		t.Fatalf("results = %+v, want a single unapplied result", results)
	}
}

func TestApplyToWorkspace_MalformedDiffFailsBeforeCopying(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "main.go"), "package main\n")

	_, _, err := ApplyToWorkspace(repo, "not a diff at all", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a malformed diff")
	}
}

func TestApplyToRepository_WritesDirectlyToTheGivenPath(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc old() {}\n")

	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-func old() {}\n+func new() {}\n"
	results, err := ApplyToRepository(repo, diff)
	if err != nil {
		t.Fatalf("ApplyToRepository() error: %v", err)
	}
	if len(results) != 1 || !results[0].Applied {
		t.Fatalf("results = %+v", results)
	}

	got, err := os.ReadFile(filepath.Join(repo, "main.go"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(got) != "package main\n\nfunc new() {}\n" {
		t.Errorf("content = %q", got)
	}
}

func TestApplyToRepository_RejectsPathEscapingDiff(t *testing.T) {
	repo := t.TempDir()
	diff := "--- a/../outside.txt\n+++ b/../outside.txt\n@@ -0,0 +1,1 @@\n+pwned\n"
	_, err := ApplyToRepository(repo, diff)
	if err == nil {
		t.Fatal("expected an error for a path-escaping diff")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(repo), "outside.txt")); !os.IsNotExist(statErr) {
		t.Fatal("a file was written outside the repository root")
	}
}

func TestApplyToRepository_CreatesAndDeletesFiles(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "old.txt"), "line one\nline two\n")

	diff := "--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1,1 @@\n+hello\n" +
		"--- a/old.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-line one\n-line two\n"
	results, err := ApplyToRepository(repo, diff)
	if err != nil {
		t.Fatalf("ApplyToRepository() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if _, err := os.Stat(filepath.Join(repo, "new.txt")); err != nil {
		t.Errorf("new.txt was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt was not deleted")
	}
}
