package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalk_VisitsFilesAndSurvivingDirs(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "src/main.go", "package main")
	writeFixture(t, root, "node_modules/left-pad/index.js", "module.exports = {}")

	var files []string
	var dirs []string
	err := Walk(root, func(rel string, isDir bool) error {
		if isDir {
			dirs = append(dirs, rel)
		} else {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}

	foundSrcFile := false
	for _, f := range files {
		if f == "src/main.go" {
			foundSrcFile = true
		}
		if f == "node_modules/left-pad/index.js" {
			t.Errorf("Walk() visited a file inside node_modules, which must be skipped: %v", files)
		}
	}
	if !foundSrcFile {
		t.Errorf("Walk() did not visit src/main.go, files=%v", files)
	}

	for _, d := range dirs {
		if d == "node_modules" {
			t.Errorf("Walk() visited node_modules directory itself (fine) but should not have descended into it: dirs=%v", dirs)
		}
	}
}

func TestWalk_ReturnsErrorForMissingRoot(t *testing.T) {
	if err := Walk(filepath.Join(t.TempDir(), "missing"), func(string, bool) error { return nil }); err == nil {
		t.Fatal("expected an error walking a nonexistent root")
	}
}

func TestWalk_HandlesEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	visited := 0
	err := Walk(root, func(rel string, isDir bool) error {
		visited++
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	if visited != 0 {
		t.Errorf("expected no entries in an empty directory, got %d", visited)
	}
}

func TestWalk_UnreadableSubdirDoesNotAbortWalk(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "a.txt", "a")
	writeFixture(t, root, "locked/inner.txt", "b")

	lockedDir := filepath.Join(root, "locked")
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

	var seen []string
	err := Walk(root, func(rel string, isDir bool) error {
		if !isDir {
			seen = append(seen, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error: %v (a single unreadable subdirectory must not abort the whole walk)", err)
	}

	foundA := false
	for _, f := range seen {
		if f == "a.txt" {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("expected a.txt to still be visited despite the unreadable sibling directory, got %v", seen)
	}
}
