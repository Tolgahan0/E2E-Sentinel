package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %q: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %q: %v", relPath, err)
	}
}

func findByKey(findings []Finding, category, name string) (Finding, bool) {
	for _, f := range findings {
		if f.Category == category && f.Name == name {
			return f, true
		}
	}
	return Finding{}, false
}

// TestScan_NextGoDockerPlaywright matches the Phase 1 acceptance
// criteria verbatim: "A Next.js + Go repository is correctly detected",
// "Docker files are listed", "existing Playwright tests are discovered".
func TestScan_NextGoDockerPlaywright(t *testing.T) {
	root := t.TempDir()

	writeFixture(t, root, "apps/web/package.json", `{
		"dependencies": {"next": "14.2.0", "react": "18.3.0"},
		"devDependencies": {"@playwright/test": "1.45.0"}
	}`)
	writeFixture(t, root, "apps/web/next.config.js", "module.exports = {}")
	writeFixture(t, root, "apps/web/playwright.config.ts", "export default {}")
	writeFixture(t, root, "apps/api/go.mod", "module example.com/api\n\nrequire github.com/go-chi/chi/v5 v5.0.0\n")
	writeFixture(t, root, "Dockerfile", "FROM golang:1.25")
	writeFixture(t, root, "docker-compose.yml", "services: {}")
	writeFixture(t, root, ".github/workflows/ci.yml", "name: CI")
	// Noise that must be skipped, not scanned.
	writeFixture(t, root, "apps/web/node_modules/some-lib/package.json", `{"dependencies":{"left-pad":"1.0.0"}}`)

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	mustFind := []struct{ category, name string }{
		{CategoryLanguage, "node"},
		{CategoryLanguage, "go"},
		{CategoryFramework, "next"},
		{CategoryFramework, "react"},
		{CategoryFramework, "chi"},
		{CategoryDocker, "dockerfile"},
		{CategoryDocker, "docker_compose"},
		{CategoryTestTool, "playwright"},
		{CategoryCI, "github_actions"},
	}
	for _, want := range mustFind {
		if _, ok := findByKey(findings, want.category, want.name); !ok {
			t.Errorf("expected finding %s/%s, got: %+v", want.category, want.name, findings)
		}
	}

	playwright, _ := findByKey(findings, CategoryTestTool, "playwright")
	if playwright.Confidence != ConfidenceHigh {
		t.Errorf("playwright confidence = %q, want high", playwright.Confidence)
	}

	// node_modules must never be scanned.
	if _, ok := findByKey(findings, CategoryFramework, "left-pad"); ok {
		t.Error("node_modules dependency leaked into findings; skipDirs is not working")
	}
}

func TestScan_DeduplicatesAcrossMultiplePaths(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "web1/package.json", `{"dependencies":{"react":"18.0.0"}}`)
	writeFixture(t, root, "web2/package.json", `{"dependencies":{"react":"18.0.0"}}`)

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	react, ok := findByKey(findings, CategoryFramework, "react")
	if !ok {
		t.Fatal("expected a single deduplicated react finding")
	}
	paths, _ := react.Evidence["paths"].([]string)
	if len(paths) != 2 {
		t.Errorf("expected evidence to list both paths, got %v", paths)
	}
}

func TestScan_SymlinkEscapeIsNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks behave differently on windows")
	}
	outside := t.TempDir()
	writeFixture(t, outside, "secret/package.json", `{"dependencies":{"express":"4.0.0"}}`)

	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "escape")); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if _, ok := findByKey(findings, CategoryFramework, "express"); ok {
		t.Errorf("scan followed a symlink outside the project root: %+v", findings)
	}
}

func TestScan_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "package.json", `{"dependencies":{"next":"14.0.0"}}`)
	writeFixture(t, root, "go.mod", "module x\n")

	first, err := Scan(root)
	if err != nil {
		t.Fatalf("first Scan() error: %v", err)
	}
	second, err := Scan(root)
	if err != nil {
		t.Fatalf("second Scan() error: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("scan is not idempotent: got %d findings then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Category != second[i].Category || first[i].Name != second[i].Name {
			t.Errorf("finding %d differs between runs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestScan_MigrationsDirectoryDetected(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "migrations/0001_init.sql", "CREATE TABLE x();")

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if _, ok := findByKey(findings, CategoryDatabase, "sql_migrations"); !ok {
		t.Errorf("expected sql_migrations finding, got %+v", findings)
	}
}
