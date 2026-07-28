// Package planning implements deterministic, rule-based test planning
// (spec §9): suggested test cases derived from what discovery, service,
// and graph data already established — no AI required (spec §16.6,
// §25 Phase 4 acceptance: "Plan generation works without AI using
// deterministic rules").
package planning

import "context"

// Categories (subset of spec §9.1 derivable from Phase 1-3 data without
// runtime observation or AI).
const (
	CategoryAuthentication  = "authentication"
	CategoryAuthorization   = "authorization"
	CategoryTenantIsolation = "tenant_isolation"
	CategoryCRUD            = "crud"
	CategoryAPISchema       = "api_schema"
	CategoryErrorHandling   = "error_handling"
	CategorySmoke           = "smoke"
	CategoryCriticalJourney = "critical_user_journey"
)

// Statuses (spec §6.6).
const (
	StatusSuggested = "suggested"
	StatusApproved  = "approved"
	StatusGenerated = "generated"
	StatusReady     = "ready"
	StatusRunning   = "running"
	StatusPassed    = "passed"
	StatusFailed    = "failed"
	StatusFlaky     = "flaky"
	StatusBlocked   = "blocked"
	StatusDisabled  = "disabled"
)

// Approval statuses. Distinct from Status: Status tracks the execution
// lifecycle, ApprovalStatus tracks the explicit human gate (spec §2.3).
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
)

// Priorities, highest risk first.
const (
	PriorityP0 = "P0"
	PriorityP1 = "P1"
	PriorityP2 = "P2"
	PriorityP3 = "P3"
)

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// TestCase is a (suggested or approved) test case, spec §6.6.
type TestCase struct {
	ID                  string
	ProjectID           string
	Title               string
	Description         string
	Category            string
	Framework           string
	Status              string
	RiskLevel           string
	Priority            string
	Confidence          string
	Source              string
	Preconditions       string
	Steps               []string
	Assertions          []string
	RequiredCredentials []string
	IsMutating          bool
	IsProductionSafe    bool
	GeneratedFilePath   string
	ApprovalStatus      string
	// RoutePath and RouteMethod carry the structured route info the
	// deterministic spec generator (internal/testgen) needs — Steps is
	// human-readable prose, not a safe parse target for codegen.
	// RouteMethod is "" for a browser page route.
	RoutePath   string
	RouteMethod string
	// NaturalKey identifies a suggestion across regenerations (e.g.
	// "authentication|POST /api/v1/auth/login") so re-running planning
	// never duplicates or overwrites a test the user already
	// reviewed/edited/approved (spec §7.1 idempotency principle).
	NaturalKey string
}

// ErrNotFound is returned when a test case ID does not exist.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "planning: test case not found" }

// ErrProductionUnsafe is returned when approving a mutating test would
// run against a project environment classified production or unknown
// (spec §2.6, §25 Phase 4 acceptance: "Production-unsafe tests cannot be
// approved accidentally").
var ErrProductionUnsafe = errProductionUnsafe{}

type errProductionUnsafe struct{}

func (errProductionUnsafe) Error() string {
	return "planning: mutating test cannot be approved for a production/unknown-classified environment"
}

// Store persists test cases.
type Store interface {
	// CreateIfAbsent inserts tc unless a test case with the same
	// (project_id, natural_key) already exists, in which case it leaves
	// the existing row untouched and reports created=false.
	CreateIfAbsent(ctx context.Context, tc TestCase) (result TestCase, created bool, err error)
	List(ctx context.Context, projectID string) ([]TestCase, error)
	Get(ctx context.Context, id string) (TestCase, error)
	UpdateApproval(ctx context.Context, id, approvalStatus string) (TestCase, error)
	Update(ctx context.Context, id string, title, description, priority string) (TestCase, error)
}
