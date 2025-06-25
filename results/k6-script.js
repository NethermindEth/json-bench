
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { randomItem } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics
const rpcErrors = new Counter('rpc_errors');
const rpcCalls = new Counter('rpc_calls');
const methodCalls = {};
const methodLatency = {};
const clientMethodCalls = {};
const clientMethodLatency = {};

// Configuration
const config = {
  clients: [{"Name":"geth","URL":"http://74.207.227.67:8545"},{"Name":"nethermind","URL":"http://172.232.169.243:8545"}],
  endpoints: [{"Method":"eth_call","Params":[{"data":"0x70a08231000000000000000000000000000000000000000000000000000000000000000a","to":"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}],"Frequency":"40%"},{"Method":"eth_getBalance","Params":["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045","latest"],"Frequency":"20%"},{"Method":"eth_blockNumber","Params":[],"Frequency":"20%"},{"Method":"eth_getTransactionCount","Params":["0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045","latest"],"Frequency":"10%"},{"Method":"eth_getBlockByNumber","Params":["latest",false],"Frequency":"10%"}],
  duration: '1m',
  rps: 300,
};

// Initialize metrics for each method and client-method combination
config.endpoints.forEach(endpoint => {
  // Global method metrics
  methodCalls[endpoint.Method] = new Counter('method_calls_' + endpoint.Method);
  methodLatency[endpoint.Method] = new Trend('method_latency_' + endpoint.Method);
  
  // Per-client method metrics
  config.clients.forEach(client => {
    const clientName = client.Name;
    if (!clientMethodCalls[clientName]) {
      clientMethodCalls[clientName] = {};
      clientMethodLatency[clientName] = {};
    }
    clientMethodCalls[clientName][endpoint.Method] = new Counter('client_' + clientName + '_method_calls_' + endpoint.Method);
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
    
    const startTime = new Date().getTime();
    const response = http.post(url, JSON.stringify(payload), { headers });
    const endTime = new Date().getTime();
    
    // Record metrics
    rpcCalls.add(1, { client: String(clientName), method: String(method) });
    methodCalls[method].add(1);
    methodLatency[method].add(endTime - startTime);
    
    // Record client-specific metrics
    clientMethodCalls[clientName][method].add(1);
    clientMethodLatency[clientName][method].add(endTime - startTime);
    
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
      rpcErrors.add(1, { client: String(clientName), method: String(method) });
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
