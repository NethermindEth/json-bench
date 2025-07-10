import axios, { AxiosInstance, AxiosRequestConfig, AxiosResponse } from 'axios'
import {
  HistoricRun,
  BenchmarkResult,
  TrendData,
  TrendPoint,
  RunFilter,
  TrendFilter,
  RegressionReport,
  WSMessage,
  NewRunMessage,
  RegressionMessage,
  BaselineUpdateMessage,
  DashboardStats,
  APIError,
  MetricQuery,
  TimeSeriesMetric,
  MethodMetricsData,
  RunDetailsResponse,
  ClientMetrics
} from '../types/api'

/**
 * Custom error class for API-related errors
 */
class BenchmarkAPIError extends Error {
  public readonly status?: number
  public readonly details?: Record<string, string | number | boolean>

  constructor(
    message: string,
    status?: number,
    details?: Record<string, string | number | boolean>
  ) {
    super(message)
    this.name = 'BenchmarkAPIError'
    this.status = status
    this.details = details
  }
}

/**
 * Configuration options for the BenchmarkAPI client
 */
interface BenchmarkAPIConfig {
  baseURL: string
  timeout?: number
  retries?: number
  retryDelay?: number
  authToken?: string
}

/**
 * Comparison result type that matches the backend response
 */
interface Comparison {
  run1: HistoricRun
  run2: HistoricRun
  metrics: {
    [metricName: string]: {
      run1: number
      run2: number
      delta: number
      percentChange: number
    }
  }
  summary: string
}

/**
 * TypeScript API client for the JSON-RPC benchmark dashboard
 * 
 * Provides a complete interface to the Go backend REST API and WebSocket functionality.
 * Includes error handling, retry logic, timeout management, and TypeScript type safety.
 * 
 * @example
 * ```typescript
 * const api = new BenchmarkAPI({
 *   baseURL: 'http://localhost:8080',
 *   timeout: 10000,
 *   retries: 3
 * })
 * 
 * // List recent runs
 * const runs = await api.listRuns({ limit: 10 })
 * 
 * // Get trends
 * const trends = await api.getTrends({ period: '7d', metric: 'avg_latency' })
 * 
 * // Connect to WebSocket for real-time updates
 * const disconnect = api.connectWebSocket((message) => {
 *   console.log('New message:', message)
 * })
 * ```
 */
class BenchmarkAPI {
  private client: AxiosInstance
  private config: BenchmarkAPIConfig
  private wsConnection?: WebSocket

  /**
   * Creates a new BenchmarkAPI client instance
   * 
   * @param config - Configuration options for the API client
   */
  constructor(config: BenchmarkAPIConfig) {
    this.config = {
      timeout: 10000,
      retries: 3,
      retryDelay: 1000,
      ...config
    }

    this.client = axios.create({
      baseURL: this.config.baseURL,
      timeout: this.config.timeout,
      headers: {
        'Content-Type': 'application/json',
        ...(this.config.authToken && {
          'Authorization': `Bearer ${this.config.authToken}`
        })
      }
    })

    // Add response interceptor for error handling
    this.client.interceptors.response.use(
      (response) => response,
      (error) => {
        if (error.response?.data?.error) {
          const apiError: APIError = error.response.data
          throw new BenchmarkAPIError(
            apiError.message || apiError.error,
            error.response.status,
            apiError.details
          )
        }
        throw new BenchmarkAPIError(
          error.message || 'An unknown error occurred',
          error.response?.status
        )
      }
    )
  }

  /**
   * Makes a request with retry logic
   */
  private async makeRequest<T>(
    config: AxiosRequestConfig,
    retries: number = this.config.retries || 3
  ): Promise<AxiosResponse<T>> {
    try {
      return await this.client.request<T>(config)
    } catch (error) {
      if (retries > 0 && this.shouldRetry(error)) {
        await this.delay(this.config.retryDelay || 1000)
        return this.makeRequest<T>(config, retries - 1)
      }
      throw error
    }
  }

  /**
   * Determines if a request should be retried
   */
  private shouldRetry(error: any): boolean {
    // Retry on network errors or 5xx status codes
    return !error.response || (error.response.status >= 500 && error.response.status < 600)
  }

