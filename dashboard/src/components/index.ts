// Visualization Components
export { default as TrendChart } from './TrendChart'
export type { TrendChartProps } from './TrendChart'

export { default as MetricCard, LatencyCard, SuccessRateCard, ThroughputCard } from './MetricCard'
export type { MetricCardProps } from './MetricCard'

export { default as LatencyDistribution } from './LatencyDistribution'
export type { LatencyDistributionProps, LatencyBucket } from './LatencyDistribution'

export { default as ErrorRateChart } from './ErrorRateChart'
export type { ErrorRateChartProps, ErrorDataPoint } from './ErrorRateChart'

export { default as ThroughputChart } from './ThroughputChart'
export type { ThroughputChartProps, ThroughputDataPoint } from './ThroughputChart'

// Enhanced Chart Components
export { 
  EnhancedLatencyDistribution, 
  PerformanceComparisonChart 
} from './charts'
export type { 
  EnhancedLatencyDistributionProps, 
  EnhancedLatencyBucket, 
  FilterContext, 
  StatisticalAnalysis,
  PerformanceComparisonChartProps, 
  ComparisonItem, 
  BaselineData 
} from './charts'

// Comparison and Management Components
export { default as ComparisonView } from './ComparisonView'
export type { ComparisonProps } from './ComparisonView'

export { default as RegressionAlert, RegressionAlertList } from './RegressionAlert'
export type { RegressionAlertProps, RegressionAlertListProps } from './RegressionAlert'

export { default as BaselineManager } from './BaselineManager'
export type { BaselineManagerProps, BaselineAction } from './BaselineManager'

// Existing Components
export { default as Layout } from './Layout'
export { default as LoadingSpinner } from './LoadingSpinner'
export { default as ConnectionStatus } from './ConnectionStatus'

// New Components
export { default as ErrorBoundary } from './ErrorBoundary'
export { default as Breadcrumb } from './Breadcrumb'
export type { BreadcrumbItem, BreadcrumbProps } from './Breadcrumb'

// UI Components
export { ExpandableSection, MetricTable, ExportButton } from './ui'
export type { 
  ExpandableSectionProps, 
  MetricTableProps, 
  ColumnDef,
  ExportButtonProps,
  ExportFormat 
} from './ui'

// Metrics Components
export { 
  PerClientMetricsTable,
  ErrorAnalysisPanel,
  SystemMetricsPanel
} from './metrics'
export type { 
  PerClientMetricsTableProps,
  ErrorAnalysisPanelProps,
  SystemMetricsPanelProps,
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
  ErrorSeverity,
  ErrorImpact
} from './metrics'

// Re-export types from api for convenience
export type {
  TrendPoint,
  TrendData,
  Regression,
  BenchmarkResult,
  ErrorSummary,
  HistoricRun,
  BaselineComparison,
} from '../types/api'