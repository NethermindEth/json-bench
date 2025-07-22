// API types for the JSON-RPC benchmark dashboard
// These match the Go backend types

export interface HistoricRun {
  id: string
  timestamp: string
  git_commit: string
  git_branch: string
  test_name: string
  description: string
  config_hash: string
  result_path: string
  duration: string
  total_requests: number
  success_rate: number
  avg_latency_ms: number
  p95_latency_ms: number
  p99_latency_ms: number
  max_latency_ms: number
  total_errors: number
  overall_error_rate: number
  clients: string[]
  methods: string[]
  tags: string[]
  is_baseline: boolean
  baseline_name?: string
  best_client: string
  clients_count: number
  endpoints_count: number
  target_rps: number
  start_time: string
  end_time: string
  created_at: string
  notes?: string
  full_results?: string  // Contains JSON string of full benchmark results
}

export interface TimeSeriesMetric {
  time: string
  runId: string
  client: string
  method: string
  metricName: string
  value: number
  tags: Record<string, string>
}

export interface Regression {
  runId: string
  baselineId: string
  metricName: string
  baselineValue: number
  currentValue: number
  percentChange: number
  severity: 'minor' | 'major' | 'critical'
}

export interface TrendPoint {
  timestamp: string
  value: number
  runId: string
  metadata?: Record<string, string | number | boolean>
}

export interface TrendData {
  period: string
  trendPoints: TrendPoint[]
  direction: 'improving' | 'degrading' | 'stable'
  percentChange: number
  forecast?: TrendPoint[]
}

export interface BaselineComparison {
  baselineRun: HistoricRun
  currentRun: BenchmarkResult
  regressions: Regression[]
  improvements: Improvement[]
  summary: string
}

export interface Improvement {
  runId: string
  baselineId: string
  metricName: string
  baselineValue: number
  currentValue: number
  percentChange: number
  significance: 'minor' | 'major' | 'significant'
}

export interface RegressionReport {
  runId: string
  regressions: Regression[]
  hasCritical: boolean
  summary: string
}

// Extended types for the benchmark result (from the main tool)
export interface BenchmarkResult {
  testName: string
  timestamp: string
  duration: string
  totalRequests: number
  successfulRequests: number
  failedRequests: number
  successRate: number
  avgLatency: number
  minLatency: number
  maxLatency: number
  p50Latency: number
  p75Latency: number
  p90Latency: number
  p95Latency: number
  p99Latency: number
  p999Latency: number
  throughput: number
  errors: ErrorSummary[]
  clientResults: ClientResult[]
  methodResults: MethodResult[]
  systemMetrics?: SystemMetrics
}

export interface ErrorSummary {
  error: string
  count: number
  percentage: number
}

export interface ClientResult {
  client: string
  requests: number
  successRate: number
  avgLatency: number
  p95Latency: number
  errors: number
}

export interface MethodResult {
  method: string
  requests: number
  successRate: number
  avgLatency: number
  p95Latency: number
  errors: number
}

export interface SystemMetrics {
  cpuUsage: number
  memoryUsage: number
  networkRx: number
  networkTx: number
  timestamp: string
}

// API request/response types
export interface RunFilter {
  gitBranch?: string
  client?: string
  method?: string
  testName?: string
  isBaseline?: boolean
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export interface TrendFilter {
  period: '24h' | '7d' | '30d' | '90d'
  client?: string
  method?: string
  metric?: string
  gitBranch?: string
}

export interface MetricQuery {
  from: string
  to: string
  metrics: string[]
  clients?: string[]
  methods?: string[]
  groupBy?: string[]
}

// Method metrics data returned by /api/runs/{id}/methods endpoint
export interface MethodMetricsData {
  run_id: string
  client?: string
  methods?: {
    [methodName: string]: {
      total_requests?: number
      success_rate?: number
      avg_latency?: number | null
      p50_latency?: number | null
      p95_latency?: number | null
      p99_latency?: number | null
      min_latency?: number | null
      max_latency?: number | null
      error_rate?: number
      throughput?: number
      std_dev?: number | null
    }
  }
  methods_by_client?: {
    [clientName: string]: {
      [methodName: string]: {
        total_requests?: number
        success_rate?: number
        avg_latency?: number | null
        p50_latency?: number | null
        p95_latency?: number | null
        p99_latency?: number | null
        min_latency?: number | null
        max_latency?: number | null
        error_rate?: number
        throughput?: number
        std_dev?: number | null
      }
    }
  }
}

export interface ComparisonRequest {
  run1Id: string
  run2Id: string
}

export interface ComparisonResponse {
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

// WebSocket message types
export interface WSMessage {
  type: 'new_run' | 'regression_detected' | 'baseline_updated'
  data: unknown
}

export interface NewRunMessage extends WSMessage {
  type: 'new_run'
  data: HistoricRun
}

export interface RegressionMessage extends WSMessage {
  type: 'regression_detected'
  data: Regression
}

export interface BaselineUpdateMessage extends WSMessage {
  type: 'baseline_updated'
  data: {
    runId: string
    baselineName: string
    action: 'created' | 'updated' | 'deleted'
  }
}

// Dashboard statistics
export interface DashboardStats {
  totalRuns: number
  lastRunTime: string
  successRate: number
  activeRegressions: number
  avgLatencyTrend: number
  throughputTrend: number
}

// Per-client metrics types
export interface LatencyMetrics {
  avg: number
  min: number
  max: number
  p50: number
  p90: number
  p95: number
  p99: number
  std_dev: number
  throughput: number
  count: number
}

export interface MethodMetrics {
  count: number
  success_count: number
  error_count: number
  success_rate: number
  error_rate: number
  avg: number
  min: number
  max: number
  p50: number
  p90: number
  p95: number
  p99: number
  std_dev: number
  throughput: number
  coeff_var: number
}

export interface ClientMetrics {
  total_requests: number
  total_errors: number
  success_rate: number
  error_rate: number
  latency: LatencyMetrics
  methods: Record<string, MethodMetrics>
}

export interface RunDetailsResponse {
  run: HistoricRun
  client_metrics?: Record<string, ClientMetrics>
}

// API error response
export interface APIError {
  error: string
  message: string
  details?: Record<string, string | number | boolean>
}

// Grafana SimpleJSON datasource types
export interface GrafanaQuery {
  targets: GrafanaTarget[]
  range: {
    from: string
    to: string
  }
  interval: string
  maxDataPoints: number
}

export interface GrafanaTarget {
  target: string
  type: 'timeserie' | 'table'
  data?: Record<string, unknown>
}

export interface GrafanaResponse {
  target: string
  datapoints: Array<[number, number]> // [value, timestamp]
}

export interface GrafanaTableResponse {
  columns: Array<{ text: string; type: string }>
  rows: Array<Array<string | number | boolean | null>>
  type: 'table'
}