  /**
   * Utility function for delays
   */
  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms))
  }

  /**
   * Builds query parameters from an object
   */
  private buildQueryParams(params: Record<string, any>): string {
    const searchParams = new URLSearchParams()
    
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null) {
        if (Array.isArray(value)) {
          value.forEach(item => searchParams.append(key, String(item)))
        } else {
          searchParams.append(key, String(value))
        }
      }
    })
    
    return searchParams.toString()
  }

  // ==================== RUN MANAGEMENT ====================

  /**
   * Lists historical runs with optional filtering
   * 
   * @param filter - Optional filter criteria for runs
   * @returns Promise resolving to runs response object
   */
  async listRuns(filter?: RunFilter): Promise<{ count: number; limit: number; runs: HistoricRun[] }> {
    const queryParams = filter ? this.buildQueryParams(filter) : ''
    const url = `/api/runs${queryParams ? `?${queryParams}` : ''}`
    
    const response = await this.makeRequest<{ count: number; limit: number; runs: HistoricRun[] }>({
      method: 'GET',
      url
    })
    
    return response.data
  }

  /**
   * Gets a specific run by ID
   * 
   * @param id - The run ID
   * @returns Promise resolving to the run details response containing run and client metrics
   */
  async getRun(id: string): Promise<RunDetailsResponse> {
    const response = await this.makeRequest<RunDetailsResponse>({
      method: 'GET',
      url: `/api/runs/${encodeURIComponent(id)}`
    })
    
    return response.data
  }

  /**
   * Gets the detailed benchmark report for a specific run
   * 
   * @param id - The run ID
   * @returns Promise resolving to the benchmark result
   */
  async getRunReport(id: string): Promise<BenchmarkResult> {
    const response = await this.makeRequest<BenchmarkResult>({
      method: 'GET',
      url: `/api/runs/${encodeURIComponent(id)}/report`
    })
    
    return response.data
  }

  /**
   * Gets method-specific metrics for a run
   * 
   * @param runId - The run ID
   * @returns Promise resolving to method metrics data
   */
  async getRunMethods(runId: string): Promise<MethodMetricsData> {
    const response = await this.makeRequest<MethodMetricsData>({
      method: 'GET',
      url: `/api/runs/${encodeURIComponent(runId)}/methods`
    })
    
    return response.data
  }

  // ==================== TRENDS ====================

  /**
   * Gets trend data based on filter criteria
   * 
   * @param filter - Trend filter parameters
   * @returns Promise resolving to trend data
   */
  async getTrends(filter: TrendFilter): Promise<TrendData> {
    const queryParams = this.buildQueryParams(filter)
    
    const response = await this.makeRequest<TrendData>({
      method: 'GET',
      url: `/api/trends?${queryParams}`
    })
    
    return response.data
  }

  /**
   * Gets trend data for a specific method over time
   * 
   * @param method - The RPC method name
   * @param days - Number of days to look back
   * @returns Promise resolving to array of trend points
   */
  async getMethodTrends(method: string, days: number): Promise<TrendPoint[]> {
    const response = await this.makeRequest<TrendPoint[]>({
      method: 'GET',
      url: `/api/trends/method/${encodeURIComponent(method)}?days=${days}`
    })
    
    return response.data
  }

  /**
   * Gets trend data for a specific client over time
   * 
   * @param client - The client name
   * @param days - Number of days to look back
   * @returns Promise resolving to array of trend points
   */
  async getClientTrends(client: string, days: number): Promise<TrendPoint[]> {
    const response = await this.makeRequest<TrendPoint[]>({
      method: 'GET',
      url: `/api/trends/client/${encodeURIComponent(client)}?days=${days}`
    })
    
    return response.data
  }

  // ==================== BASELINES ====================

  /**
   * Lists all baseline runs
   * 
   * @returns Promise resolving to array of baseline runs
   */
  async listBaselines(): Promise<HistoricRun[]> {
    const response = await this.makeRequest<HistoricRun[]>({
      method: 'GET',
      url: '/api/baselines'
    })
    
    return response.data
  }

  /**
   * Sets a run as a baseline with a given name
   * 
   * @param runId - The run ID to set as baseline
   * @param name - The baseline name
   * @returns Promise that resolves when baseline is set
   */
  async setBaseline(runId: string, name: string): Promise<void> {
    await this.makeRequest({
      method: 'POST',
      url: '/api/baselines',
      data: { runId, name }
    })
  }

  /**
   * Removes a baseline
   * 
   * @param runId - The baseline run ID to remove
   * @returns Promise that resolves when baseline is removed
   */
  async removeBaseline(runId: string): Promise<void> {
    await this.makeRequest({
      method: 'DELETE',
      url: `/api/baselines/${encodeURIComponent(runId)}`
    })
  }

  // ==================== COMPARISONS ====================

  /**
   * Compares two runs and returns detailed comparison data
   * 
   * @param runId1 - First run ID to compare
   * @param runId2 - Second run ID to compare
   * @returns Promise resolving to comparison result
   */
  async compareRuns(runId1: string, runId2: string): Promise<Comparison> {
    const response = await this.makeRequest<Comparison>({
      method: 'GET',
      url: `/api/compare?run1=${encodeURIComponent(runId1)}&run2=${encodeURIComponent(runId2)}`
    })
    
    return response.data
  }

  /**
   * Gets regression report for a specific run compared to baselines
   * 
   * @param runId - The run ID to check for regressions
   * @returns Promise resolving to regression report
   */
  async getRegressions(runId: string): Promise<RegressionReport> {
    const response = await this.makeRequest<RegressionReport>({
      method: 'GET',
      url: `/api/runs/${encodeURIComponent(runId)}/regressions`
    })
    
    return response.data
  }

  // ==================== METRICS ====================

  /**
   * Queries time series metrics
   * 
   * @param query - Metric query parameters
   * @returns Promise resolving to array of time series metrics
   */
  async queryMetrics(query: MetricQuery): Promise<TimeSeriesMetric[]> {
    const response = await this.makeRequest<TimeSeriesMetric[]>({
      method: 'POST',
      url: '/api/metrics/query',
      data: query
    })
    
    return response.data
  }

  // ==================== DASHBOARD STATS ====================

  /**
   * Gets dashboard statistics and overview data
   * 
   * @returns Promise resolving to dashboard statistics
   */
  async getDashboardStats(): Promise<DashboardStats> {
    const response = await this.makeRequest<DashboardStats>({
      method: 'GET',
      url: '/api/dashboard/stats'
    })
    
    return response.data
  }

  // ==================== WEBSOCKET ====================

  /**
   * Connects to the WebSocket endpoint for real-time updates
   * 
   * @param onMessage - Callback function to handle incoming messages
   * @param onError - Optional callback for WebSocket errors
   * @param onClose - Optional callback for WebSocket close events
   * @returns Function to disconnect the WebSocket
   */
  connectWebSocket(
    onMessage: (message: WSMessage) => void,
    onError?: (error: Event) => void,
    onClose?: (event: CloseEvent) => void
  ): () => void {
    // Disconnect existing connection if any
    if (this.wsConnection) {
      this.wsConnection.close()
    }

    // Convert HTTP URL to WebSocket URL
    const wsUrl = this.config.baseURL.replace(/^https?:/, 'ws:') + '/api/ws'
    
    this.wsConnection = new WebSocket(wsUrl)

    this.wsConnection.onmessage = (event) => {
      try {
        const message: WSMessage = JSON.parse(event.data)
        onMessage(message)
      } catch (error) {
        console.error('Failed to parse WebSocket message:', error)
      }
    }

    this.wsConnection.onerror = (error) => {
      console.error('WebSocket error:', error)
      if (onError) {
        onError(error)
      }
    }

    this.wsConnection.onclose = (event) => {
      console.log('WebSocket connection closed:', event.code, event.reason)
      if (onClose) {
        onClose(event)
      }
    }

    // Return disconnect function
    return () => {
      if (this.wsConnection) {
        this.wsConnection.close()
        this.wsConnection = undefined
      }
    }
  }

  /**
   * Checks if WebSocket is connected
   * 
   * @returns True if WebSocket is connected
   */
  isWebSocketConnected(): boolean {
    return this.wsConnection?.readyState === WebSocket.OPEN
  }

  // ==================== UTILITY METHODS ====================

  /**
   * Updates the authentication token
   * 
   * @param token - New authentication token
   */
  setAuthToken(token: string): void {
    this.config.authToken = token
    this.client.defaults.headers.common['Authorization'] = `Bearer ${token}`
  }

  /**
   * Removes the authentication token
   */
  removeAuthToken(): void {
    this.config.authToken = undefined
    delete this.client.defaults.headers.common['Authorization']
  }

  /**
   * Gets the current base URL
   * 
   * @returns The current base URL
   */
  getBaseURL(): string {
    return this.config.baseURL
  }

  /**
   * Updates the base URL
   * 
   * @param baseURL - New base URL
   */
  setBaseURL(baseURL: string): void {
    this.config.baseURL = baseURL
    this.client.defaults.baseURL = baseURL
  }

  /**
   * Performs a health check on the API
   * 
   * @returns Promise resolving to health status
   */
  async healthCheck(): Promise<{ status: string; timestamp: string; version?: string }> {
    const response = await this.makeRequest<{ status: string; timestamp: string; version?: string }>({
      method: 'GET',
      url: '/api/health'
    })
    
    return response.data
  }

  /**
   * Cleans up resources and closes connections
   */
  destroy(): void {
    if (this.wsConnection) {
      this.wsConnection.close()
      this.wsConnection = undefined
    }
  }
}

