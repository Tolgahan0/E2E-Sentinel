package fixproposals

import (
	"fmt"
	"os"
	"path/filepath"

	"e2e-sentinel/apps/api/internal/projects"
)

// FileResult is the outcome of applying one FileChange to a target
// directory.
type FileResult struct {
	Path    string
	Action  string // "created", "modified", "deleted"
	Applied bool
	Error   string
}

// ErrPathEscapesRoot is returned when a diff's file path — attacker-
// controlled if the diff came from an AI provider or a compromised
// project — would resolve outside the target directory. Checked before
// any write, exactly like projects.ValidateRepositoryPath's containment
// check for discovery.
var ErrPathEscapesRoot = fmt.Errorf("fixproposals: diff file path escapes the target directory")

// applyChangesToDir applies every parsed FileChange to files under root
// (a workspace copy or, for the final approved write, the real
// repository). It is all-or-nothing at the call-site's discretion: every
// change is attempted and reported individually in the returned
// results, but the caller decides whether any failure should be
// treated as an overall failure (both current callers do).
func applyChangesToDir(root string, changes []FileChange) ([]FileResult, error) {
	root = filepath.Clean(root)
	results := make([]FileResult, 0, len(changes))
	var firstErr error

	for _, fc := range changes {
		res := FileResult{Path: fc.Path()}
		switch {
		case fc.IsDeleted:
			res.Action = "deleted"
		case fc.IsNew:
			res.Action = "created"
		default:
			res.Action = "modified"
		}

		target := filepath.Join(root, fc.Path())
		if !projects.WithinRoot(root, target) {
			res.Error = ErrPathEscapesRoot.Error()
			results = append(results, res)
			if firstErr == nil {
				firstErr = fmt.Errorf("%w: %q", ErrPathEscapesRoot, fc.Path())
			}
			continue
		}

		if err := applyOne(target, fc); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		res.Applied = true
		results = append(results, res)
	}

	return results, firstErr
}

func applyOne(target string, fc FileChange) error {
	if fc.IsDeleted {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("fixproposals: removing %q: %w", fc.Path(), err)
		}
		return nil
	}

	var original string
	if !fc.IsNew {
		data, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("fixproposals: reading %q: %w", fc.Path(), err)
		}
		original = string(data)
	}

	newContent, err := ApplyFileChange(original, fc)
	if err != nil {
		return fmt.Errorf("fixproposals: %q: %w", fc.Path(), err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("fixproposals: creating directory for %q: %w", fc.Path(), err)
	}
	if err := os.WriteFile(target, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("fixproposals: writing %q: %w", fc.Path(), err)
	}
	return nil
}
