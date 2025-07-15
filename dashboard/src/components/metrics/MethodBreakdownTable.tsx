/**
 * @deprecated This component has been removed as part of the dashboard metrics update.
 * Method performance is now displayed as expandable rows within PerClientMetricsTable.
 * This file is kept for reference but should not be used.
 */

import React, { useState, useMemo, useCallback } from 'react'
import {
  FunnelIcon,
  XMarkIcon,
  ExclamationTriangleIcon,
  CheckCircleIcon,
  ClockIcon,
  BoltIcon,
  Cog6ToothIcon,
  InformationCircleIcon,
  EyeIcon,
  EyeSlashIcon,
} from '@heroicons/react/24/outline'
import { DetailedMetrics, MethodMetrics, ErrorBreakdown } from '../../types/detailed-metrics'
import { MetricTable, ColumnDef } from '../ui/MetricTable'
import ExportButton from '../ui/ExportButton'
import {
  formatLatency,
  formatLatencyValue,
  formatPercentage,
  formatRequestCount,
  formatErrorCount,
  formatTrend,
  getLatencyColor,
  getSuccessRateColor,
} from '../../utils/metric-formatters'

interface MethodBreakdownTableProps {
  data: DetailedMetrics
  clientFilter?: string
  groupByClient?: boolean
  showPercentiles?: boolean
  onMethodSelect?: (method: string, client?: string) => void
}

interface EnrichedMethodMetrics extends MethodMetrics {
  errorRate: number
  successRate: number
  totalErrors: number
  affectedClients: string[]
  responseSize?: number
  parameterCount: number
}

interface ClientMethodBreakdown {
  clientName: string
  requestCount: number
  errorCount: number
  errorRate: number
  successRate: number
  latencyPercentiles: {
    p50: number
    p95: number
    p99: number
  }
  performanceScore: number
  lastError?: ErrorBreakdown
}

