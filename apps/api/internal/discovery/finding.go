// Package discovery implements deterministic, evidence-based repository
// scanning (spec §7). It never executes anything found in the scanned
// repository and never follows a symlink that would escape the
// validated project root.
package discovery

// Category values for a Finding. Kept as plain strings (not an enum type)
// so future categories can be added without a breaking change.
const (
	CategoryLanguage  = "language"
	CategoryFramework = "framework"
	CategoryDocker    = "docker"
	CategoryCI        = "ci"
	CategoryTestTool  = "test_tool"
	CategoryAPISchema = "api_schema"
	CategoryDatabase  = "database"
)

// Confidence levels, per spec §8.2 / §9.4 — never present low-confidence
// inference as a confirmed fact.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Finding is one piece of deterministic evidence about the scanned
// repository (a detected language, framework, CI system, existing test
// tool, etc.).
type Finding struct {
	Category   string
	Name       string
	Path       string // first matching path, relative to the project root
	Confidence string
	Evidence   map[string]any
}
