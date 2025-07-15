import React, { useState, useMemo, useCallback } from 'react'
import {
  ExclamationTriangleIcon,
  XCircleIcon,
  FireIcon,
  InformationCircleIcon,
  ChevronRightIcon,
  ChevronDownIcon,
  CalendarDaysIcon,
  ChartBarIcon,
  DocumentArrowDownIcon,
  ArrowPathIcon,
  FunnelIcon,
  EyeIcon,
  MagnifyingGlassIcon,
} from '@heroicons/react/24/outline'
import { DetailedMetrics, ErrorAnalysis, ErrorSeverity, ErrorImpact, ErrorBreakdown, ErrorTrend } from '../../types/detailed-metrics'
import { ErrorRateChart } from '../ErrorRateChart'
import { MetricTable, ColumnDef } from '../ui/MetricTable'
import LoadingSpinner from '../LoadingSpinner'
import { ExportButton } from '../ui'

export interface ErrorAnalysisPanelProps {
  data: DetailedMetrics
  showTrends?: boolean
  groupBy?: 'type' | 'client' | 'method'
  onErrorSelect?: (error: string) => void
}

interface ErrorSummaryCard {
  title: string
  value: number | string
  severity: ErrorSeverity
  trend?: 'up' | 'down' | 'stable'
  trendValue?: number
  icon: React.ComponentType<{ className?: string }>
  description?: string
}

interface ErrorTableRow {
  id: string
  errorType: string
  client: string
  method: string
  count: number
  percentage: number
  severity: ErrorSeverity
  impact: ErrorImpact
  lastOccurrence: string
  firstOccurrence: string
  frequency: number
  resolution: string
  httpStatusCode?: number
  errorMessage: string
  category: string
  affectedRequests: number
  affectedClients: number
  correlatedErrors: string[]
  timeToResolve?: number
}

interface ErrorCorrelation {
  errorA: string
  errorB: string
  correlation: number
  cooccurrences: number
  strength: 'weak' | 'moderate' | 'strong'
}

interface TimeBasedErrorAnalysis {
  recent: ErrorBreakdown[]
  historical: ErrorBreakdown[]
  trend: 'improving' | 'degrading' | 'stable'
  changeRate: number
}

