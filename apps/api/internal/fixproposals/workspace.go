package fixproposals

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"e2e-sentinel/apps/api/internal/projects"
)

// skipDirs are directories excluded from the workspace copy — large,
// regenerable, or irrelevant to a source-level patch. Mirrors
// internal/discovery's skip-dir philosophy (spec §7.1 keeps discovery
// off dependency trees); the workspace copy is for testing a patch
// applies cleanly, not for reproducing a full build tree.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".next": true, "dist": true, "build": true,
}

// ApplyToWorkspace copies repositoryPath into a fresh directory under
// workspaceBaseDir and applies diff there — never touching the original
// repository (spec §15.2 "Apply in temporary workspace"; spec §3.3 "It
// must not apply patches" to the real repository until final approval).
// The workspace directory is left in place on both success and failure
// so the caller can inspect it; ony the caller decides when to remove it
// (mirroring Phase 5's Runner.Cleanup being a separate, explicit step).
func ApplyToWorkspace(repositoryPath, diff, workspaceBaseDir string) (workspaceDir string, results []FileResult, err error) {
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		return "", nil, err
	}

	if err := os.MkdirAll(workspaceBaseDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("fixproposals: creating workspace base directory: %w", err)
	}
	workspaceDir, err = os.MkdirTemp(workspaceBaseDir, "fix-")
	if err != nil {
		return "", nil, fmt.Errorf("fixproposals: creating workspace directory: %w", err)
	}

	if err := copyTree(repositoryPath, workspaceDir); err != nil {
		return workspaceDir, nil, fmt.Errorf("fixproposals: copying repository into workspace: %w", err)
	}

	results, err = applyChangesToDir(workspaceDir, changes)
	return workspaceDir, results, err
}

// ApplyToRepository applies diff directly to repositoryPath — the one
// place E2E Sentinel ever writes to a target repository. Callers must
// only reach this after explicit approval (spec §3.4, enforced by
// httpserver's handleApplyFixToRepository, not by this function).
func ApplyToRepository(repositoryPath, diff string) ([]FileResult, error) {
	changes, err := ParseUnifiedDiff(diff)
	if err != nil {
		return nil, err
	}
	return applyChangesToDir(repositoryPath, changes)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		target := filepath.Join(dst, rel)
		if !projects.WithinRoot(dst, target) {
			return fmt.Errorf("%w: %q", ErrPathEscapesRoot, rel)
		}

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			// Never follow symlinks into the copy — mirrors
			// discovery.Walk's refusal to follow a symlink whose target
			// falls outside the validated root.
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
