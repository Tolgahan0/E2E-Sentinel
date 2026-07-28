package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDangerousRoot is returned when a candidate project root is a system
// directory that must never be scanned wholesale (spec §23.4).
var ErrDangerousRoot = errors.New("projects: refusing to use a system root directory as a project root")

// dangerousRoots is a defense-in-depth denylist of absolute paths that
// must never be accepted as a project root, even if the caller has
// filesystem permission to read them. This is not a substitute for
// running E2E Sentinel as a non-root, least-privilege user — it only
// stops obviously wrong input (e.g. "/", "/etc") from being scanned.
var dangerousRoots = []string{
	"/", "/etc", "/bin", "/sbin", "/usr", "/var", "/lib", "/lib64",
	"/boot", "/dev", "/proc", "/sys", "/System", "/Library", "/private",
	"/root",
	// macOS symlinks several of the above into /private; EvalSymlinks
	// resolves candidates to these targets, so they must be listed too.
	"/private/etc", "/private/var", "/private/tmp",
}

// ValidateRepositoryPath resolves candidatePath to an absolute, symlink-
// free directory path and rejects it if it doesn't exist, isn't a
// directory, or resolves to a system root. The returned path is what must
// be stored and scanned — never the caller-supplied string as-is.
func ValidateRepositoryPath(candidatePath string) (string, error) {
	if candidatePath == "" {
		return "", fmt.Errorf("%w: repository_path is required", ErrInvalidInput)
	}

	abs, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", fmt.Errorf("%w: resolving absolute path: %v", ErrInvalidInput, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: repository_path does not exist", ErrInvalidInput)
		}
		return "", fmt.Errorf("%w: resolving symlinks: %v", ErrInvalidInput, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: repository_path does not exist", ErrInvalidInput)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: repository_path is not a directory", ErrInvalidInput)
	}

	clean := filepath.Clean(resolved)
	for _, denied := range dangerousRoots {
		if clean == denied {
			return "", ErrDangerousRoot
		}
	}

	return clean, nil
}

// WithinRoot reports whether candidate (already cleaned/absolute) is
// root itself or a descendant of it. Used by the discovery walker to
// double-check that no path it is about to touch — including through a
// symlink — escapes the validated project root.
func WithinRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !hasParentTraversal(rel)
}

func hasParentTraversal(rel string) bool {
	return len(rel) >= 2 && rel[:2] == ".."
}