// ==================== UTILITY FUNCTIONS ====================

/**
 * Type guards for WebSocket messages
 */
function isNewRunMessage(message: WSMessage): message is NewRunMessage {
  return message.type === 'new_run'
}

function isRegressionMessage(message: WSMessage): message is RegressionMessage {
  return message.type === 'regression_detected'
}

function isBaselineUpdateMessage(message: WSMessage): message is BaselineUpdateMessage {
  return message.type === 'baseline_updated'
}

/**
 * Creates a default API client instance with common configuration
 * 
 * @param baseURL - Base URL for the API
 * @param options - Optional configuration overrides
 * @returns Configured BenchmarkAPI instance
 */
function createBenchmarkAPI(
  baseURL: string,
  options?: Partial<BenchmarkAPIConfig>
): BenchmarkAPI {
  return new BenchmarkAPI({
    baseURL,
    timeout: 10000,
    retries: 3,
    retryDelay: 1000,
    ...options
  })
}

// ==================== EXPORTS ====================

// Export the main API class and error types
export { 
  BenchmarkAPI, 
  BenchmarkAPIError, 
  createBenchmarkAPI,
  isNewRunMessage,
  isRegressionMessage,
  isBaselineUpdateMessage,
  type BenchmarkAPIConfig,
  type Comparison
}

// Re-export all types from api.ts for convenience
export * from '../types/api'

// Default export
export default BenchmarkAPI