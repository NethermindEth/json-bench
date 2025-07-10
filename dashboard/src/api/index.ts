/**
 * API module for the JSON-RPC benchmark dashboard
 * 
 * This module provides a complete TypeScript API client for interacting
 * with the Go backend REST API and WebSocket functionality.
 * 
 * @example
 * ```typescript
 * import { BenchmarkAPI, createBenchmarkAPI } from './api'
 * 
 * // Create API client
 * const api = createBenchmarkAPI('http://localhost:8080')
 * 
 * // Use the client
 * const runs = await api.listRuns({ limit: 10 })
 * ```
 */

// Main API client and utilities
export {
  BenchmarkAPI,
  BenchmarkAPIError,
  createBenchmarkAPI,
  isNewRunMessage,
  isRegressionMessage,
  isBaselineUpdateMessage,
  type BenchmarkAPIConfig,
  type Comparison
} from './client'

// Re-export all API types
export * from '../types/api'

// Default export
export { default } from './client'