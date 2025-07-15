/**
 * Example usage patterns for the BenchmarkAPI client
 * 
 * This file demonstrates common usage patterns and best practices
 * for using the TypeScript API client.
 */

import { createBenchmarkAPI, BenchmarkAPIError } from './client'
import type { RunFilter, TrendFilter, WSMessage } from '../types/api'

/**
 * Example: Basic API client setup and usage
 */
export async function basicUsageExample() {
  // Create API client with default configuration
  const api = createBenchmarkAPI('http://localhost:8080', {
    timeout: 15000,
    retries: 3
  })

  try {
    // Check API health
    const health = await api.healthCheck()
    console.log('API Health:', health)

    // List recent runs
    const recentRuns = await api.listRuns({ limit: 10 })
    console.log(`Found ${recentRuns.length} recent runs`)

    // Get dashboard statistics
    const stats = await api.getDashboardStats()
    console.log('Dashboard stats:', stats)

  } catch (error) {
    if (error instanceof BenchmarkAPIError) {
      console.error(`API Error (${error.status}): ${error.message}`)
      if (error.details) {
        console.error('Details:', error.details)
      }
    } else {
      console.error('Unexpected error:', error)
    }
  }
}

/**
 * Example: Advanced filtering and querying
 */
export async function advancedQueryExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  try {
    // Filter runs by specific criteria
    const filter: RunFilter = {
      gitBranch: 'main',
      client: 'geth',
      method: 'eth_getBalance',
      from: '2024-01-01T00:00:00Z',
      to: '2024-12-31T23:59:59Z',
      limit: 50
    }

    const filteredRuns = await api.listRuns(filter)
    console.log(`Found ${filteredRuns.length} runs matching criteria`)

    // Get trend data
    const trendFilter: TrendFilter = {
      period: '7d',
      client: 'geth',
      metric: 'avg_latency'
    }

    const trends = await api.getTrends(trendFilter)
    console.log(`Trend direction: ${trends.direction}`)
    console.log(`Percent change: ${trends.percentChange}%`)

    // Get specific method trends
    const methodTrends = await api.getMethodTrends('eth_getBalance', 30)
    console.log(`Got ${methodTrends.length} trend points for eth_getBalance`)

  } catch (error) {
    console.error('Query failed:', error)
  }
}

/**
 * Example: Working with baselines and comparisons
 */
export async function baselinesAndComparisonsExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  try {
    // List all baselines
    const baselines = await api.listBaselines()
    console.log(`Found ${baselines.length} baselines`)

    // Set a new baseline
    if (baselines.length > 0) {
      const latestRun = baselines[0]
      await api.setBaseline(latestRun.id, 'production-baseline-v2')
      console.log(`Set baseline for run ${latestRun.id}`)
    }

    // Compare two runs
    if (baselines.length >= 2) {
      const comparison = await api.compareRuns(baselines[0].id, baselines[1].id)
      console.log('Comparison summary:', comparison.summary)
      
      // Print metric differences
      Object.entries(comparison.metrics).forEach(([metric, data]) => {
        console.log(`${metric}: ${data.percentChange.toFixed(2)}% change`)
      })
    }

    // Check for regressions
    const recentRuns = await api.listRuns({ limit: 1 })
    if (recentRuns.length > 0) {
      const regressions = await api.getRegressions(recentRuns[0].id)
      if (regressions.hasCritical) {
        console.warn('Critical regressions detected!')
        console.warn(regressions.summary)
      }
    }

  } catch (error) {
    console.error('Baseline operations failed:', error)
  }
}

/**
 * Example: Real-time updates with WebSocket
 */
export function webSocketExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  // Connect to WebSocket for real-time updates
  const disconnect = api.connectWebSocket(
    (message: WSMessage) => {
      switch (message.type) {
        case 'new_run':
          console.log('New benchmark run completed:', message.data)
          break
        case 'regression_detected':
          console.warn('Regression detected:', message.data)
          break
        case 'baseline_updated':
          console.log('Baseline updated:', message.data)
          break
        default:
          console.log('Unknown message type:', message)
      }
    },
    (error) => {
      console.error('WebSocket error:', error)
    },
    (event) => {
      console.log('WebSocket closed:', event.code, event.reason)
    }
  )

  // Auto-reconnect logic (optional)
  const reconnectInterval = setInterval(() => {
    if (!api.isWebSocketConnected()) {
      console.log('WebSocket disconnected, attempting to reconnect...')
      disconnect() // Clean up old connection
      // Create new connection (recursive call)
      webSocketExample()
      clearInterval(reconnectInterval)
    }
  }, 5000)

  // Return cleanup function
  return () => {
    disconnect()
    clearInterval(reconnectInterval)
    api.destroy()
  }
}

/**
 * Example: Error handling patterns
 */
export async function errorHandlingExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  try {
    // This might fail if run doesn't exist
    const run = await api.getRun('nonexistent-run-id')
    console.log('Run found:', run)

  } catch (error) {
    if (error instanceof BenchmarkAPIError) {
      // Handle API-specific errors
      switch (error.status) {
        case 404:
          console.log('Run not found')
          break
        case 429:
          console.log('Rate limited, please retry later')
          break
        case 500:
          console.error('Server error:', error.message)
          break
        default:
          console.error(`API error (${error.status}): ${error.message}`)
      }
    } else {
      // Handle network or other errors
      console.error('Network or unexpected error:', error)
    }
  }
}

/**
 * Example: Working with metrics and time series data
 */
export async function metricsExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  try {
    // Query specific metrics
    const metrics = await api.queryMetrics({
      from: '2024-01-01T00:00:00Z',
      to: '2024-01-02T00:00:00Z',
      metrics: ['avg_latency', 'throughput', 'error_rate'],
      clients: ['geth', 'nethermind'],
      methods: ['eth_getBalance', 'eth_getBlockByNumber'],
      groupBy: ['client', 'method']
    })

    console.log(`Retrieved ${metrics.length} metric data points`)

    // Process metrics data
    const latencyMetrics = metrics.filter(m => m.metricName === 'avg_latency')
    const avgLatency = latencyMetrics.reduce((sum, m) => sum + m.value, 0) / latencyMetrics.length
    console.log(`Average latency across all clients: ${avgLatency.toFixed(2)}ms`)

  } catch (error) {
    console.error('Metrics query failed:', error)
  }
}

/**
 * Example: Authentication and configuration
 */
export async function authenticationExample() {
  const api = createBenchmarkAPI('http://localhost:8080')

  // Set authentication token
  api.setAuthToken('your-jwt-token-here')

  try {
    // Make authenticated requests
    await api.listRuns()
    console.log('Authenticated request successful')

  } catch (error) {
    if (error instanceof BenchmarkAPIError && error.status === 401) {
      console.log('Authentication failed, removing token')
      api.removeAuthToken()
    }
  }

  // Update base URL if needed
  api.setBaseURL('https://api.example.com')

  // Clean up when done
  api.destroy()
}