package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// K6ScriptTemplate is the template for generating k6 test scripts
const K6ScriptTemplate = `
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { randomItem } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const rpcErrors = new Counter('rpc_errors');
const rpcCalls = new Counter('rpc_calls');
const methodCalls = {};
const methodLatency = {};

// Configuration
const config = {
  clients: {{.ClientsJSON}},
  endpoints: {{.EndpointsJSON}},
  duration: '{{.Duration}}',
  rps: {{.RPS}},
};

// Initialize metrics for each method
config.endpoints.forEach(endpoint => {
  methodCalls[endpoint.method] = new Counter('method_calls_' + endpoint.method);
  methodLatency[endpoint.method] = new Trend('method_latency_' + endpoint.method);
});

// Define test options
export const options = {
  scenarios: {
    constant_load: {
      executor: 'constant-arrival-rate',
      rate: config.rps,
      timeUnit: '1s',
      duration: config.duration,
      preAllocatedVUs: 10,
      maxVUs: 100,
    },
  },
  thresholds: {
    'http_req_failed': ['rate<0.01'], // Less than 1% of requests should fail
    'http_req_duration': ['p(95)<1000'], // 95% of requests should be below 1s
  },
};

// Helper function to create a JSON-RPC request
function createJsonRpcRequest(method, params) {
  return {
    jsonrpc: '2.0',
    id: Math.floor(Math.random() * 1000000),
    method: method,
    params: params,
  };
}

// Select an endpoint based on frequency distribution
function selectEndpoint() {
  const rand = Math.random() * 100;
  let cumulativeFreq = 0;
  
  for (const endpoint of config.endpoints) {
    const freq = parseInt(endpoint.frequency);
    cumulativeFreq += freq;
    
    if (rand <= cumulativeFreq) {
      return endpoint;
    }
  }
  
  return config.endpoints[0]; // Fallback
}

// Main test function
export default function() {
  const endpoint = selectEndpoint();
  const method = endpoint.method;
  const params = endpoint.params;
  
  const payload = createJsonRpcRequest(method, params);
  
  // Send requests to all clients
  for (const client of config.clients) {
    const url = client.url;
    const clientName = client.name;
    
    const headers = {
      'Content-Type': 'application/json',
    };
    
    const startTime = new Date().getTime();
    const response = http.post(url, JSON.stringify(payload), { headers });
    const endTime = new Date().getTime();
    
    // Record metrics
    rpcCalls.add(1, { client: clientName, method });
    methodCalls[method].add(1, { client: clientName });
    methodLatency[method].add(endTime - startTime, { client: clientName });
    
    // Check response
    const success = check(response, {
      'status is 200': (r) => r.status === 200,
      'has result': (r) => {
        try {
          const body = JSON.parse(r.body);
          return body.result !== undefined && body.error === undefined;
        } catch (e) {
          return false;
        }
      },
    });
    
    if (!success) {
      rpcErrors.add(1, { client: clientName, method });
    }
    
    // Store response for validation
    if (__ENV.RECORD_RESPONSES === 'true') {
      const responseFile = open('responses/' + clientName + '_' + method + '_' + payload.id + '.json', 'w');
      responseFile.write(response.body);
      responseFile.close();
    }
  }
  
  // Add a small sleep to avoid overwhelming the clients
  sleep(0.1);
}
`

// ScriptData holds the data to be injected into the k6 script template
type ScriptData struct {
	ClientsJSON   string
	EndpointsJSON string
	Duration      string
	RPS           int
}

// GenerateK6Script generates a k6 script from the benchmark configuration
func GenerateK6Script(cfg *config.Config, outputPath string) error {
	// Convert clients to JSON
	clientsJSON, err := json.Marshal(cfg.Clients)
	if err != nil {
		return fmt.Errorf("failed to marshal clients: %w", err)
	}

	// Convert endpoints to JSON
	endpointsJSON, err := json.Marshal(cfg.Endpoints)
	if err != nil {
		return fmt.Errorf("failed to marshal endpoints: %w", err)
	}

	// Prepare template data
	data := ScriptData{
		ClientsJSON:   string(clientsJSON),
		EndpointsJSON: string(endpointsJSON),
		Duration:      cfg.Duration,
		RPS:           cfg.RPS,
	}

	// Parse template
	tmpl, err := template.New("k6script").Parse(K6ScriptTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// Execute template
	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

// BenchmarkResult is an alias for types.BenchmarkResult
type BenchmarkResult = types.BenchmarkResult

// RunK6Benchmark runs the generated k6 script and returns the results
func RunK6Benchmark(scriptPath, outputDir string) (*BenchmarkResult, error) {
	// Create responses directory
	responsesDir := filepath.Join(outputDir, "responses")
	if err := os.MkdirAll(responsesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create responses directory: %w", err)
	}

	// Prepare k6 command
	summaryPath := filepath.Join(outputDir, "summary.json")
	args := []string{
		"run",
		scriptPath,
		"--summary-export=" + summaryPath,
		"--env", "RECORD_RESPONSES=true",
	}

	// Run k6 command
	cmd := exec.Command("k6", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "K6_RESPONSES_DIR="+responsesDir)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("k6 execution failed: %w", err)
	}

	// Read summary file
	summaryData, err := os.ReadFile(summaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read summary file: %w", err)
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse summary JSON: %w", err)
	}

	// For now, return a minimal result
	// In a real implementation, we would process the responses and calculate diffs
	result := &BenchmarkResult{
		Summary:      summary,
		Timestamp:    fmt.Sprintf("%d", time.Now().Unix()),
		ResponsesDir: responsesDir,
	}

	return result, nil
}
