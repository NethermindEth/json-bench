// Extended detailed metrics types for enhanced dashboard functionality
// These types provide comprehensive metric analysis and visualization support

export interface DetailedMetrics {
  clientMetrics: ClientMetrics[]
  methodMetrics: MethodMetrics[]
  errorAnalysis: ErrorAnalysis
  systemMetrics: SystemMetrics
  latencyDistribution: LatencyDistribution
  timestamp: string
  runId: string
  duration: number
  totalRequests: number
  environment: EnvironmentInfo
}

export interface ClientMetrics {
  clientName: string
  totalRequests: number
  successRate: number
  errorRate: number
  latencyPercentiles: LatencyPercentiles
  throughput: number
  connectionMetrics: ConnectionMetrics
  errorsByMethod: Record<string, ErrorBreakdown>
  timeSeriesData: TimeSeriesData[]
  statusCodes: Record<number, number>
  reliability: ReliabilityMetrics
  performanceScore: number
}

export interface MethodMetrics {
  methodName: string
  requestCount: number
  latencyPercentiles: LatencyPercentiles
  errorsByClient: Record<string, ErrorBreakdown>
  complexity: number
  reliability: ReliabilityMetrics
  trendIndicator: TrendIndicator
  parameters: ParameterMetrics[]
  performanceScore: number
}

export interface LatencyPercentiles {
  p50: number
  p75: number
  p90: number
  p95: number
  p99: number
  p999: number
  min: number
  max: number
  mean: number
  stdDev: number
  median: number
  iqr: number
  mad: number
  variance: number
  skewness: number
  kurtosis: number
  coefficientOfVariation: number
}

export interface ErrorAnalysis {
  totalErrors: number
  errorsByType: Record<string, ErrorBreakdown>
  errorsByClient: Record<string, ErrorBreakdown>
  errorsByMethod: Record<string, ErrorBreakdown>
  errorTrends: ErrorTrend[]
  errorCategories: ErrorCategory[]
  rootCauses: RootCause[]
  impactAnalysis: ImpactAnalysis
  mitigationSuggestions: string[]
}

export interface SystemMetrics {
  cpuUsage: number
  memoryUsage: number
  networkIO: NetworkMetrics
  diskIO: DiskMetrics
  environment: EnvironmentInfo
  resourceUtilization: ResourceUtilization
  bottlenecks: BottleneckAnalysis[]
  capacity: CapacityMetrics
  healthScore: number
}

export interface ErrorBreakdown {
  count: number
  percentage: number
  lastOccurrence: string
  firstOccurrence: string
  frequency: number
  severity: ErrorSeverity
  impact: ErrorImpact
  resolution: string
  category: string
  httpStatusCode?: number
  errorMessage: string
  stackTrace?: string
}

export interface ErrorTrend {
  timestamp: string
  errorCount: number
  errorRate: number
  errorType: string
  client: string
  method: string
  trend: 'increasing' | 'decreasing' | 'stable'
  changeRate: number
  severity: ErrorSeverity
}

export interface ConnectionMetrics {
  activeConnections: number
  connectionsCreated: number
  connectionsClosed: number
  connectionReuse: number
  avgConnectionAge: number
  connectionPoolSize: number
  connectionTimeouts: number
  dnsResolutionTime: number
  tcpHandshakeTime: number
  tlsHandshakeTime: number
  keepAliveConnections: number
  connectionErrors: number
  maxConnections: number
  connectionQueueDepth: number
}

export interface NetworkMetrics {
  bytesIn: number
  bytesOut: number
  packetsIn: number
  packetsOut: number
  bandwidth: number
  latency: number
  jitter: number
  packetLoss: number
  connectionCount: number
  throughput: number
  retransmissions: number
  duplicatePackets: number
}

export interface DiskMetrics {
  readBytes: number
  writeBytes: number
  readOperations: number
  writeOperations: number
  readLatency: number
  writeLatency: number
  queueDepth: number
  utilization: number
  freeSpace: number
  totalSpace: number
  iops: number
  throughput: number
}

export interface EnvironmentInfo {
  os: string
  architecture: string
  cpuModel: string
  cpuCores: number
  totalMemoryGB: number
  goVersion: string
  k6Version: string
  networkType: string
  region: string
  availability: string
  containerized: boolean
  cloudProvider?: string
  instanceType?: string
  nodeVersion?: string
  pythonVersion?: string
}

export interface LatencyDistribution {
  buckets: LatencyBucket[]
  histogram: HistogramData[]
  percentileDistribution: PercentileDistribution
  outliers: OutlierData[]
  patterns: LatencyPattern[]
}


export interface LatencyBucket {
  min: number
  max: number
  count: number
  percentage: number
  cumulativePercentage: number
}

export interface HistogramData {
  bin: number
  frequency: number
  density: number
  cumulative: number
}

export interface PercentileDistribution {
  percentiles: number[]
  values: number[]
  distribution: number[]
}

export interface OutlierData {
  value: number
  timestamp: string
  client: string
  method: string
  deviation: number
  zScore: number
  category: 'mild' | 'moderate' | 'extreme'
}

