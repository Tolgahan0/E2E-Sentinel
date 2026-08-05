package runs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var artifactMIMETypes = map[string]string{
	".png":  "image/png",
	".webm": "video/webm",
	".zip":  "application/zip",
}

var artifactKinds = map[string]string{
	".png":  "screenshot",
	".webm": "video",
	".zip":  "trace",
}

// collectPlaywrightArtifacts walks a run workspace's test-results/
// directory for Playwright's own output (screenshots/videos/traces,
// written there when a test fails — spec §10.1's "Screenshot on
// failure", "Video on failure"). Shared between DockerPlaywrightRunner
// and LocalPlaywrightRunner: both write into an identical directory
// layout (workspace root containing the generated spec +
// playwright.config.ts), only how that workspace is executed
// (container vs. host process) differs.
func collectPlaywrightArtifacts(workspaceDir string) ([]ArtifactFile, error) {
	resultsDir := filepath.Join(workspaceDir, "test-results")
	if _, err := os.Stat(resultsDir); os.IsNotExist(err) {
		return nil, nil // no failures means Playwright may not have created this directory at all
	}

	var files []ArtifactFile
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("runs: reading %q: %w", dir, err)
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if err := walk(full); err != nil {
					return err
				}
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			kind, ok := artifactKinds[ext]
			if !ok {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return fmt.Errorf("runs: reading artifact %q: %w", full, err)
			}
			files = append(files, ArtifactFile{Kind: kind, MimeType: artifactMIMETypes[ext], Data: data})
		}
		return nil
	}

	if err := walk(resultsDir); err != nil {
		return nil, err
	}
	return files, nil
}