export function ErrorAnalysisPanel({
  data,
  showTrends = true,
  groupBy = 'type',
  onErrorSelect
}: ErrorAnalysisPanelProps) {
  const [selectedTimeRange, setSelectedTimeRange] = useState<'1h' | '24h' | '7d' | '30d'>('24h')
  const [selectedSeverity, setSelectedSeverity] = useState<ErrorSeverity | 'all'>('all')
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['summary', 'breakdown']))
  const [selectedError, setSelectedError] = useState<string | null>(null)
  const [correlationThreshold, setCorrelationThreshold] = useState(0.5)

  // Calculate error summary cards
  const errorSummaryCards = useMemo((): ErrorSummaryCard[] => {
    const errorAnalysis = data.errorAnalysis
    const totalRequests = data.totalRequests || 0
    const errorRate = totalRequests > 0 ? errorAnalysis.totalErrors / totalRequests : 0

    // Calculate critical errors
    const criticalErrors = Object.values(errorAnalysis.errorsByType)
      .filter(error => error.severity === ErrorSeverity.CRITICAL)
      .reduce((sum, error) => sum + error.count, 0)

    // Calculate error types
    const errorTypes = Object.keys(errorAnalysis.errorsByType).length

    // Calculate trend for error rate
    const recentTrend = errorAnalysis.errorTrends.length > 0 
      ? errorAnalysis.errorTrends[errorAnalysis.errorTrends.length - 1]
      : null

    return [
      {
        title: 'Total Errors',
        value: errorAnalysis.totalErrors.toLocaleString(),
        severity: errorAnalysis.totalErrors > 1000 ? ErrorSeverity.HIGH : 
                 errorAnalysis.totalErrors > 100 ? ErrorSeverity.MEDIUM : ErrorSeverity.LOW,
        trend: recentTrend?.trend === 'increasing' ? 'up' : 
               recentTrend?.trend === 'decreasing' ? 'down' : 'stable',
        trendValue: recentTrend?.changeRate || 0,
        icon: XCircleIcon,
        description: 'Total number of errors across all clients and methods'
      },
      {
        title: 'Error Rate',
        value: `${(errorRate * 100).toFixed(2)}%`,
        severity: errorRate > 0.1 ? ErrorSeverity.CRITICAL : 
                 errorRate > 0.05 ? ErrorSeverity.HIGH : 
                 errorRate > 0.01 ? ErrorSeverity.MEDIUM : ErrorSeverity.LOW,
        trend: recentTrend?.trend === 'increasing' ? 'up' : 
               recentTrend?.trend === 'decreasing' ? 'down' : 'stable',
        trendValue: recentTrend?.changeRate || 0,
        icon: ChartBarIcon,
        description: 'Percentage of requests that resulted in errors'
      },
      {
        title: 'Critical Errors',
        value: criticalErrors.toLocaleString(),
        severity: criticalErrors > 0 ? ErrorSeverity.CRITICAL : ErrorSeverity.LOW,
        trend: 'stable',
        trendValue: 0,
        icon: FireIcon,
        description: 'High-severity errors requiring immediate attention'
      },
      {
        title: 'Error Types',
        value: errorTypes.toString(),
        severity: errorTypes > 10 ? ErrorSeverity.HIGH : 
                 errorTypes > 5 ? ErrorSeverity.MEDIUM : ErrorSeverity.LOW,
        trend: 'stable',
        trendValue: 0,
        icon: ExclamationTriangleIcon,
        description: 'Number of distinct error types encountered'
      }
    ]
  }, [data])

  // Calculate error breakdown data for table
  const errorBreakdownData = useMemo((): ErrorTableRow[] => {
    const errorAnalysis = data.errorAnalysis
    const rows: ErrorTableRow[] = []

    // Group errors by the selected groupBy option
    let groupedErrors: Record<string, ErrorBreakdown[]> = {}

    switch (groupBy) {
      case 'type':
        groupedErrors = Object.entries(errorAnalysis.errorsByType).reduce((acc, [type, error]) => {
          acc[type] = [error]
          return acc
        }, {} as Record<string, ErrorBreakdown[]>)
        break
      case 'client':
        groupedErrors = errorAnalysis.errorsByClient
        break
      case 'method':
        groupedErrors = errorAnalysis.errorsByMethod
        break
    }

    Object.entries(groupedErrors).forEach(([groupKey, errors]) => {
      if (Array.isArray(errors)) {
        errors.forEach((error, index) => {
          rows.push({
            id: `${groupKey}-${index}`,
            errorType: groupBy === 'type' ? groupKey : error.category || 'Unknown',
            client: groupBy === 'client' ? groupKey : 'Multiple',
            method: groupBy === 'method' ? groupKey : 'Multiple',
            count: error.count,
            percentage: error.percentage,
            severity: error.severity,
            impact: error.impact,
            lastOccurrence: error.lastOccurrence,
            firstOccurrence: error.firstOccurrence,
            frequency: error.frequency,
            resolution: error.resolution,
            httpStatusCode: error.httpStatusCode,
            errorMessage: error.errorMessage,
            category: error.category,
            affectedRequests: error.count,
            affectedClients: groupBy === 'client' ? 1 : Object.keys(errorAnalysis.errorsByClient).length,
            correlatedErrors: [],
            timeToResolve: Math.random() * 60 // Mock data - in real implementation would come from metrics
          })
        })
      } else {
        rows.push({
          id: groupKey,
          errorType: groupBy === 'type' ? groupKey : errors.category || 'Unknown',
          client: groupBy === 'client' ? groupKey : 'Multiple',
          method: groupBy === 'method' ? groupKey : 'Multiple',
          count: errors.count,
          percentage: errors.percentage,
          severity: errors.severity,
          impact: errors.impact,
          lastOccurrence: errors.lastOccurrence,
          firstOccurrence: errors.firstOccurrence,
          frequency: errors.frequency,
          resolution: errors.resolution,
          httpStatusCode: errors.httpStatusCode,
          errorMessage: errors.errorMessage,
          category: errors.category,
          affectedRequests: errors.count,
          affectedClients: groupBy === 'client' ? 1 : Object.keys(errorAnalysis.errorsByClient).length,
          correlatedErrors: [],
          timeToResolve: Math.random() * 60 // Mock data
        })
      }
    })

    return rows.sort((a, b) => b.count - a.count)
  }, [data, groupBy])

  // Calculate error correlations
  const errorCorrelations = useMemo((): ErrorCorrelation[] => {
    const correlations: ErrorCorrelation[] = []
    const errorTypes = Object.keys(data.errorAnalysis.errorsByType)

    // Calculate correlation between different error types
    for (let i = 0; i < errorTypes.length; i++) {
      for (let j = i + 1; j < errorTypes.length; j++) {
        const errorA = errorTypes[i]
        const errorB = errorTypes[j]
        
        // Mock correlation calculation - in real implementation would analyze co-occurrence
        const correlation = Math.random() * 0.8 + 0.2
        const cooccurrences = Math.floor(Math.random() * 50) + 1
        
        const strength = correlation > 0.7 ? 'strong' : 
                        correlation > 0.5 ? 'moderate' : 'weak'

        if (correlation >= correlationThreshold) {
          correlations.push({
            errorA,
            errorB,
            correlation,
            cooccurrences,
            strength
          })
        }
      }
    }

    return correlations.sort((a, b) => b.correlation - a.correlation)
  }, [data, correlationThreshold])

  // Calculate time-based error analysis
  const timeBasedAnalysis = useMemo((): TimeBasedErrorAnalysis => {
    const errorAnalysis = data.errorAnalysis
    const now = new Date()
    const cutoffTime = new Date(now.getTime() - 24 * 60 * 60 * 1000) // 24 hours ago

    const recent: ErrorBreakdown[] = []
    const historical: ErrorBreakdown[] = []

    Object.values(errorAnalysis.errorsByType).forEach(error => {
      const lastOccurrence = new Date(error.lastOccurrence)
      if (lastOccurrence > cutoffTime) {
        recent.push(error)
      } else {
        historical.push(error)
      }
    })

    const recentTotal = recent.reduce((sum, error) => sum + error.count, 0)
    const historicalTotal = historical.reduce((sum, error) => sum + error.count, 0)
    
    const changeRate = historicalTotal > 0 ? (recentTotal - historicalTotal) / historicalTotal : 0
    const trend = changeRate > 0.1 ? 'degrading' : changeRate < -0.1 ? 'improving' : 'stable'

    return {
      recent,
      historical,
      trend,
      changeRate
    }
  }, [data])

  // Prepare chart data for ErrorRateChart
  const chartData = useMemo(() => {
    return data.errorAnalysis.errorTrends.map(trend => ({
      timestamp: trend.timestamp,
      errorRate: trend.errorRate,
      totalErrors: trend.errorCount,
      totalRequests: Math.floor(trend.errorCount / (trend.errorRate || 0.001)), // Approximate
      runId: data.runId,
      metadata: {
        client: trend.client,
        method: trend.method,
        errorType: trend.errorType
      }
    }))
  }, [data])

  // Column definitions for error breakdown table
  const columns: ColumnDef<ErrorTableRow>[] = [
    {
      id: 'severity',
      header: 'Severity',
      accessor: 'severity',
      width: '80px',
      render: (severity: ErrorSeverity) => (
        <div className="flex items-center">
          <div className={`w-3 h-3 rounded-full mr-2 ${getSeverityColor(severity)}`} />
          <span className={`text-sm font-medium ${getSeverityTextColor(severity)}`}>
            {severity.toUpperCase()}
          </span>
        </div>
      )
    },
    {
      id: 'errorType',
      header: 'Error Type',
      accessor: 'errorType',
      width: '200px',
      render: (errorType: string, row: ErrorTableRow) => (
        <div className="flex flex-col">
          <div className="font-medium text-gray-900">{errorType}</div>
          <div className="text-sm text-gray-500 truncate max-w-48">{row.errorMessage}</div>
        </div>
      )
    },
    {
      id: 'count',
      header: 'Count',
      accessor: 'count',
      width: '80px',
      render: (count: number) => (
        <span className="font-mono text-sm font-medium">{count.toLocaleString()}</span>
      )
    },
    {
      id: 'percentage',
      header: 'Percentage',
      accessor: 'percentage',
      width: '100px',
      render: (percentage: number) => (
        <span className="font-mono text-sm">{percentage.toFixed(2)}%</span>
      )
    },
    {
      id: 'impact',
      header: 'Impact',
      accessor: 'impact',
      width: '100px',
      render: (impact: ErrorImpact) => (
        <span className={`px-2 py-1 text-xs rounded-full ${getImpactColor(impact)}`}>
          {impact.toUpperCase()}
        </span>
      )
    },
    {
      id: 'affectedRequests',
      header: 'Affected Requests',
      accessor: 'affectedRequests',
      width: '120px',
      render: (requests: number) => (
        <span className="font-mono text-sm">{requests.toLocaleString()}</span>
      )
    },
    {
      id: 'frequency',
      header: 'Frequency',
      accessor: 'frequency',
      width: '100px',
      render: (frequency: number) => (
        <span className="font-mono text-sm">{frequency.toFixed(2)}/min</span>
      )
    },
    {
      id: 'lastOccurrence',
      header: 'Last Occurrence',
      accessor: 'lastOccurrence',
      width: '150px',
      render: (lastOccurrence: string) => (
        <span className="text-sm">{formatTimestamp(lastOccurrence)}</span>
      )
    },
    {
      id: 'actions',
      header: 'Actions',
      accessor: 'id',
      width: '100px',
      render: (id: string, row: ErrorTableRow) => (
        <div className="flex items-center space-x-2">
          <button
            onClick={() => handleErrorSelect(row.errorType)}
            className="btn btn-ghost btn-sm"
            title="View details"
          >
            <EyeIcon className="h-4 w-4" />
          </button>
          <button
            onClick={() => handleErrorDrillDown(row)}
            className="btn btn-ghost btn-sm"
            title="Drill down"
          >
            <MagnifyingGlassIcon className="h-4 w-4" />
          </button>
        </div>
      )
    }
  ]

  // Helper functions
  const getSeverityColor = (severity: ErrorSeverity): string => {
    switch (severity) {
      case ErrorSeverity.CRITICAL: return 'bg-red-500'
      case ErrorSeverity.HIGH: return 'bg-orange-500'
      case ErrorSeverity.MEDIUM: return 'bg-yellow-500'
      case ErrorSeverity.LOW: return 'bg-blue-500'
      default: return 'bg-gray-500'
    }
  }

  const getSeverityTextColor = (severity: ErrorSeverity): string => {
    switch (severity) {
      case ErrorSeverity.CRITICAL: return 'text-red-700'
      case ErrorSeverity.HIGH: return 'text-orange-700'
      case ErrorSeverity.MEDIUM: return 'text-yellow-700'
      case ErrorSeverity.LOW: return 'text-blue-700'
      default: return 'text-gray-700'
    }
  }

  const getImpactColor = (impact: ErrorImpact): string => {
    switch (impact) {
      case ErrorImpact.SEVERE: return 'bg-red-100 text-red-800'
      case ErrorImpact.SIGNIFICANT: return 'bg-orange-100 text-orange-800'
      case ErrorImpact.MODERATE: return 'bg-yellow-100 text-yellow-800'
      case ErrorImpact.MINIMAL: return 'bg-green-100 text-green-800'
      default: return 'bg-gray-100 text-gray-800'
    }
  }

  const formatTimestamp = (timestamp: string): string => {
    return new Date(timestamp).toLocaleString()
  }

  const getTrendIcon = (trend: string) => {
    switch (trend) {
      case 'up': return '↗'
      case 'down': return '↘'
      default: return '→'
    }
  }

  const getTrendColor = (trend: string) => {
    switch (trend) {
      case 'up': return 'text-red-600'
      case 'down': return 'text-green-600'
      default: return 'text-gray-600'
    }
  }

  // Event handlers
  const handleErrorSelect = useCallback((errorType: string) => {
    setSelectedError(errorType)
    onErrorSelect?.(errorType)
  }, [onErrorSelect])

  const handleErrorDrillDown = useCallback((row: ErrorTableRow) => {
    // In a real implementation, this would open a detailed view
    console.log('Drilling down into error:', row)
  }, [])

  const toggleSection = useCallback((section: string) => {
    setExpandedSections(prev => {
      const newSet = new Set(prev)
      if (newSet.has(section)) {
        newSet.delete(section)
      } else {
        newSet.add(section)
      }
      return newSet
    })
  }, [])

  const handleExportErrorReport = useCallback(() => {
    const reportData = {
      summary: errorSummaryCards,
      breakdown: errorBreakdownData,
      correlations: errorCorrelations,
      timeBasedAnalysis,
      timestamp: new Date().toISOString(),
      runId: data.runId
    }

    const blob = new Blob([JSON.stringify(reportData, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `error_analysis_report_${data.runId}_${new Date().toISOString().split('T')[0]}.json`
    link.click()
    URL.revokeObjectURL(url)
  }, [errorSummaryCards, errorBreakdownData, errorCorrelations, timeBasedAnalysis, data.runId])

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Error Analysis</h2>
          <p className="text-sm text-gray-600 mt-1">
            Comprehensive error analysis and breakdown for run {data.runId}
          </p>
        </div>
        <div className="flex items-center space-x-3">
          <select
            value={groupBy}
            onChange={(e) => setGroupBy(e.target.value as 'type' | 'client' | 'method')}
            className="select select-bordered select-sm"
          >
            <option value="type">Group by Type</option>
            <option value="client">Group by Client</option>
            <option value="method">Group by Method</option>
          </select>
          <select
            value={selectedSeverity}
            onChange={(e) => setSelectedSeverity(e.target.value as ErrorSeverity | 'all')}
            className="select select-bordered select-sm"
          >
            <option value="all">All Severities</option>
            <option value={ErrorSeverity.CRITICAL}>Critical</option>
            <option value={ErrorSeverity.HIGH}>High</option>
            <option value={ErrorSeverity.MEDIUM}>Medium</option>
            <option value={ErrorSeverity.LOW}>Low</option>
          </select>
          <button
            onClick={handleExportErrorReport}
            className="btn btn-primary btn-sm"
          >
            <DocumentArrowDownIcon className="h-4 w-4 mr-2" />
            Export Report
          </button>
        </div>
      </div>

      {/* Error Summary Cards */}
      <div className="card">
        <div className="card-header">
          <button
            onClick={() => toggleSection('summary')}
            className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
          >
            {expandedSections.has('summary') ? (
              <ChevronDownIcon className="h-5 w-5 mr-2" />
            ) : (
              <ChevronRightIcon className="h-5 w-5 mr-2" />
            )}
            Error Summary
          </button>
        </div>
        
        {expandedSections.has('summary') && (
          <div className="card-content">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {errorSummaryCards.map((card, index) => (
                <div key={index} className="card bg-gradient-to-br from-white to-gray-50 border-l-4 border-l-red-500">
                  <div className="card-content">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center">
                        <div className={`p-2 rounded-lg ${getSeverityColor(card.severity)} bg-opacity-10`}>
                          <card.icon className={`h-6 w-6 ${getSeverityTextColor(card.severity)}`} />
                        </div>
                        <div className="ml-4">
                          <p className="text-sm font-medium text-gray-600">{card.title}</p>
                          <p className="text-2xl font-bold text-gray-900">{card.value}</p>
                        </div>
                      </div>
                      {showTrends && card.trend && (
                        <div className={`text-sm ${getTrendColor(card.trend)}`}>
                          <span className="font-medium">
                            {getTrendIcon(card.trend)} {Math.abs(card.trendValue || 0).toFixed(1)}%
                          </span>
                        </div>
                      )}
                    </div>
                    {card.description && (
                      <p className="text-xs text-gray-500 mt-2">{card.description}</p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Error Trends Chart */}
      {showTrends && (
        <div className="card">
          <div className="card-header">
            <button
              onClick={() => toggleSection('trends')}
              className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
            >
              {expandedSections.has('trends') ? (
                <ChevronDownIcon className="h-5 w-5 mr-2" />
              ) : (
                <ChevronRightIcon className="h-5 w-5 mr-2" />
              )}
              Error Trends
            </button>
          </div>
          
          {expandedSections.has('trends') && (
            <div className="card-content">
              <ErrorRateChart
                data={chartData}
                title="Error Rate Over Time"
                height={400}
                showFill={true}
                showThreshold={true}
                errorThreshold={0.05}
                onPointClick={(point) => {
                  console.log('Error rate point clicked:', point)
                }}
                groupBy={groupBy === 'type' ? 'errorType' : groupBy}
                showErrorTypes={true}
              />
            </div>
          )}
        </div>
      )}

      {/* Error Breakdown Table */}
      <div className="card">
        <div className="card-header">
          <button
            onClick={() => toggleSection('breakdown')}
            className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
          >
            {expandedSections.has('breakdown') ? (
              <ChevronDownIcon className="h-5 w-5 mr-2" />
            ) : (
              <ChevronRightIcon className="h-5 w-5 mr-2" />
            )}
            Error Breakdown
          </button>
        </div>
        
        {expandedSections.has('breakdown') && (
          <div className="card-content">
            <MetricTable
              data={errorBreakdownData.filter(row => 
                selectedSeverity === 'all' || row.severity === selectedSeverity
              )}
              columns={columns}
              title={`Errors grouped by ${groupBy}`}
              subtitle={`${errorBreakdownData.length} error types found`}
              sortable={true}
              filterable={true}
              exportable={true}
              pageSize={20}
              maxHeight="600px"
              onRowClick={(row) => handleErrorSelect(row.errorType)}
              striped={true}
              bordered={true}
            />
          </div>
        )}
      </div>

      {/* Error Correlations */}
      <div className="card">
        <div className="card-header">
          <button
            onClick={() => toggleSection('correlations')}
            className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
          >
            {expandedSections.has('correlations') ? (
              <ChevronDownIcon className="h-5 w-5 mr-2" />
            ) : (
              <ChevronRightIcon className="h-5 w-5 mr-2" />
            )}
            Error Correlations
          </button>
        </div>
        
        {expandedSections.has('correlations') && (
          <div className="card-content">
            <div className="mb-4">
              <div className="flex items-center space-x-4">
                <label className="text-sm font-medium text-gray-700">
                  Correlation Threshold:
                </label>
                <input
                  type="range"
                  min="0.1"
                  max="1.0"
                  step="0.1"
                  value={correlationThreshold}
                  onChange={(e) => setCorrelationThreshold(parseFloat(e.target.value))}
                  className="range range-primary"
                />
                <span className="text-sm text-gray-600">{correlationThreshold.toFixed(1)}</span>
              </div>
            </div>
            
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {errorCorrelations.map((correlation, index) => (
                <div key={index} className="card bg-gray-50 border">
                  <div className="card-content">
                    <div className="flex items-center justify-between mb-2">
                      <span className={`px-2 py-1 text-xs rounded-full ${
                        correlation.strength === 'strong' ? 'bg-red-100 text-red-800' :
                        correlation.strength === 'moderate' ? 'bg-yellow-100 text-yellow-800' :
                        'bg-green-100 text-green-800'
                      }`}>
                        {correlation.strength.toUpperCase()}
                      </span>
                      <span className="text-sm font-mono text-gray-600">
                        {correlation.correlation.toFixed(2)}
                      </span>
                    </div>
                    <div className="text-sm text-gray-900">
                      <div className="font-medium">{correlation.errorA}</div>
                      <div className="text-gray-500">correlates with</div>
                      <div className="font-medium">{correlation.errorB}</div>
                    </div>
                    <div className="text-xs text-gray-500 mt-2">
                      {correlation.cooccurrences} co-occurrences
                    </div>
                  </div>
                </div>
              ))}
            </div>
            
            {errorCorrelations.length === 0 && (
              <div className="text-center py-8 text-gray-500">
                No error correlations found above threshold {correlationThreshold.toFixed(1)}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Time-Based Analysis */}
      <div className="card">
        <div className="card-header">
          <button
            onClick={() => toggleSection('timeAnalysis')}
            className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
          >
            {expandedSections.has('timeAnalysis') ? (
              <ChevronDownIcon className="h-5 w-5 mr-2" />
            ) : (
              <ChevronRightIcon className="h-5 w-5 mr-2" />
            )}
            Time-Based Analysis
          </button>
        </div>
        
        {expandedSections.has('timeAnalysis') && (
          <div className="card-content">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {timeBasedAnalysis.recent.length}
                </div>
                <div className="text-sm text-gray-500">Recent Errors (24h)</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {timeBasedAnalysis.historical.length}
                </div>
                <div className="text-sm text-gray-500">Historical Errors</div>
              </div>
              <div className="text-center">
                <div className={`text-2xl font-bold ${
                  timeBasedAnalysis.trend === 'improving' ? 'text-green-600' :
                  timeBasedAnalysis.trend === 'degrading' ? 'text-red-600' :
                  'text-yellow-600'
                }`}>
                  {timeBasedAnalysis.trend.toUpperCase()}
                </div>
                <div className="text-sm text-gray-500">
                  Trend ({(timeBasedAnalysis.changeRate * 100).toFixed(1)}%)
                </div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Error Impact Analysis */}
      <div className="card">
        <div className="card-header">
          <button
            onClick={() => toggleSection('impact')}
            className="flex items-center text-lg font-semibold text-gray-900 hover:text-gray-700"
          >
            {expandedSections.has('impact') ? (
              <ChevronDownIcon className="h-5 w-5 mr-2" />
            ) : (
              <ChevronRightIcon className="h-5 w-5 mr-2" />
            )}
            Impact Analysis
          </button>
        </div>
        
        {expandedSections.has('impact') && (
          <div className="card-content">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {data.errorAnalysis.impactAnalysis.affectedUsers.toLocaleString()}
                </div>
                <div className="text-sm text-gray-500">Affected Users</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {data.errorAnalysis.impactAnalysis.affectedRequests.toLocaleString()}
                </div>
                <div className="text-sm text-gray-500">Affected Requests</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {data.errorAnalysis.impactAnalysis.downtimeMinutes.toFixed(1)}
                </div>
                <div className="text-sm text-gray-500">Downtime (min)</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold text-gray-900">
                  {(data.errorAnalysis.impactAnalysis.systemImpact * 100).toFixed(1)}%
                </div>
                <div className="text-sm text-gray-500">System Impact</div>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default ErrorAnalysisPanel