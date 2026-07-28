package runs

import "context"

// RunInput is what a Runner needs to execute one generated test.
type RunInput struct {
	RunID        string
	SpecFilename string
	SpecContent  string
}

// RunResult is a runner's outcome. Pass/fail is ExitCode == 0, decided
// entirely by the runner process's exit code — never by AI (spec §2.4).
type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ArtifactFile is one file a Runner collected after execution.
type ArtifactFile struct {
	Kind     string
	MimeType string
	Data     []byte
}

// Runner executes one test in isolation (spec §11.2, adapted: Prepare is
// folded into Execute since Phase 5 has exactly one runner
// implementation and no scheduling queue yet — splitting them out is a
// non-breaking refactor for whenever a second runner type is added).
type Runner interface {
	Name() string
	Validate(ctx context.Context, input RunInput) error
	Execute(ctx context.Context, input RunInput) (*RunResult, error)
	CollectArtifacts(ctx context.Context, runID string) ([]ArtifactFile, error)
	Cancel(ctx context.Context, runID string) error
	Cleanup(ctx context.Context, runID string) error
}
