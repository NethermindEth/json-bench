package generator

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

const (
	K6CommandDefault   = "k6"
	K6ScriptFilename   = "k6-script.js"
	K6ConfigFilename   = "config.json"
	K6RequestsFilename = "requests.csv"

	ReqsCountThresholdFactor = 0.1
)

// K6Script is the script file content to be used for running k6 tests
//
//go:embed scripts/k6-script.js
var K6Script string

// GenerateK6Script generates the k6 script file and returns the path to the file
func GenerateK6Script(cfg *config.Config, outputDir string) (string, error) {
	scriptPath := path.Join(outputDir, K6ScriptFilename)

	err := os.WriteFile(scriptPath, []byte(K6Script), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write k6 script file: %w", err)
	}

	absPath, err := filepath.Abs(scriptPath)
	if err != nil {
		return "", err
	}

	return absPath, nil
}

// GenerateK6Config generates the k6 config file and returns the path to the file
func GenerateK6Config(cfg *config.Config, outputDir string) (string, error) {
	configPath := path.Join(outputDir, K6ConfigFilename)
	scenarios := make(types.K6Scenarios, len(cfg.ResolvedClients))
	config := types.K6Config{
		// TODO: make more k6 options configurable
		Options: types.K6Options{
			Thresholds:        make(types.K6Thresholds, 0),
			Scenarios:         scenarios,
			SystemTags:        []string{"scenario", "status", "url", "group", "check", "error", "error_code"},
			SummaryTrendStats: []string{"avg", "min", "med", "max", "p(90)", "p(95)", "p(99)"},
			Tags: map[string]string{
				"testid": cfg.TestName,
			},
		},
	}

	// Add thresholds to config
	config.Options.Thresholds["http_req_failed"] = []string{"rate < 0.01"}
	for _, call := range cfg.Calls {
		if call.Thresholds != nil {
			// If method name is empty, use the rpc method as identifier
			identifier := call.Name
			if identifier == "" {
				identifier = call.Method
			}
			thresholdsTarget := fmt.Sprintf("http_req_duration{req_name:'%s'}", identifier)
			// Avoid overriding existing thresholds
			if existingThresholds, exists := config.Options.Thresholds[thresholdsTarget]; !exists {
				config.Options.Thresholds[thresholdsTarget] = call.Thresholds
			} else {
				config.Options.Thresholds[thresholdsTarget] = append(existingThresholds, call.Thresholds...)
			}
		}
	}

	// Add scenario to config for each client
	for _, client := range cfg.ResolvedClients {
		tags := make(map[string]string)
		if client.Type != "" {
			tags["client_type"] = client.Type
		}

		if cfg.RPS > 0 {
			scenarios[client.Name] = &types.K6ScenarioCAR{
				K6ScenarioBase: types.K6ScenarioBase{
					Executor: types.K6ScenarioExecutorConstantArrivalRate,
					Env: map[string]string{
						"RPC_CLIENT_ENDPOINT": client.URL,
					},
					Tags: tags,
				},
				Duration:        cfg.Duration,
				Rate:            cfg.RPS,
				PreAllocatedVUs: cfg.VUs,
				TimeUnit:        "1s",
				MaxVUs:          cfg.VUs,
			}
		} else if cfg.Iterations > 0 {
			scenarios[client.Name] = &types.K6ScenarioSI{
				K6ScenarioBase: types.K6ScenarioBase{
					Executor: types.K6ScenarioExecutorSharedIterations,
					Env: map[string]string{
						"RPC_CLIENT_ENDPOINT": client.URL,
					},
					Tags: tags,
				},
				VUs:         cfg.VUs,
				Iterations:  cfg.Iterations,
				MaxDuration: cfg.Duration,
			}
		} else {
			return "", fmt.Errorf("invalid scenario configuration")
		}
	}

	// Write config file
	configFile, err := os.Create(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to create config file: %w", err)
	}
	defer configFile.Close()

	jsonEncoder := json.NewEncoder(configFile)
	jsonEncoder.SetIndent("", "  ")
	jsonEncoder.SetEscapeHTML(false)

	if err = jsonEncoder.Encode(config); err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}

	return absPath, nil
}

