package types

type K6ScenarioExecutor string

const (
	K6ScenarioExecutorSharedIterations    K6ScenarioExecutor = "shared-iterations"
	K6ScenarioExecutorConstantArrivalRate K6ScenarioExecutor = "constant-arrival-rate"
)

// K6Scenarios is a map of scenario names to k6 scenarios configurations
type K6Scenarios map[string]K6Scenario

// K6Thresholds is a map of threshold variables to threshold configurations
type K6Thresholds map[string][]string

// K6Scenario is a configuration for a k6 scenario
type K6Scenario struct {
	Executor        K6ScenarioExecutor `json:"executor"`
	Rate            int                `json:"rate,omitempty"`
	TimeUnit        string             `json:"timeUnit,omitempty"`
	Duration        string             `json:"duration,omitempty"`
	PreAllocatedVUs int                `json:"preAllocatedVUs,omitempty"`
	MaxVUs          int                `json:"maxVUs,omitempty"`
	Env             map[string]string  `json:"env,omitempty"`
	Tags            map[string]string  `json:"tags,omitempty"`
}

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