export function MethodBreakdownTable({
  data,
  clientFilter,
  groupByClient = false,
  showPercentiles = true,
  onMethodSelect,
}: MethodBreakdownTableProps) {
  // Removed expandedMethods state - no longer needed
  const [showAdvancedMetrics, setShowAdvancedMetrics] = useState(false)
  const [sortConfig, setSortConfig] = useState<{ column: string; direction: 'asc' | 'desc' }>({
    column: 'performanceScore',
    direction: 'desc',
  })

  // Enrich method metrics with additional calculated fields
  const enrichedMethods = useMemo((): EnrichedMethodMetrics[] => {
    return data.methodMetrics.map(method => {
      const totalErrors = Object.values(method.errorsByClient).reduce(
        (sum, error) => sum + error.count,
        0
      )
      const errorRate = method.requestCount > 0 ? (totalErrors / method.requestCount) * 100 : 0
      const successRate = 100 - errorRate
      // Removed avgLatency - using percentiles instead
      
      const affectedClients = Object.keys(method.errorsByClient).filter(
        client => method.errorsByClient[client].count > 0
      )

      return {
        ...method,
        errorRate,
        successRate,
        // Removed avgLatency from return
        totalErrors,
        affectedClients,
        parameterCount: method.parameters.length,
      }
    })
  }, [data.methodMetrics])

  // Filter methods based on client filter
  const filteredMethods = useMemo(() => {
    if (!clientFilter) return enrichedMethods
    return enrichedMethods.filter(method => 
      Object.keys(method.errorsByClient).includes(clientFilter) ||
      data.clientMetrics.some(client => 
        client.clientName === clientFilter && 
        client.errorsByMethod[method.methodName]
      )
    )
  }, [enrichedMethods, clientFilter, data.clientMetrics])

  // Get client breakdown for a specific method
  const getClientBreakdown = useCallback((methodName: string): ClientMethodBreakdown[] => {
    const method = data.methodMetrics.find(m => m.methodName === methodName)
    if (!method) return []

    return data.clientMetrics.map(client => {
      const clientError = client.errorsByMethod[methodName]
      const methodError = method.errorsByClient[client.clientName]
      
      const errorCount = methodError?.count || 0
      const requestCount = Math.round(
        data.totalRequests * (client.totalRequests / data.totalRequests) * 
        (method.requestCount / data.totalRequests)
      )
      
      const errorRate = requestCount > 0 ? (errorCount / requestCount) * 100 : 0
      const successRate = 100 - errorRate

      return {
        clientName: client.clientName,
        requestCount,
        errorCount,
        errorRate,
        successRate,
        latencyPercentiles: {
          p50: client.latencyPercentiles.p50,
          p95: client.latencyPercentiles.p95,
          p99: client.latencyPercentiles.p99,
          // Removed mean - using percentiles only
        },
        performanceScore: client.performanceScore,
        lastError: methodError,
      }
    }).filter(breakdown => breakdown.requestCount > 0)
  }, [data])

  // Removed method expansion functionality

  // Handle method selection
  const handleMethodSelect = useCallback((methodName: string, clientName?: string) => {
    onMethodSelect?.(methodName, clientName)
  }, [onMethodSelect])


  // Define main table columns
  const columns: ColumnDef<EnrichedMethodMetrics>[] = [
    {
      id: 'methodName',
      header: 'Method',
      accessor: 'methodName',
      sortable: true,
      render: (value, row) => (
        <div className="flex items-center space-x-2">
          <button
            onClick={() => handleMethodSelect(row.methodName)}
            className="font-medium text-primary-600 hover:text-primary-700 text-left"
          >
            {value}
          </button>
          {row.affectedClients.length > 0 && (
            <ExclamationTriangleIcon 
              className="h-4 w-4 text-yellow-500" 
              title={`Errors in ${row.affectedClients.length} clients`}
            />
          )}
        </div>
      ),
    },
    {
      id: 'requestCount',
      header: 'Requests',
      accessor: 'requestCount',
      sortable: true,
      render: (value) => (
        <span className="font-mono text-sm">{formatRequestCount(value)}</span>
      ),
    },
    {
      id: 'successRate',
      header: 'Success Rate',
      accessor: 'successRate',
      sortable: true,
      render: (value) => (
        <span className={`font-medium ${getSuccessRateColor(value)}`}>
          {formatPercentage(value)}
        </span>
      ),
    },
  ]

  // Add percentile columns if enabled
  if (showPercentiles) {
    columns.push(
      {
        id: 'p50',
        header: 'P50',
        accessor: (row) => row.latencyPercentiles.p50,
        sortable: true,
        render: (value) => (
          <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400'}`}>
            {formatLatencyValue(value)}
          </span>
        ),
      },
      {
        id: 'p95',
        header: 'P95',
        accessor: (row) => row.latencyPercentiles.p95,
        sortable: true,
        render: (value) => (
          <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400'}`}>
            {formatLatencyValue(value)}
          </span>
        ),
      },
      {
        id: 'p99',
        header: 'P99',
        accessor: (row) => row.latencyPercentiles.p99,
        sortable: true,
        render: (value) => (
          <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400'}`}>
            {formatLatencyValue(value)}
          </span>
        ),
      }
    )
  }

  // Add performance score column
  columns.push(
    {
      id: 'performanceScore',
      header: 'Score',
      accessor: 'performanceScore',
      sortable: true,
      render: (value) => {
        // const { value: formattedScore, color } = formatPerformanceScore(value)
        const color = value >= 90 ? 'text-green-600' : value >= 70 ? 'text-yellow-600' : 'text-red-600'
        return (
          <div className="flex items-center space-x-2">
            <span className={`font-medium ${color}`}>
              {Math.round(value)}
            </span>
            <div className="w-12 h-2 bg-gray-200 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full ${
                  value >= 90 ? 'bg-green-500' :
                  value >= 80 ? 'bg-yellow-500' :
                  value >= 70 ? 'bg-orange-500' : 'bg-red-500'
                }`}
                style={{ width: `${Math.min(100, value)}%` }}
              />
            </div>
          </div>
        )
      },
    }
  )

  // Add trend indicator if advanced metrics are shown
  if (showAdvancedMetrics) {
    columns.push({
      id: 'trend',
      header: 'Trend',
      accessor: 'trendIndicator',
      sortable: false,
      render: (value) => {
        if (!value) return <span className="text-gray-400">-</span>
        const trend = formatTrend(value.direction, value.confidence)
        return (
          <div className="flex items-center space-x-1">
            <span className={`text-lg ${trend.color}`}>
              {trend.symbol}
            </span>
            <span className="text-xs text-gray-500">
              {Math.round(value.confidence)}%
            </span>
          </div>
        )
      },
    })
  }

  // Prepare export data
  const exportData = useMemo(() => {
    return filteredMethods.map(method => ({
      method: method.methodName,
      requests: method.requestCount,
      successRate: `${method.successRate.toFixed(2)}%`,
      errorRate: `${method.errorRate.toFixed(2)}%`,
      // Removed avgLatency from export
      p50: `${method.latencyPercentiles.p50.toFixed(2)}ms`,
      p95: `${method.latencyPercentiles.p95.toFixed(2)}ms`,
      p99: `${method.latencyPercentiles.p99.toFixed(2)}ms`,
      performanceScore: method.performanceScore,
      parameterCount: method.parameterCount,
      totalErrors: method.totalErrors,
      affectedClients: method.affectedClients.join(', '),
      trend: method.trendIndicator?.direction || 'stable',
      trendConfidence: method.trendIndicator?.confidence || 0,
    }))
  }, [filteredMethods])

  return (
    <div className="space-y-6">
      {/* Header with controls */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-semibold text-gray-900">
            Method Performance Breakdown
          </h3>
          <p className="text-sm text-gray-600 mt-1">
            Detailed analysis of {filteredMethods.length} methods
            {clientFilter && ` (filtered by ${clientFilter})`}
          </p>
        </div>
        
        <div className="flex items-center space-x-3">
          {/* Toggle percentiles */}
          <button
            onClick={() => setShowAdvancedMetrics(!showAdvancedMetrics)}
            className={`btn btn-outline btn-sm ${showAdvancedMetrics ? 'bg-primary-50' : ''}`}
          >
            {showAdvancedMetrics ? (
              <>
                <EyeSlashIcon className="h-4 w-4 mr-2" />
                Hide Advanced
              </>
            ) : (
              <>
                <EyeIcon className="h-4 w-4 mr-2" />
                Show Advanced
              </>
            )}
          </button>

          {/* Export button */}
          <ExportButton
            data={exportData}
            filename={`method_breakdown_${new Date().toISOString().split('T')[0]}`}
            formats={['csv', 'json', 'xlsx']}
          />
        </div>
      </div>

      {/* Summary stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="bg-white p-4 rounded-lg border border-gray-200">
          <div className="flex items-center">
            <Cog6ToothIcon className="h-5 w-5 text-gray-400 mr-2" />
            <span className="text-sm font-medium text-gray-600">Total Methods</span>
          </div>
          <p className="text-2xl font-bold text-gray-900 mt-1">
            {filteredMethods.length}
          </p>
        </div>
        
        <div className="bg-white p-4 rounded-lg border border-gray-200">
          <div className="flex items-center">
            <BoltIcon className="h-5 w-5 text-green-500 mr-2" />
            <span className="text-sm font-medium text-gray-600">Avg Performance</span>
          </div>
          <p className="text-2xl font-bold text-gray-900 mt-1">
            {Math.round(
              filteredMethods.reduce((sum, m) => sum + m.performanceScore, 0) / 
              filteredMethods.length
            )}
          </p>
        </div>
        
        <div className="bg-white p-4 rounded-lg border border-gray-200">
          <div className="flex items-center">
            <ExclamationTriangleIcon className="h-5 w-5 text-yellow-500 mr-2" />
            <span className="text-sm font-medium text-gray-600">Methods with Errors</span>
          </div>
          <p className="text-2xl font-bold text-gray-900 mt-1">
            {filteredMethods.filter(m => m.totalErrors > 0).length}
          </p>
        </div>
        
        <div className="bg-white p-4 rounded-lg border border-gray-200">
          <div className="flex items-center">
            <ClockIcon className="h-5 w-5 text-blue-500 mr-2" />
            <span className="text-sm font-medium text-gray-600">Median Latency</span>
          </div>
          <p className="text-2xl font-bold text-gray-900 mt-1">
            {filteredMethods.length > 0 
              ? formatLatencyValue(
                  filteredMethods.reduce((sum, m) => sum + m.latencyPercentiles.p50, 0) / 
                  filteredMethods.length
                )
              : 'N/A'
            }
          </p>
        </div>
      </div>

      {/* Main table */}
      <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
        <MetricTable
          data={filteredMethods}
          columns={columns}
          onRowClick={(method) => handleMethodSelect(method.methodName)}
          className="border-none"
          striped={true}
          sortable={true}
          filterable={true}
          title=""
          emptyMessage="No methods found matching the current filters"
        />

        {/* Removed expandable client breakdown section */}
      </div>

      {/* Legend */}
      <div className="bg-gray-50 rounded-lg p-4">
        <h4 className="text-sm font-medium text-gray-700 mb-2">Legend</h4>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 text-xs">
          <div>
            <span className="font-medium">Performance Score:</span>
            <div className="mt-1 space-y-1">
              <div className="flex items-center">
                <div className="w-3 h-3 bg-green-500 rounded mr-2"></div>
                <span>90+ Excellent</span>
              </div>
              <div className="flex items-center">
                <div className="w-3 h-3 bg-yellow-500 rounded mr-2"></div>
                <span>80-89 Good</span>
              </div>
              <div className="flex items-center">
                <div className="w-3 h-3 bg-orange-500 rounded mr-2"></div>
                <span>70-79 Fair</span>
              </div>
              <div className="flex items-center">
                <div className="w-3 h-3 bg-red-500 rounded mr-2"></div>
                <span>&lt;70 Poor</span>
              </div>
            </div>
          </div>
          
          <div>
            <span className="font-medium">Success Rate:</span>
            <div className="mt-1 space-y-1">
              <div className="flex items-center">
                <span className="text-green-600 mr-2">●</span>
                <span>99.9%+ Excellent</span>
              </div>
              <div className="flex items-center">
                <span className="text-yellow-600 mr-2">●</span>
                <span>95-99% Good</span>
              </div>
              <div className="flex items-center">
                <span className="text-red-600 mr-2">●</span>
                <span>&lt;95% Poor</span>
              </div>
            </div>
          </div>
          
          {showAdvancedMetrics && (
            <div>
              <span className="font-medium">Trends:</span>
              <div className="mt-1 space-y-1">
                <div className="flex items-center">
                  <span className="text-green-600 mr-2">↗</span>
                  <span>Improving</span>
                </div>
                <div className="flex items-center">
                  <span className="text-gray-600 mr-2">→</span>
                  <span>Stable</span>
                </div>
                <div className="flex items-center">
                  <span className="text-red-600 mr-2">↘</span>
                  <span>Degrading</span>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default MethodBreakdownTable