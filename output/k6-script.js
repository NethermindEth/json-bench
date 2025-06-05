
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
  clients: [{"Name":"geth","URL":"http://localhost:8545"},{"Name":"nethermind","URL":"http://localhost:8545"}],
  endpoints: [{"Method":"eth_call","Params":[{"data":"0x70a08231000000000000000000000000000000000000000000000000000000000000000a","to":"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}],"Frequency":"60%"},{"Method":"eth_getLogs","Params":[{"address":["0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"],"fromBlock":"0x1000000","toBlock":"0x1000100"}],"Frequency":"30%"},{"Method":"eth_getBlockByNumber","Params":["latest",true],"Frequency":"10%"}],
  duration: '5m',
  rps: 200,
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
