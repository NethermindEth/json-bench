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
import { Counter, Rate, Trend, Gauge } from 'k6/metrics';
import { randomItem } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const rpcErrors = new Counter('rpc_errors');
const rpcCalls = new Counter('rpc_calls');
const rpcSuccess = new Counter('rpc_success');
const methodCalls = {};
const methodErrors = {};
const methodSuccess = {};
const methodLatency = {};
const clientMethodCalls = {};
const clientMethodErrors = {};
const clientMethodSuccess = {};
const clientMethodLatency = {};
const clientErrors = new Counter('client_errors');
const clientSuccess = new Counter('client_success');

// Connection metrics
const connectionReuse = new Counter('connection_reuse');
const connectionNew = new Counter('connection_new');
const dnsLookupTime = new Trend('dns_lookup_time');
const tcpHandshakeTime = new Trend('tcp_handshake_time');
const tlsHandshakeTime = new Trend('tls_handshake_time');
const activeConnections = new Gauge('active_connections');

// Configuration
const config = {
  clients: {{.ClientsJSON}},
  endpoints: {{.EndpointsJSON}},
  duration: '{{.Duration}}',
  rps: {{.RPS}},
};

// Initialize metrics for each method and client-method combination
config.endpoints.forEach(endpoint => {
  // Global method metrics
  methodCalls[endpoint.Method] = new Counter('method_calls_' + endpoint.Method);
  methodErrors[endpoint.Method] = new Counter('method_errors_' + endpoint.Method);
  methodSuccess[endpoint.Method] = new Counter('method_success_' + endpoint.Method);
  methodLatency[endpoint.Method] = new Trend('method_latency_' + endpoint.Method);
  
  // Per-client method metrics
  config.clients.forEach(client => {
    const clientName = client.Name;
    if (!clientMethodCalls[clientName]) {
      clientMethodCalls[clientName] = {};
      clientMethodErrors[clientName] = {};
      clientMethodSuccess[clientName] = {};
      clientMethodLatency[clientName] = {};
    }
    clientMethodCalls[clientName][endpoint.Method] = new Counter('client_' + clientName + '_method_calls_' + endpoint.Method);
    clientMethodErrors[clientName][endpoint.Method] = new Counter('client_' + clientName + '_method_errors_' + endpoint.Method);
    clientMethodSuccess[clientName][endpoint.Method] = new Counter('client_' + clientName + '_method_success_' + endpoint.Method);
    clientMethodLatency[clientName][endpoint.Method] = new Trend('client_' + clientName + '_method_latency_' + endpoint.Method);
  });
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
  // Ensure K6 calculates all percentiles including p99
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)', 'p(99.9)', 'count'],
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
    const freq = parseInt(endpoint.Frequency);
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
  const method = endpoint.Method;
  const params = endpoint.Params;
  
  const payload = createJsonRpcRequest(method, params);
  
  // Send requests to all clients
  for (const client of config.clients) {
    const url = client.URL;
    const clientName = client.Name;
    
    const headers = {
      'Content-Type': 'application/json',
    };
    
    const response = http.post(url, JSON.stringify(payload), { headers });
    
    // Record connection metrics
    if (response.timings.dns_lookup > 0) {
      connectionNew.add(1, { client: String(clientName) });
      dnsLookupTime.add(response.timings.dns_lookup);
    } else {
      connectionReuse.add(1, { client: String(clientName) });
    }
    
    if (response.timings.connecting > 0) {
      tcpHandshakeTime.add(response.timings.connecting);
    }
    
    if (response.timings.tls_handshaking > 0) {
      tlsHandshakeTime.add(response.timings.tls_handshaking);
    }
    
    // Record metrics
    rpcCalls.add(1, { client: String(clientName), method: String(method) });
    methodCalls[method].add(1);
    
    // Use http_req_duration if available, otherwise use response.timings.duration
    // Note: response.timings.duration can sometimes be 0 under high load
    const duration = response.timings.duration;
    
    methodLatency[method].add(duration);
    
    // Record client-specific metrics
    clientMethodCalls[clientName][method].add(1);
    clientMethodLatency[clientName][method].add(duration);
    
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
    
    if (success) {
      rpcSuccess.add(1, { client: String(clientName), method: String(method) });
      methodSuccess[method].add(1);
      clientMethodSuccess[clientName][method].add(1);
      clientSuccess.add(1, { client: String(clientName) });
    } else {
      rpcErrors.add(1, { client: String(clientName), method: String(method) });
      methodErrors[method].add(1);
      clientMethodErrors[clientName][method].add(1);
      clientErrors.add(1, { client: String(clientName) });
    }
    
    // Store response for validation - using console.log instead of file operations
    if (__ENV.RECORD_RESPONSES === 'true') {
      // File operations are only available in init context, so we'll log the response
      // The runner can capture this output and save it to a file if needed
      console.log('RESPONSE_DATA:' + String(clientName) + ':' + String(method) + ':' + payload.id + ':' + response.body);
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
	// Convert resolved clients to a simpler format for K6 script
	simpleClients := make([]map[string]string, len(cfg.ResolvedClients))
	for i, client := range cfg.ResolvedClients {
		simpleClients[i] = map[string]string{
			"Name": client.Name,
			"URL":  client.URL,
		}
	}

	// Convert clients to JSON
	clientsJSON, err := json.Marshal(simpleClients)
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

// Use types.BenchmarkResult directly

// RunK6Benchmark runs the generated k6 script and returns the results
func RunK6Benchmark(scriptPath, outputDir string) (*types.BenchmarkResult, error) {
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

	startTime := time.Now()
	err := cmd.Run()
	endTime := time.Now()

	// Continue even if k6 fails, as we may still have partial results
	if err != nil {
		fmt.Printf("Warning: k6 execution completed with errors: %v\n", err)
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

	// Parse k6 results to extract client metrics
	clientMetrics, err := ParseK6Results(outputDir)
	if err != nil {
		// Log error but continue with basic results
		fmt.Printf("Warning: Failed to parse detailed metrics: %v\n", err)
	}

	// Create result
	result := &types.BenchmarkResult{
		Summary:       summary,
		ClientMetrics: clientMetrics,
		Timestamp:     time.Now().Format("2006-01-02 15:04:05"),
		StartTime:     startTime.Format("2006-01-02 15:04:05"),
		EndTime:       endTime.Format("2006-01-02 15:04:05"),
		Duration:      endTime.Sub(startTime).String(),
		ResponsesDir:  responsesDir,
	}

	return result, nil
}
