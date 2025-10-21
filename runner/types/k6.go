package types

type K6ScenarioExecutor string

const (
	K6ScenarioExecutorSharedIterations    K6ScenarioExecutor = "shared-iterations"
	K6ScenarioExecutorConstantArrivalRate K6ScenarioExecutor = "constant-arrival-rate"
)

// K6Scenario represents any k6 scenario configuration
type K6Scenario interface {
	GetExecutor() K6ScenarioExecutor
}

// K6ScenarioBase is a configuration for a k6 scenario base
type K6ScenarioBase struct {
	Executor K6ScenarioExecutor `json:"executor"`
	Env      map[string]string  `json:"env,omitempty"`
	Tags     map[string]string  `json:"tags,omitempty"`
}

// Implement K6Scenario interface for K6ScenarioBase
func (s *K6ScenarioBase) GetExecutor() K6ScenarioExecutor {
	return s.Executor
}

// K6ScenarioSI is a configuration for a k6 scenario executor that uses shared iterations
type K6ScenarioSI struct {
	K6ScenarioBase `json:",inline"`
	VUs            int    `json:"vus,omitempty"`
	Iterations     int    `json:"iterations,omitempty"`
	MaxDuration    string `json:"maxDuration,omitempty"`
}

// K6ScenarioCAR is a configuration for a k6 scenario executor that uses constant arrival rate
type K6ScenarioCAR struct {
	K6ScenarioBase  `json:",inline"`
	Duration        string `json:"duration"`
	Rate            int    `json:"rate"`
	PreAllocatedVUs int    `json:"preAllocatedVUs"`
	TimeUnit        string `json:"timeUnit,omitempty"`
	MaxVUs          int    `json:"maxVUs,omitempty"`
}

// K6Scenarios is a map of scenario names to k6 scenarios configurations
type K6Scenarios map[string]K6Scenario

// K6Thresholds is a map of threshold variables to threshold configurations
type K6Thresholds map[string][]string

// K6Options is a configuration for a k6 options
type K6Options struct {
	Scenarios         K6Scenarios       `json:"scenarios"`
	Thresholds        K6Thresholds      `json:"thresholds,omitempty"`
	SystemTags        []string          `json:"systemTags,omitempty"`
	SummaryTrendStats []string          `json:"summaryTrendStats,omitempty"`
	Tags              map[string]string `json:"tags,omitempty"`
}

// K6Config is a configuration for a k6 test
type K6Config struct {
	Options K6Options `json:"options"`
}
