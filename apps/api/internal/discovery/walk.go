package discovery

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"e2e-sentinel/apps/api/internal/projects"
)

// Limits bound how much of a repository a single scan will walk, so a
// pathological or enormous tree can't turn discovery into a
// denial-of-service against E2E Sentinel itself.
const (
	maxDepth       = 10
	maxEntriesSeen = 50_000
)

// skipDirs are never descended into: dependency/build output directories
// carry no discovery signal and are typically enormous.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "target": true, ".venv": true,
	"venv": true, "__pycache__": true, ".turbo": true, "coverage": true,
	".cache": true, "bin": true, "obj": true, ".idea": true, ".vscode": true,
}

// Walk applies the same traversal safety rules Scan uses — symlink
// escape prevention, skip-dir list, depth and entry-count limits — to an
// arbitrary visit function, so other packages (e.g. internal/routes)
// don't need to re-implement repository-walking safety. root must
// already be validated via projects.ValidateRepositoryPath.
//
// visit is called for every surviving entry (both files and directories
// that weren't skipped) with its path relative to root, using forward
// slashes.
func Walk(root string, visit func(rel string, isDir bool) error) error {
	seen := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// The root itself being inaccessible (deleted, permission
			// changed, etc. since it was validated) is a real failure —
			// silently reporting zero findings would be misleading.
			// A nested entry being inaccessible is tolerated: skip it
			// and keep walking the rest of the tree.
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		seen++
		if seen > maxEntriesSeen {
			return fs.SkipAll
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.Type()&fs.ModeSymlink != 0 {
			resolved, symErr := filepath.EvalSymlinks(path)
			if symErr != nil || !projects.WithinRoot(root, resolved) {
				return nil
			}
		}

		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" && d.Name() != ".maestro" && d.Name() != ".detox") {
				return fs.SkipDir
			}
			if depth(rel) > maxDepth {
				return fs.SkipDir
			}
			if rel == "." {
				return nil
			}
			return visit(rel, true)
		}

		return visit(rel, false)
	})
	if err != nil {
		return fmt.Errorf("discovery: walking %q: %w", root, err)
	}
	return nil
}

func depth(rel string) int {
	if rel == "." {
		return 0
	}
	return strings.Count(rel, "/") + 1
}
