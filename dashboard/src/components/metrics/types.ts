// Base types for metric components
export interface BaseMetricProps {
  className?: string
  loading?: boolean
  error?: string | null
}

// Per-client metrics table props
export interface PerClientMetricsTableProps extends BaseMetricProps {
  data: ClientMetricData[]
  sortBy?: keyof ClientMetricData
  sortOrder?: 'asc' | 'desc'
  onSort?: (field: keyof ClientMetricData, order: 'asc' | 'desc') => void
  showLatencyBreakdown?: boolean
  showErrorDetails?: boolean
}

export interface ClientMetricData {
  clientId: string
  clientName: string
  clientVersion: string
  totalRequests: number
  successfulRequests: number
  failedRequests: number
  errorRate: number
  averageLatency: number
  p50Latency: number
  p95Latency: number
  p99Latency: number
  minLatency: number
  maxLatency: number
  requestsPerSecond: number
  bytesTransferred: number
  lastUpdated: Date
}

// Method breakdown table props
export interface MethodBreakdownTableProps extends BaseMetricProps {
  data: MethodMetricData[]
  groupBy?: 'method' | 'endpoint' | 'status'
  showPercentages?: boolean
  highlightSlowMethods?: boolean
  slowMethodThreshold?: number
  onMethodClick?: (method: string) => void
}

export interface MethodMetricData {
  method: string
  endpoint: string
  totalCalls: number
  successfulCalls: number
  failedCalls: number
  errorRate: number
  averageLatency: number
  p50Latency: number
  p95Latency: number
  p99Latency: number
  minLatency: number
  maxLatency: number
  callsPerSecond: number
  averageResponseSize: number
  errorBreakdown: ErrorBreakdownData[]
  lastUpdated: Date
}

// Error analysis panel props
export interface ErrorAnalysisPanelProps extends BaseMetricProps {
  data: ErrorAnalysisData
  timeRange?: TimeRange
  showTrends?: boolean
  showErrorCodes?: boolean
  showErrorMessages?: boolean
  maxErrorsToShow?: number
  onErrorClick?: (error: ErrorData) => void
}

export interface ErrorAnalysisData {
  totalErrors: number
  errorRate: number
  errorsByType: ErrorTypeData[]
  errorsByCode: ErrorCodeData[]
  errorsByMessage: ErrorMessageData[]
  errorTrends: ErrorTrendData[]
  topErrors: ErrorData[]
  lastUpdated: Date
}

export interface ErrorTypeData {
  type: string
  count: number
  percentage: number
  examples: string[]
}

export interface ErrorCodeData {
  code: string | number
  count: number
  percentage: number
  message: string
}

export interface ErrorMessageData {
  message: string
  count: number
  percentage: number
  firstSeen: Date
  lastSeen: Date
}

export interface ErrorTrendData {
  timestamp: Date
  errorCount: number
  errorRate: number
  errorTypes: Record<string, number>
}

export interface ErrorData {
  id: string
  type: string
  code: string | number
  message: string
  count: number
  percentage: number
  firstSeen: Date
  lastSeen: Date
  affectedMethods: string[]
  stackTrace?: string
}

export interface ErrorBreakdownData {
  errorType: string
  errorCode: string | number
  errorMessage: string
  count: number
  percentage: number
  firstSeen: Date
  lastSeen: Date
}

// System metrics panel props
export interface SystemMetricsPanelProps extends BaseMetricProps {
  data: SystemMetricsData
  showCPUMetrics?: boolean
  showMemoryMetrics?: boolean
  showNetworkMetrics?: boolean
  showDiskMetrics?: boolean
  refreshInterval?: number
  onRefresh?: () => void
}

export interface SystemMetricsData {
  cpu: CPUMetrics
  memory: MemoryMetrics
  network: NetworkMetrics
  disk: DiskMetrics
  uptime: number
  loadAverage: number[]
  processes: ProcessMetrics[]
  lastUpdated: Date
}

export interface CPUMetrics {
  usage: number
  cores: number
  loadAverage1m: number
  loadAverage5m: number
  loadAverage15m: number
  userTime: number
  systemTime: number
  idleTime: number
  temperature?: number
}

export interface MemoryMetrics {
  total: number
  used: number
  free: number
  cached: number
  buffers: number
  usage: number
  swapTotal: number
  swapUsed: number
  swapFree: number
}

export interface NetworkMetrics {
  bytesReceived: number
  bytesSent: number
  packetsReceived: number
  packetsSent: number
  errors: number
  dropped: number
  interfaces: NetworkInterfaceMetrics[]
}

export interface NetworkInterfaceMetrics {
  name: string
  bytesReceived: number
  bytesSent: number
  packetsReceived: number
  packetsSent: number
  errors: number
  dropped: number
  isUp: boolean
}

export interface DiskMetrics {
  total: number
  used: number
  free: number
  usage: number
  readOperations: number
  writeOperations: number
  readBytes: number
  writeBytes: number
  readTime: number
  writeTime: number
  partitions: DiskPartitionMetrics[]
}

export interface DiskPartitionMetrics {
  device: string
  mountPoint: string
  fsType: string
  total: number
  used: number
  free: number
  usage: number
}

export interface ProcessMetrics {
  pid: number
  name: string
  command: string
  cpuUsage: number
  memoryUsage: number
  memoryRSS: number
  memoryVMS: number
  status: string
  parentPid?: number
  startTime: Date
}

// Time range for filtering
export interface TimeRange {
  start: Date
  end: Date
}

// Common sorting and filtering types
export type SortOrder = 'asc' | 'desc'
export type SortField = string
export type FilterValue = string | number | boolean | Date | null
export type FilterOperator = 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'contains' | 'startsWith' | 'endsWith'

export interface SortConfig {
  field: SortField
  order: SortOrder
}

export interface FilterConfig {
  field: string
  operator: FilterOperator
  value: FilterValue
}

// Export utility types
export type MetricComponentProps = 
  | PerClientMetricsTableProps
  | MethodBreakdownTableProps
  | ErrorAnalysisPanelProps
  | SystemMetricsPanelProps