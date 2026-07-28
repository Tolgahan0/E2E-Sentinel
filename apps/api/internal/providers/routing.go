package providers

// Task types eligible for provider routing (spec §16.4).
const (
	TaskArchitectureAnalysis = "architecture_analysis"
	TaskTestPlanning         = "test_planning"
	TaskTestGeneration       = "test_generation"
	TaskFailureAnalysis      = "failure_analysis"
	TaskFixGeneration        = "fix_generation"
	TaskReportSummarization  = "report_summarization"
)

// AllTasks lists every routable task type, in a stable order suitable
// for rendering in the web panel.
var AllTasks = []string{
	TaskArchitectureAnalysis,
	TaskTestPlanning,
	TaskTestGeneration,
	TaskFailureAnalysis,
	TaskFixGeneration,
	TaskReportSummarization,
}

var validTasks = func() map[string]bool {
	m := make(map[string]bool, len(AllTasks))
	for _, t := range AllTasks {
		m[t] = true
	}
	return m
}()

// ValidTask reports whether t is a recognized task type.
func ValidTask(t string) bool {
	return validTasks[t]
}

// RoutingSettingsKey is the internal/settings key under which the
// task-type -> provider-ID routing map is stored, as a JSON object.
const RoutingSettingsKey = "ai.task_routing"
