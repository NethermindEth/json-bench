import { useState, useMemo } from 'react'
import {
  ArrowTrendingUpIcon,
  ArrowTrendingDownIcon,
  MinusIcon,
  ArrowDownTrayIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  ExclamationTriangleIcon,
  CheckCircleIcon,
} from '@heroicons/react/24/outline'
import { HistoricRun, BenchmarkResult } from '../types/api'
import LoadingSpinner from './LoadingSpinner'
import { formatLatencyValue } from '../utils/metric-formatters'

export interface ComparisonProps {
  run1: HistoricRun
  run2: HistoricRun
  run1Report?: BenchmarkResult
  run2Report?: BenchmarkResult
  loading?: boolean
  onExport?: (format: 'csv' | 'json' | 'pdf') => void
}

interface MetricComparison {
  name: string
  displayName: string
  unit?: string
  run1Value: number
  run2Value: number
  delta: number
  percentChange: number
  direction: 'improvement' | 'regression' | 'neutral'
  significance: 'low' | 'medium' | 'high'
  higherIsBetter: boolean
}

const METRICS_CONFIG = [
  { key: 'successRate', name: 'Success Rate', unit: '%', higherIsBetter: true },
  { key: 'avgLatency', name: 'Average Latency', unit: 'ms', higherIsBetter: false },
  { key: 'p95Latency', name: 'P95 Latency', unit: 'ms', higherIsBetter: false },
  { key: 'p99Latency', name: 'P99 Latency', unit: 'ms', higherIsBetter: false },
  { key: 'throughput', name: 'Throughput', unit: 'req/s', higherIsBetter: true },
  { key: 'totalRequests', name: 'Total Requests', unit: '', higherIsBetter: true },
  { key: 'failedRequests', name: 'Failed Requests', unit: '', higherIsBetter: false },
]

/**
 * ComparisonView component provides side-by-side comparison of two benchmark runs
 * with delta calculations, color-coded improvements/regressions, and export functionality
 */
