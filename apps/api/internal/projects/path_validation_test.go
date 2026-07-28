package projects

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateRepositoryPath_AcceptsRealDirectory(t *testing.T) {
	dir := t.TempDir()
	resolved, err := ValidateRepositoryPath(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantResolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	if resolved != filepath.Clean(wantResolved) {
		t.Errorf("resolved = %q, want %q", resolved, wantResolved)
	}
}

func TestValidateRepositoryPath_RejectsEmpty(t *testing.T) {
	if _, err := ValidateRepositoryPath(""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestValidateRepositoryPath_RejectsNonexistentPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	if _, err := ValidateRepositoryPath(missing); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestValidateRepositoryPath_RejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing fixture file: %v", err)
	}
	if _, err := ValidateRepositoryPath(file); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestValidateRepositoryPath_RejectsSystemRoots(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("dangerous-root list is POSIX-path specific")
	}
	for _, root := range []string{"/", "/etc"} {
		if _, err := ValidateRepositoryPath(root); !errors.Is(err, ErrDangerousRoot) {
			t.Errorf("ValidateRepositoryPath(%q) err = %v, want ErrDangerousRoot", root, err)
		}
	}
}

func TestValidateRepositoryPath_ResolvesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks behave differently on windows")
	}
	outside := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	resolved, err := ValidateRepositoryPath(link)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", outside, err)
	}
	// The important property: the caller now has the REAL resolved path
	// (outside's real location), not the symlink path, so any later
	// containment check compares against reality, not the alias.
	if resolved != filepath.Clean(wantOutside) {
		t.Errorf("resolved = %q, want the symlink target's real path %q", resolved, wantOutside)
	}
}

func TestWithinRoot(t *testing.T) {
	root := "/home/user/project"
	cases := []struct {
		candidate string
		want      bool
	}{
		{"/home/user/project", true},
		{"/home/user/project/src/main.go", true},
		{"/home/user/project/../other", false},
		{"/home/user", false},
		{"/etc/passwd", false},
	}
	for _, tc := range cases {
		if got := WithinRoot(root, tc.candidate); got != tc.want {
			t.Errorf("WithinRoot(%q, %q) = %v, want %v", root, tc.candidate, got, tc.want)
		}
	}
}