export interface LatencyPattern {
  type: 'spike' | 'plateau' | 'gradual_increase' | 'oscillation' | 'normal'
  startTime: string
  endTime: string
  magnitude: number
  confidence: number
  description: string
  affectedClients: string[]
  affectedMethods: string[]
}

export interface ReliabilityMetrics {
  uptime: number
  availability: number
  mttr: number
  mtbf: number
  errorBudget: number
  slaCompliance: number
  serviceLevel: ServiceLevel
  incidents: IncidentMetrics[]
}

export interface TrendIndicator {
  direction: 'improving' | 'degrading' | 'stable'
  confidence: number
  changeRate: number
  forecast: ForecastData[]
  anomalies: AnomalyData[]
}

export interface ParameterMetrics {
  name: string
  type: string
  frequency: number
  errorRate: number
  averageLatency: number
  complexityScore: number
  validationErrors: number
}

export interface ErrorCategory {
  name: string
  description: string
  count: number
  severity: ErrorSeverity
  examples: string[]
  resolution: string
  prevention: string
}

export interface RootCause {
  category: string
  description: string
  frequency: number
  impact: ErrorImpact
  resolution: string
  prevention: string
  relatedErrors: string[]
}

export interface ImpactAnalysis {
  userImpact: number
  systemImpact: number
  businessImpact: number
  financialImpact: number
  reputationImpact: number
  affectedUsers: number
  affectedRequests: number
  downtimeMinutes: number
}

export interface ResourceUtilization {
  cpu: UtilizationMetrics
  memory: UtilizationMetrics
  network: UtilizationMetrics
  disk: UtilizationMetrics
  overall: number
}

export interface UtilizationMetrics {
  current: number
  average: number
  peak: number
  threshold: number
  efficiency: number
  wastage: number
  recommendations: string[]
}

export interface BottleneckAnalysis {
  type: 'cpu' | 'memory' | 'network' | 'disk' | 'database' | 'application'
  severity: 'low' | 'medium' | 'high' | 'critical'
  impact: number
  description: string
  recommendations: string[]
  estimatedImprovement: number
}

export interface CapacityMetrics {
  current: number
  maximum: number
  projected: number
  timeToCapacity: number
  growthRate: number
  recommendations: string[]
}

export interface TimeSeriesData {
  timestamp: string
  value: number
  metric: string
  tags: Record<string, string>
}

export interface ServiceLevel {
  target: number
  actual: number
  budget: number
  remaining: number
  compliance: boolean
  breaches: ServiceLevelBreach[]
}

export interface ServiceLevelBreach {
  timestamp: string
  duration: number
  severity: 'minor' | 'major' | 'critical'
  cause: string
  resolution: string
}

export interface IncidentMetrics {
  timestamp: string
  severity: 'low' | 'medium' | 'high' | 'critical'
  duration: number
  affectedServices: string[]
  rootCause: string
  resolution: string
  preventionMeasures: string[]
}

export interface ForecastData {
  timestamp: string
  predicted: number
  confidence: number
  upperBound: number
  lowerBound: number
}

export interface AnomalyData {
  timestamp: string
  value: number
  expectedValue: number
  anomalyScore: number
  severity: 'low' | 'medium' | 'high'
  description: string
  cause?: string
}



// Enums and constants
export enum ErrorSeverity {
  LOW = 'low',
  MEDIUM = 'medium',
  HIGH = 'high',
  CRITICAL = 'critical'
}

export enum ErrorImpact {
  MINIMAL = 'minimal',
  MODERATE = 'moderate',
  SIGNIFICANT = 'significant',
  SEVERE = 'severe'
}

// Utility type for metric aggregation
export interface MetricAggregation {
  period: string
  count: number
  sum: number
  min: number
  max: number
  avg: number
  median: number
  stdDev: number
  percentiles: LatencyPercentiles
}

// Type guards
export function isDetailedMetrics(obj: any): obj is DetailedMetrics {
  return obj && 
    Array.isArray(obj.clientMetrics) &&
    Array.isArray(obj.methodMetrics) &&
    obj.errorAnalysis &&
    obj.systemMetrics &&
    obj.latencyDistribution
}

export function isClientMetrics(obj: any): obj is ClientMetrics {
  return obj &&
    typeof obj.clientName === 'string' &&
    typeof obj.totalRequests === 'number' &&
    typeof obj.successRate === 'number' &&
    obj.latencyPercentiles &&
    obj.connectionMetrics
}

export function isMethodMetrics(obj: any): obj is MethodMetrics {
  return obj &&
    typeof obj.methodName === 'string' &&
    typeof obj.requestCount === 'number' &&
    obj.latencyPercentiles &&
    typeof obj.errorsByClient === 'object'
}

// Default values
export const defaultLatencyPercentiles: LatencyPercentiles = {
  p50: 0,
  p75: 0,
  p90: 0,
  p95: 0,
  p99: 0,
  p999: 0,
  min: 0,
  max: 0,
  mean: 0,
  stdDev: 0,
  median: 0,
  iqr: 0,
  mad: 0,
  variance: 0,
  skewness: 0,
  kurtosis: 0,
  coefficientOfVariation: 0
}