export default function ComparisonView({
  run1,
  run2,
  run1Report,
  run2Report,
  loading = false,
  onExport,
}: ComparisonProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(['overview', 'metrics'])
  )
  const [sortBy, setSortBy] = useState<'name' | 'percentChange' | 'significance'>('significance')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')

  // Calculate metric comparisons
  const metricComparisons = useMemo((): MetricComparison[] => {
    if (!run1Report || !run2Report) return []

    return METRICS_CONFIG.map(config => {
      const run1Value = (run1Report as any)[config.key] || 0
      const run2Value = (run2Report as any)[config.key] || 0
      const delta = run2Value - run1Value
      const percentChange = run1Value !== 0 ? ((delta / run1Value) * 100) : 0

      // Determine direction based on whether higher is better
      let direction: 'improvement' | 'regression' | 'neutral' = 'neutral'
      if (Math.abs(percentChange) > 1) { // Only consider significant changes
        if (config.higherIsBetter) {
          direction = delta > 0 ? 'improvement' : 'regression'
        } else {
          direction = delta < 0 ? 'improvement' : 'regression'
        }
      }

      // Determine significance
      const absPercentChange = Math.abs(percentChange)
      let significance: 'low' | 'medium' | 'high' = 'low'
      if (absPercentChange > 20) significance = 'high'
      else if (absPercentChange > 5) significance = 'medium'

      return {
        name: config.key,
        displayName: config.name,
        unit: config.unit,
        run1Value,
        run2Value,
        delta,
        percentChange,
        direction,
        significance,
        higherIsBetter: config.higherIsBetter,
      }
    })
  }, [run1Report, run2Report])

  // Sort metrics
  const sortedMetrics = useMemo(() => {
    return [...metricComparisons].sort((a, b) => {
      let comparison = 0
      
      switch (sortBy) {
        case 'name':
          comparison = a.displayName.localeCompare(b.displayName)
          break
        case 'percentChange':
          comparison = Math.abs(a.percentChange) - Math.abs(b.percentChange)
          break
        case 'significance':
          const significanceOrder = { high: 3, medium: 2, low: 1 }
          comparison = significanceOrder[a.significance] - significanceOrder[b.significance]
          break
      }
      
      return sortOrder === 'desc' ? -comparison : comparison
    })
  }, [metricComparisons, sortBy, sortOrder])

  const toggleSection = (sectionName: string) => {
    const newExpanded = new Set(expandedSections)
    if (newExpanded.has(sectionName)) {
      newExpanded.delete(sectionName)
    } else {
      newExpanded.add(sectionName)
    }
    setExpandedSections(newExpanded)
  }

  const formatValue = (value: number | null | undefined, unit?: string): string => {
    if (value === null || value === undefined) {
      return 'N/A'
    }
    if (unit === '%') {
      return `${value.toFixed(2)}%`
    } else if (unit === 'ms') {
      return formatLatencyValue(value)
    } else if (unit === 'req/s') {
      return `${value.toFixed(1)} req/s`
    } else {
      return value.toLocaleString()
    }
  }

  const formatDelta = (delta: number, percentChange: number, unit?: string): JSX.Element => {
    const isPositive = delta > 0
    const icon = Math.abs(percentChange) < 1 
      ? MinusIcon 
      : isPositive 
        ? ArrowTrendingUpIcon 
        : ArrowTrendingDownIcon

    const IconComponent = icon
    const deltaText = formatValue(Math.abs(delta), unit)
    const percentText = `${Math.abs(percentChange).toFixed(2)}%`

    return (
      <div className="flex items-center space-x-1">
        <IconComponent className="h-4 w-4" />
        <span>
          {deltaText} ({percentText})
        </span>
      </div>
    )
  }

  const getDirectionColor = (direction: MetricComparison['direction']): string => {
    switch (direction) {
      case 'improvement':
        return 'text-success-600'
      case 'regression':
        return 'text-danger-600'
      default:
        return 'text-gray-500'
    }
  }

  const getSignificanceIcon = (significance: MetricComparison['significance']): JSX.Element => {
    switch (significance) {
      case 'high':
        return <ExclamationTriangleIcon className="h-4 w-4 text-warning-500" />
      case 'medium':
        return <ExclamationTriangleIcon className="h-4 w-4 text-warning-400" />
      default:
        return <CheckCircleIcon className="h-4 w-4 text-gray-400" />
    }
  }

  const handleSort = (newSortBy: typeof sortBy) => {
    if (sortBy === newSortBy) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc')
    } else {
      setSortBy(newSortBy)
      setSortOrder('desc')
    }
  }

  const SectionHeader = ({ title, sectionKey, count }: { title: string; sectionKey: string; count?: number }) => {
    const isExpanded = expandedSections.has(sectionKey)
    return (
      <button
        onClick={() => toggleSection(sectionKey)}
        className="flex items-center justify-between w-full text-left p-4 hover:bg-gray-50 transition-colors"
      >
        <div className="flex items-center space-x-2">
          <h3 className="text-lg font-semibold text-gray-900">{title}</h3>
          {count !== undefined && (
            <span className="badge badge-info">{count}</span>
          )}
        </div>
        {isExpanded ? (
          <ChevronUpIcon className="h-5 w-5 text-gray-500" />
        ) : (
          <ChevronDownIcon className="h-5 w-5 text-gray-500" />
        )}
      </button>
    )
  }

  if (loading) {
    return (
      <div className="card">
        <div className="card-content">
          <LoadingSpinner size="lg" className="py-8" />
          <p className="text-center text-gray-500 mt-4">Loading comparison data...</p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="card">
        <div className="card-header">
          <div className="flex items-center justify-between">
            <h2 className="text-xl font-bold text-gray-900">Run Comparison</h2>
            {onExport && (
              <div className="flex items-center space-x-2">
                <span className="text-sm text-gray-500">Export:</span>
                <button
                  onClick={() => onExport('csv')}
                  className="btn btn-outline btn-sm"
                >
                  CSV
                </button>
                <button
                  onClick={() => onExport('json')}
                  className="btn btn-outline btn-sm"
                >
                  JSON
                </button>
                <button
                  onClick={() => onExport('pdf')}
                  className="btn btn-outline btn-sm"
                >
                  <ArrowDownTrayIcon className="h-4 w-4 mr-1" />
                  PDF
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Overview Section */}
      <div className="card">
        <SectionHeader title="Overview" sectionKey="overview" />
        {expandedSections.has('overview') && (
          <div className="card-content border-t border-gray-200">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              {/* Run 1 */}
              <div className="space-y-3">
                <h4 className="font-semibold text-gray-900 border-b pb-2">
                  Run 1: {run1.testName}
                </h4>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-gray-500">Timestamp:</span>
                    <span className="font-mono">
                      {new Date(run1.timestamp).toLocaleString()}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Git Branch:</span>
                    <span className="badge badge-info">{run1.gitBranch}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Git Commit:</span>
                    <span className="font-mono text-xs">
                      {run1.gitCommit.substring(0, 8)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Duration:</span>
                    <span>{run1.duration}</span>
                  </div>
                  {run1.isBaseline && (
                    <div className="flex justify-between">
                      <span className="text-gray-500">Baseline:</span>
                      <span className="badge badge-warning">{run1.baselineName}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* Run 2 */}
              <div className="space-y-3">
                <h4 className="font-semibold text-gray-900 border-b pb-2">
                  Run 2: {run2.testName}
                </h4>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-gray-500">Timestamp:</span>
                    <span className="font-mono">
                      {new Date(run2.timestamp).toLocaleString()}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Git Branch:</span>
                    <span className="badge badge-info">{run2.gitBranch}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Git Commit:</span>
                    <span className="font-mono text-xs">
                      {run2.gitCommit.substring(0, 8)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-500">Duration:</span>
                    <span>{run2.duration}</span>
                  </div>
                  {run2.isBaseline && (
                    <div className="flex justify-between">
                      <span className="text-gray-500">Baseline:</span>
                      <span className="badge badge-warning">{run2.baselineName}</span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Metrics Comparison */}
      <div className="card">
        <SectionHeader 
          title="Metrics Comparison" 
          sectionKey="metrics" 
          count={metricComparisons.length}
        />
        {expandedSections.has('metrics') && (
          <div className="border-t border-gray-200">
            {/* Sort Controls */}
            <div className="px-6 py-3 bg-gray-50 border-b border-gray-200">
              <div className="flex items-center space-x-4">
                <span className="text-sm font-medium text-gray-700">Sort by:</span>
                <button
                  onClick={() => handleSort('name')}
                  className={`text-sm px-2 py-1 rounded ${
                    sortBy === 'name' 
                      ? 'bg-primary-100 text-primary-700' 
                      : 'text-gray-500 hover:text-gray-700'
                  }`}
                >
                  Name {sortBy === 'name' && (sortOrder === 'asc' ? '↑' : '↓')}
                </button>
                <button
                  onClick={() => handleSort('percentChange')}
                  className={`text-sm px-2 py-1 rounded ${
                    sortBy === 'percentChange' 
                      ? 'bg-primary-100 text-primary-700' 
                      : 'text-gray-500 hover:text-gray-700'
                  }`}
                >
                  Change {sortBy === 'percentChange' && (sortOrder === 'asc' ? '↑' : '↓')}
                </button>
                <button
                  onClick={() => handleSort('significance')}
                  className={`text-sm px-2 py-1 rounded ${
                    sortBy === 'significance' 
                      ? 'bg-primary-100 text-primary-700' 
                      : 'text-gray-500 hover:text-gray-700'
                  }`}
                >
                  Significance {sortBy === 'significance' && (sortOrder === 'asc' ? '↑' : '↓')}
                </button>
              </div>
            </div>

            {/* Metrics Table */}
            <div className="overflow-x-auto">
              <table className="table">
                <thead className="table-header">
                  <tr>
                    <th className="table-header-cell">Metric</th>
                    <th className="table-header-cell">Run 1</th>
                    <th className="table-header-cell">Run 2</th>
                    <th className="table-header-cell">Delta</th>
                    <th className="table-header-cell">Impact</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {sortedMetrics.map((metric) => (
                    <tr key={metric.name} className="table-row">
                      <td className="table-cell">
                        <div className="flex items-center space-x-2">
                          {getSignificanceIcon(metric.significance)}
                          <span className="font-medium">{metric.displayName}</span>
                        </div>
                      </td>
                      <td className="table-cell font-mono">
                        {formatValue(metric.run1Value, metric.unit)}
                      </td>
                      <td className="table-cell font-mono">
                        {formatValue(metric.run2Value, metric.unit)}
                      </td>
                      <td className={`table-cell ${getDirectionColor(metric.direction)}`}>
                        {formatDelta(metric.delta, metric.percentChange, metric.unit)}
                      </td>
                      <td className="table-cell">
                        <span className={`badge ${
                          metric.direction === 'improvement' 
                            ? 'badge-success' 
                            : metric.direction === 'regression'
                              ? 'badge-danger'
                              : 'bg-gray-100 text-gray-800'
                        }`}>
                          {metric.direction === 'improvement' 
                            ? 'Improvement' 
                            : metric.direction === 'regression'
                              ? 'Regression'
                              : 'No Change'
                          }
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      {/* Client/Method Breakdown */}
      {run1Report && run2Report && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Client Results */}
          <div className="card">
            <SectionHeader 
              title="Client Comparison" 
              sectionKey="clients"
              count={Math.max(run1Report.clientResults?.length || 0, run2Report.clientResults?.length || 0)}
            />
            {expandedSections.has('clients') && (
              <div className="card-content border-t border-gray-200">
                <div className="space-y-4">
                  {(run1Report.clientResults || []).map((client1) => {
                    const client2 = run2Report.clientResults?.find(c => c.client === client1.client)
                    if (!client2) return null

                    const latencyDelta = client2.avgLatency - client1.avgLatency
                    const latencyPercent = client1.avgLatency !== 0 ? ((latencyDelta / client1.avgLatency) * 100) : 0

                    return (
                      <div key={client1.client} className="border rounded-lg p-4">
                        <h5 className="font-semibold mb-2">{client1.client}</h5>
                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <span className="text-gray-500">Avg Latency:</span>
                            <div className="font-mono">
                              {formatLatencyValue(client1.avgLatency)} → {formatLatencyValue(client2.avgLatency)}
                            </div>
                            <div className={`text-xs ${
                              latencyDelta < 0 ? 'text-success-600' : latencyDelta > 0 ? 'text-danger-600' : 'text-gray-500'
                            }`}>
                              {client1.avgLatency !== null && client1.avgLatency !== undefined && client2.avgLatency !== null && client2.avgLatency !== undefined 
                                ? `${latencyPercent > 0 ? '+' : ''}${latencyPercent.toFixed(1)}%`
                                : 'N/A'
                              }
                            </div>
                          </div>
                          <div>
                            <span className="text-gray-500">Success Rate:</span>
                            <div className="font-mono">
                              {client1.successRate.toFixed(1)}% → {client2.successRate.toFixed(1)}%
                            </div>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>

          {/* Method Results */}
          <div className="card">
            <SectionHeader 
              title="Method Comparison" 
              sectionKey="methods"
              count={Math.max(run1Report.methodResults?.length || 0, run2Report.methodResults?.length || 0)}
            />
            {expandedSections.has('methods') && (
              <div className="card-content border-t border-gray-200">
                <div className="space-y-4">
                  {(run1Report.methodResults || []).map((method1) => {
                    const method2 = run2Report.methodResults?.find(m => m.method === method1.method)
                    if (!method2) return null

                    const latencyDelta = method2.avgLatency - method1.avgLatency
                    const latencyPercent = method1.avgLatency !== 0 ? ((latencyDelta / method1.avgLatency) * 100) : 0

                    return (
                      <div key={method1.method} className="border rounded-lg p-4">
                        <h5 className="font-semibold mb-2">{method1.method}</h5>
                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <span className="text-gray-500">Avg Latency:</span>
                            <div className="font-mono">
                              {formatLatencyValue(method1.avgLatency)} → {formatLatencyValue(method2.avgLatency)}
                            </div>
                            <div className={`text-xs ${
                              latencyDelta < 0 ? 'text-success-600' : latencyDelta > 0 ? 'text-danger-600' : 'text-gray-500'
                            }`}>
                              {method1.avgLatency !== null && method1.avgLatency !== undefined && method2.avgLatency !== null && method2.avgLatency !== undefined 
                                ? `${latencyPercent > 0 ? '+' : ''}${latencyPercent.toFixed(1)}%`
                                : 'N/A'
                              }
                            </div>
                          </div>
                          <div>
                            <span className="text-gray-500">Requests:</span>
                            <div className="font-mono">
                              {method1.requests.toLocaleString()} → {method2.requests.toLocaleString()}
                            </div>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Summary */}
      <div className="card">
        <div className="card-content">
          <h3 className="text-lg font-semibold text-gray-900 mb-4">Comparison Summary</h3>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="text-center">
              <div className="text-2xl font-bold text-success-600">
                {metricComparisons.filter(m => m.direction === 'improvement').length}
              </div>
              <div className="text-sm text-gray-500">Improvements</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-danger-600">
                {metricComparisons.filter(m => m.direction === 'regression').length}
              </div>
              <div className="text-sm text-gray-500">Regressions</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-gray-600">
                {metricComparisons.filter(m => m.direction === 'neutral').length}
              </div>
              <div className="text-sm text-gray-500">No Change</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

