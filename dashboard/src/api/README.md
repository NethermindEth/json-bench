# Benchmark API Client

A comprehensive TypeScript API client for the JSON-RPC benchmark dashboard backend. This module provides type-safe access to all REST endpoints and WebSocket functionality provided by the Go backend API server.

## Features

- 🎯 **Type-safe**: Full TypeScript support with comprehensive type definitions
- 🔄 **Retry Logic**: Automatic retry with configurable backoff for failed requests
- ⏱️ **Timeout Handling**: Configurable request timeouts
- 🔌 **WebSocket Support**: Real-time updates with WebSocket connection management
- 🛡️ **Error Handling**: Custom error types with detailed error information
- 🔐 **Authentication**: JWT token support for authenticated requests
- 📊 **Query Building**: Automatic query parameter construction for filters
- 🔧 **Configurable**: Flexible configuration options

## Quick Start

```typescript
import { createBenchmarkAPI } from './api'

// Create API client
const api = createBenchmarkAPI('http://localhost:8080', {
  timeout: 15000,
  retries: 3,
  retryDelay: 1000
})

// List recent runs
const runs = await api.listRuns({ limit: 10 })

// Get dashboard statistics
const stats = await api.getDashboardStats()

// Connect to WebSocket for real-time updates
const disconnect = api.connectWebSocket((message) => {
  console.log('New message:', message.type, message.data)
})
```

## API Methods

### Run Management
- `listRuns(filter?)` - List historical runs with optional filtering
- `getRun(id)` - Get a specific run by ID
- `getRunReport(id)` - Get detailed benchmark report for a run

### Trends and Analytics
- `getTrends(filter)` - Get trend data based on filter criteria
- `getMethodTrends(method, days)` - Get trend data for specific method
- `getClientTrends(client, days)` - Get trend data for specific client

### Baselines
- `listBaselines()` - List all baseline runs
- `setBaseline(runId, name)` - Set a run as baseline
- `removeBaseline(runId)` - Remove a baseline

### Comparisons
- `compareRuns(runId1, runId2)` - Compare two runs
- `getRegressions(runId)` - Get regression report for a run

### Metrics
- `queryMetrics(query)` - Query time series metrics
- `getDashboardStats()` - Get dashboard overview statistics

### WebSocket
- `connectWebSocket(onMessage, onError?, onClose?)` - Connect for real-time updates
- `isWebSocketConnected()` - Check WebSocket connection status

### Utilities
- `healthCheck()` - Check API health status
- `setAuthToken(token)` - Set authentication token
- `removeAuthToken()` - Remove authentication token
- `setBaseURL(url)` - Update base URL
- `destroy()` - Clean up resources

## Error Handling

The client uses custom error types for better error handling:

```typescript
import { BenchmarkAPIError } from './api'

try {
  const run = await api.getRun('invalid-id')
} catch (error) {
  if (error instanceof BenchmarkAPIError) {
    console.error(`API Error (${error.status}): ${error.message}`)
    if (error.details) {
      console.error('Details:', error.details)
    }
  }
}
```

## Filtering and Querying

The client supports comprehensive filtering for runs and trends:

```typescript
// Filter runs
const runs = await api.listRuns({
  gitBranch: 'main',
  client: 'geth',
  method: 'eth_getBalance',
  from: '2024-01-01T00:00:00Z',
  to: '2024-12-31T23:59:59Z',
  limit: 50
})

// Get trend data
const trends = await api.getTrends({
  period: '7d',
  client: 'geth',
  metric: 'avg_latency'
})
```

## WebSocket Real-time Updates

Connect to WebSocket for real-time notifications:

```typescript
const disconnect = api.connectWebSocket((message) => {
  switch (message.type) {
    case 'new_run':
      console.log('New benchmark run:', message.data)
      break
    case 'regression_detected':
      console.warn('Regression detected:', message.data)
      break
    case 'baseline_updated':
      console.log('Baseline updated:', message.data)
      break
  }
})

// Clean up when done
disconnect()
```

## Configuration

The API client accepts various configuration options:

```typescript
const api = createBenchmarkAPI('http://localhost:8080', {
  timeout: 15000,      // Request timeout in milliseconds
  retries: 3,          // Number of retry attempts
  retryDelay: 1000,    // Delay between retries in milliseconds
  authToken: 'jwt...'  // Authentication token
})
```

## Type Definitions

All API types are fully typed and exported from the module:

```typescript
import type {
  HistoricRun,
  BenchmarkResult,
  TrendData,
  RunFilter,
  TrendFilter,
  WSMessage
} from './api'
```

## Examples

See `examples.ts` for comprehensive usage examples covering all API functionality.

## Backend Compatibility

This client is designed to work with the Go backend API server and matches all REST endpoints and WebSocket message types. The types are kept in sync with the backend Go structs to ensure compatibility.