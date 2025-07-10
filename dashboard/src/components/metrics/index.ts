// Export metric components
export { PerClientMetricsTable } from './PerClientMetricsTable'
export type { PerClientMetricsTableProps } from './PerClientMetricsTable'

// MethodBreakdownTable has been deprecated - method performance is now shown in PerClientMetricsTable

export { ErrorAnalysisPanel } from './ErrorAnalysisPanel'
export type { ErrorAnalysisPanelProps } from './ErrorAnalysisPanel'

export { SystemMetricsPanel } from './SystemMetricsPanel'
export type { SystemMetricsPanelProps } from './SystemMetricsPanel'

// Export types from the components that are implemented
export type { 
  // Data types from detailed-metrics.ts
  DetailedMetrics,
  ClientMetrics,
  MethodMetrics,
  ErrorAnalysis,
  SystemMetrics,
  LatencyPercentiles,
  ConnectionMetrics,
  ReliabilityMetrics,
  ErrorBreakdown,
  TimeSeriesData,
  // Utility types
  ErrorSeverity,
  ErrorImpact
} from '../../types/detailed-metrics'