// GenerateK6Requests generates the k6 requests file and returns the path to the file
func GenerateK6Requests(cfg *config.Config, outputDir string) (string, error) {
	requestsPath := path.Join(outputDir, K6RequestsFilename)

	requestsFile, err := os.Create(requestsPath)
	if err != nil {
		return "", fmt.Errorf("failed to create requests file: %w", err)
	}
	defer requestsFile.Close()

	writer := csv.NewWriter(requestsFile)

	// Generate requests
	reqsCount := 1

	duration, err := time.ParseDuration(cfg.Duration)
	if err != nil {
		return "", fmt.Errorf("failed to parse config duration: %w", err)
	}

	totalWeight := 0
	for _, call := range cfg.Calls {
		totalWeight += call.Weight
	}

	// Calculate the number of requests to generate based on the duration and RPS or iterations
	var maxRequests int
	if cfg.RPS > 0 {
		maxRequests = int(math.Ceil(float64(cfg.RPS) * duration.Seconds() * (1.0 + ReqsCountThresholdFactor)))
	} else {
		maxRequests = cfg.Iterations
	}
	for reqsCount <= maxRequests {
		reqRand := rand.Float64() * float64(totalWeight)
		cumFreq := 0.0
		for _, call := range cfg.Calls {
			cumFreq += float64(call.Weight)
			if reqRand < cumFreq {
				id := reqsCount

				rpcCall, err := call.Sample()
				if err != nil {
					return "", fmt.Errorf("failed to sample call %s: %w", call.Name, err)
				}

				payload := map[string]any{
					"id":      id,
					"jsonrpc": "2.0",
					"method":  rpcCall.Method,
					"params":  rpcCall.Params,
				}
				payloadJSON, err := json.Marshal(payload)
				if err != nil {
					return "", fmt.Errorf("failed to marshal payload: %w", err)
				}
				writer.Write([]string{strconv.Itoa(id), call.Name, rpcCall.Method, string(payloadJSON)})
				break
			}
		}
		reqsCount++
		if reqsCount%1000 == 0 {
			writer.Flush()
		}
	}
	writer.Flush()

	return requestsPath, nil
}

// GenerateK6Cmd generates the k6 command and returns the command to run
func GenerateK6Cmd(
	cfg *config.Config,
	outputDir string,
	scriptPath string,
	configPath string,
	requestsPath string,
) (*exec.Cmd, string, error) {
	summaryPath := filepath.Join(outputDir, "summary.json")
	absSummaryPath, err := filepath.Abs(summaryPath)
	if err != nil {
		return nil, "", err
	}
	absScriptPath, err := filepath.Abs(scriptPath)
	if err != nil {
		return nil, "", err
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, "", err
	}
	absRequestsPath, err := filepath.Abs(requestsPath)
	if err != nil {
		return nil, "", err
	}

	k6Command := K6CommandDefault // TODO: make it configurable
	k6CommandArgs := []string{
		"run",
		absScriptPath,
		"--env", fmt.Sprintf("RPC_REQUESTS_FILE_PATH=%s", absRequestsPath),
		"--env", fmt.Sprintf("RPC_CONFIG_FILE_PATH=%s", absConfigPath),
		"--summary-mode", "full",
		"--summary-export", absSummaryPath,
	}

	cmd := exec.Command(k6Command, k6CommandArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	cmd = configureOutputs(cfg, cmd)

	return cmd, summaryPath, nil
}

// configureOutputs configures the outputs for the k6 command
func configureOutputs(cfg *config.Config, cmd *exec.Cmd) *exec.Cmd {
	if cfg.Outputs.PrometheusRW != nil {
		cmd.Args = append(cmd.Args, "--out", "experimental-prometheus-rw")
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("K6_PROMETHEUS_RW_SERVER_URL=%s", cfg.Outputs.PrometheusRW.GetRWEndpoint()),
			"K6_PROMETHEUS_RW_TREND_STATS=min,max,avg,med,p(90),p(95),p(99)",
		)
		if cfg.Outputs.PrometheusRW.BasicAuth.Username != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("K6_PROMETHEUS_RW_USERNAME=%s", cfg.Outputs.PrometheusRW.BasicAuth.Username))
		}
		if cfg.Outputs.PrometheusRW.BasicAuth.Password != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("K6_PROMETHEUS_RW_PASSWORD=%s", cfg.Outputs.PrometheusRW.BasicAuth.Password))
		}
	}

	return cmd
}

// GenerateK6 generates the k6 command and returns the command to run, the summary path, and any errors
func GenerateK6(cfg *config.Config, outputDir string) (*exec.Cmd, string, error) {
	// Generate k6 script file
	scriptPath, err := GenerateK6Script(cfg, outputDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate k6 script: %w", err)
	}

	// Generate k6 config file
	configPath, err := GenerateK6Config(cfg, outputDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate k6 config: %w", err)
	}

	// Generate k6 requests file
	requestsPath := cfg.CallsFile
	if cfg.CallsFile == "" {
		requestsPath, err = GenerateK6Requests(cfg, outputDir)
		if err != nil {
			return nil, "", fmt.Errorf("failed to generate k6 requests: %w", err)
		}
	}

	// Generate k6 command
	cmd, summaryPath, err := GenerateK6Cmd(cfg, outputDir, scriptPath, configPath, requestsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate k6 command: %w", err)
	}

	return cmd, summaryPath, nil
}
