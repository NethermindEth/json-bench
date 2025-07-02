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

// Existing Components
export { default as Layout } from './Layout'
export { default as LoadingSpinner } from './LoadingSpinner'

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