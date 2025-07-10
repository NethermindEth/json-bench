import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { DetailedMetrics, ClientMetrics, MethodMetrics } from '../../types/detailed-metrics'
import { MetricTable, ColumnDef } from '../ui/MetricTable'
import { formatLatency, formatLatencyValue, formatPercentage, formatThroughput, formatNumber, getSuccessRateColor, getLatencyColor } from '../../utils/metric-formatters'
import {
  EllipsisVerticalIcon,
  ChartBarIcon,
  DocumentArrowDownIcon,
  ExclamationTriangleIcon,
  CheckCircleIcon,
  ClockIcon,
  ServerIcon,
  BoltIcon,
  ArrowTrendingUpIcon,
  MinusIcon,
  ChevronDownIcon,
  ChevronRightIcon,
} from '@heroicons/react/24/outline'
import LoadingSpinner from '../LoadingSpinner'
import ExportButton from '../ui/ExportButton'

export interface PerClientMetricsTableProps {
  data: DetailedMetrics
  sortable?: boolean
  filterable?: boolean
  exportable?: boolean
  onClientSelect?: (client: string) => void
}

export function PerClientMetricsTable({
  data,
  sortable = true,
  filterable = true,
  exportable = true,
  onClientSelect
}: PerClientMetricsTableProps) {
  const [selectedClient, setSelectedClient] = useState<string | null>(null)
  const [contextMenuOpen, setContextMenuOpen] = useState<{ client: string; x: number; y: number } | null>(null)
  const [expandedClients, setExpandedClients] = useState<Set<string>>(new Set())
  const contextMenuRef = useRef<HTMLDivElement>(null)
  const tableContainerRef = useRef<HTMLDivElement>(null)

  // Transform client metrics
  const clientMetrics = useMemo<ClientMetrics[]>(() => {
    return data.clientMetrics
  }, [data.clientMetrics])

  // Use all client metrics without filtering
  const filteredMetrics = clientMetrics

  // Calculate summary statistics
  const summaryStats = useMemo(() => {
    const clients = filteredMetrics
    const totalRequests = clients.reduce((sum, client) => sum + client.totalRequests, 0)
    const totalErrors = clients.reduce((sum, client) => sum + (client.totalRequests * client.errorRate / 100), 0)
    const avgSuccessRate = clients.reduce((sum, client) => sum + client.successRate, 0) / clients.length
    const avgLatency = clients.reduce((sum, client) => sum + client.latencyPercentiles.p95, 0) / clients.length
    const avgThroughput = clients.reduce((sum, client) => sum + client.throughput, 0) / clients.length
    const avgErrorRate = clients.reduce((sum, client) => sum + client.errorRate, 0) / clients.length

    return {
      totalClients: clients.length,
      totalRequests,
      totalErrors,
      avgSuccessRate: avgSuccessRate || 0,
      avgLatency: avgLatency || 0,
      avgThroughput: avgThroughput || 0,
      avgErrorRate: avgErrorRate || 0
    }
  }, [filteredMetrics])

  // Handle row click
  const handleRowClick = useCallback((client: ClientMetrics) => {
    setSelectedClient(client.clientName)
    onClientSelect?.(client.clientName)
  }, [onClientSelect])

  // Handle context menu
  const handleContextMenu = useCallback((e: React.MouseEvent, client: ClientMetrics) => {
    e.preventDefault()
    e.stopPropagation()
    setContextMenuOpen({
      client: client.clientName,
      x: e.clientX,
      y: e.clientY
    })
  }, [])

  // Close context menu
  const closeContextMenu = useCallback(() => {
    setContextMenuOpen(null)
  }, [])

  // Toggle client expansion
  const toggleClientExpansion = useCallback((clientName: string) => {
    setExpandedClients(prev => {
      const next = new Set(prev)
      if (next.has(clientName)) {
        next.delete(clientName)
      } else {
        next.add(clientName)
      }
      return next
    })
  }, [])

  // Get methods for a specific client
  const getMethodsForClient = useCallback((clientName: string): MethodMetrics[] => {
    if (!data.methodMetrics) return []
    // Return all methods since they should have aggregate data
    // In practice, we might want to filter based on whether the client made requests to this method
    return data.methodMetrics
  }, [data.methodMetrics])


  // Get trend icon
  const getTrendIcon = (current: number, baseline: number, isLowerBetter: boolean = false) => {
    if (current === baseline) return MinusIcon
    const improved = isLowerBetter ? current < baseline : current > baseline
    return improved ? ArrowTrendingUpIcon : ArrowTrendingDownIcon
  }

  // Define table columns
  const columns: ColumnDef<ClientMetrics>[] = [
    {
      id: 'clientName',
      header: 'Client',
      accessor: 'clientName',
      sortable: true,
      filterable: true,
      render: (value, client) => (
        <div className="flex items-center space-x-2">
          <button
            onClick={(e) => {
              e.stopPropagation()
              toggleClientExpansion(client.clientName)
            }}
            className="p-1 hover:bg-gray-200 rounded"
          >
            {expandedClients.has(client.clientName) ? (
              <ChevronDownIcon className="h-4 w-4 text-gray-500" />
            ) : (
              <ChevronRightIcon className="h-4 w-4 text-gray-500" />
            )}
          </button>
          <span className="font-medium text-gray-900">{value}</span>
        </div>
      )
    },
    {
      id: 'totalRequests',
      header: 'Total Requests',
      accessor: 'totalRequests',
      sortable: true,
      render: (value) => (
        <span className="font-mono text-sm">
          {formatNumber(value)}
        </span>
      )
    },
    {
      id: 'successRate',
      header: 'Success Rate',
      accessor: 'successRate',
      sortable: true,
      render: (value) => (
        <span className={`font-mono text-sm ${getSuccessRateColor(value)}`}>
          {formatPercentage(value)}
        </span>
      )
    },
    {
      id: 'p50Latency',
      header: 'P50 Latency',
      accessor: (client) => client.latencyPercentiles.p50,
      sortable: true,
      render: (value) => (
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 italic'}`}>
          {formatLatencyValue(value)}
        </span>
      )
    },
    {
      id: 'p95Latency',
      header: 'P95 Latency',
      accessor: (client) => client.latencyPercentiles.p95,
      sortable: true,
      render: (value) => (
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 italic'}`}>
          {formatLatencyValue(value)}
        </span>
      )
    },
    {
      id: 'p99Latency',
      header: 'P99 Latency',
      accessor: (client) => client.latencyPercentiles.p99,
      sortable: true,
      render: (value) => (
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 italic'}`}>
          {formatLatencyValue(value)}
        </span>
      )
    },
    {
      id: 'throughput',
      header: 'Throughput',
      accessor: 'throughput',
      sortable: true,
      render: (value) => (
        <span className="font-mono text-sm text-blue-600">
          {formatThroughput(value)}
        </span>
      )
    },
    {
      id: 'errorRate',
      header: 'Error Rate',
      accessor: 'errorRate',
      sortable: true,
      render: (value) => (
        <span className={`font-mono text-sm ${value > 5 ? 'text-red-600' : value > 1 ? 'text-yellow-600' : 'text-green-600'}`}>
          {formatPercentage(value)}
        </span>
      )
    },
    {
      id: 'actions',
      header: '',
      accessor: 'clientName',
      width: '40px',
      sortable: false,
      filterable: false,
      render: (_, client) => (
        <button
          onClick={(e) => handleContextMenu(e, client)}
          className="p-1 hover:bg-gray-100 rounded"
        >
          <EllipsisVerticalIcon className="h-4 w-4 text-gray-500" />
        </button>
      )
    }
  ]


  // Close context menu on outside click
  React.useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (contextMenuRef.current && !contextMenuRef.current.contains(event.target as Node)) {
        closeContextMenu()
      }
    }
    
    if (contextMenuOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [contextMenuOpen, closeContextMenu])


  // Export client data
  const exportClientData = useCallback((clientName: string) => {
    const client = filteredMetrics.find(c => c.clientName === clientName)
    if (!client) return
    
    const exportData = {
      client: client.clientName,
      summary: {
        totalRequests: client.totalRequests,
        successRate: client.successRate,
        errorRate: client.errorRate,
        throughput: client.throughput
      },
      latencyPercentiles: client.latencyPercentiles,
      connectionMetrics: client.connectionMetrics,
      errorsByMethod: client.errorsByMethod,
      reliability: client.reliability,
      timeSeriesData: client.timeSeriesData
    }
    
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${clientName}_metrics.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [filteredMetrics])

  if (!data || !data.clientMetrics || data.clientMetrics.length === 0) {
    return (
      <div className="card">
        <div className="card-body">
          <div className="bg-yellow-50 border-l-4 border-yellow-400 p-4">
            <div className="flex">
              <div className="flex-shrink-0">
                <ExclamationTriangleIcon className="h-5 w-5 text-yellow-400" />
              </div>
              <div className="ml-3">
                <h3 className="text-sm font-medium text-yellow-800">
                  Per-client metrics not available
                </h3>
                <div className="mt-2 text-sm text-yellow-700">
                  <p>This benchmark run does not contain individual client performance data. Possible reasons:</p>
                  <ul className="list-disc list-inside mt-1">
                    <li>The benchmark was run before per-client tracking was implemented</li>
                    <li>All clients were tested together without individual tracking</li>
                    <li>The data is still being processed</li>
                  </ul>
                  <p className="mt-2">Only aggregate metrics across all clients are available for this run.</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  // Method Performance Dropdown Component
  const MethodPerformanceDropdown = ({ methods, clientName }: { methods: MethodMetrics[]; clientName: string }) => (
    <div className="px-6 py-4 bg-gray-50">
      <h4 className="font-medium text-sm text-gray-700 mb-3">Method Performance for {clientName}</h4>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-100">
            <tr>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Method</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Requests</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Success Rate</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">P50</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">P95</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">P99</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {methods.map((method) => {
              // Calculate success rate for this method based on error data
              const errorData = method.errorsByClient[clientName]
              const successRate = errorData ? (100 - errorData.percentage) : 100
              
              return (
                <tr key={method.methodName}>
                  <td className="px-4 py-2 text-sm font-medium text-gray-900">{method.methodName}</td>
                  <td className="px-4 py-2 text-sm text-gray-600 font-mono">{formatNumber(method.requestCount)}</td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={getSuccessRateColor(successRate)}>
                      {formatPercentage(successRate)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p50 !== null && method.latencyPercentiles.p50 !== undefined ? getLatencyColor(method.latencyPercentiles.p50) : 'text-gray-400 italic'}>
                      {formatLatencyValue(method.latencyPercentiles.p50)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p95 !== null && method.latencyPercentiles.p95 !== undefined ? getLatencyColor(method.latencyPercentiles.p95) : 'text-gray-400 italic'}>
                      {formatLatencyValue(method.latencyPercentiles.p95)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p99 !== null && method.latencyPercentiles.p99 !== undefined ? getLatencyColor(method.latencyPercentiles.p99) : 'text-gray-400 italic'}>
                      {formatLatencyValue(method.latencyPercentiles.p99)}
                    </span>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )

  return (
    <div className="space-y-6">
      {/* Summary Statistics */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="card">
          <div className="card-body">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-gray-600">Total Clients</p>
                <p className="text-2xl font-bold text-gray-900">{summaryStats.totalClients}</p>
              </div>
              <ServerIcon className="h-8 w-8 text-blue-600" />
            </div>
          </div>
        </div>
        
        <div className="card">
          <div className="card-body">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-gray-600">Avg Success Rate</p>
                <p className={`text-2xl font-bold ${getSuccessRateColor(summaryStats.avgSuccessRate)}`}>
                  {formatPercentage(summaryStats.avgSuccessRate)}
                </p>
              </div>
              <CheckCircleIcon className="h-8 w-8 text-green-600" />
            </div>
          </div>
        </div>
        
        <div className="card">
          <div className="card-body">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-gray-600">Avg P95 Latency</p>
                <p className={`text-2xl font-bold ${summaryStats.avgLatency !== null && summaryStats.avgLatency !== undefined ? getLatencyColor(summaryStats.avgLatency) : 'text-gray-400 italic'}`}>
                  {formatLatencyValue(summaryStats.avgLatency)}
                </p>
              </div>
              <ClockIcon className="h-8 w-8 text-yellow-600" />
            </div>
          </div>
        </div>
        
        <div className="card">
          <div className="card-body">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-gray-600">Avg Throughput</p>
                <p className="text-2xl font-bold text-blue-600">
                  {formatThroughput(summaryStats.avgThroughput)}
                </p>
              </div>
              <BoltIcon className="h-8 w-8 text-blue-600" />
            </div>
          </div>
        </div>
      </div>

      {/* Main Table */}
      <div className="card">
        <div className="card-header">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-lg font-semibold text-gray-900">Per-Client Metrics</h3>
              <p className="text-sm text-gray-600 mt-1">
                Detailed performance metrics for each client ({filteredMetrics.length} clients)
              </p>
            </div>
            <div className="flex items-center space-x-3">
              {exportable && (
                <ExportButton
                  data={filteredMetrics}
                  filename="client_metrics_export"
                  formats={['csv', 'json', 'xlsx']}
                />
              )}
            </div>
          </div>
        </div>
        
        <div 
          ref={tableContainerRef} 
          className="relative border border-gray-200 rounded-b-lg shadow-inner"
          style={{ 
            height: '60vh',
            maxHeight: '800px',
            minHeight: '400px',
            overflowY: 'scroll',
            overflowX: 'auto',
            WebkitOverflowScrolling: 'touch', // iOS smooth scrolling
            msOverflowStyle: '-ms-autohiding-scrollbar', // IE/Edge
            scrollBehavior: 'smooth'
          }}
        >
          <table className="table relative">
            <thead className="table-header" style={{ position: 'sticky', top: 0, zIndex: 10, backgroundColor: 'white', boxShadow: '0 1px 2px 0 rgba(0, 0, 0, 0.05)' }}>
              <tr>
                {columns.map((column) => (
                  <th
                    key={column.id}
                    className={`table-header-cell ${column.headerClassName || ''} ${
                      sortable && column.sortable !== false ? 'cursor-pointer hover:bg-gray-100' : ''
                    }`}
                    style={{
                      width: column.width,
                      minWidth: column.minWidth,
                      maxWidth: column.maxWidth,
                    }}
                  >
                    {column.header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="bg-white divide-y divide-gray-200">
              {filteredMetrics.map((row, index) => (
                <React.Fragment key={row.clientName}>
                  <tr
                    className={`
                      table-row cursor-pointer
                      ${index % 2 === 0 ? 'bg-gray-50' : ''}
                      ${selectedClient === row.clientName ? 'bg-blue-50' : ''}
                      hover:bg-blue-50
                    `}
                    onClick={() => handleRowClick(row)}
                  >
                      {columns.map((column) => {
                        const value = typeof column.accessor === 'function' 
                          ? column.accessor(row) 
                          : row[column.accessor]
                        
                        return (
                          <td
                            key={column.id}
                            className={`table-cell ${column.className || ''}`}
                            style={{
                              width: column.width,
                              minWidth: column.minWidth,
                              maxWidth: column.maxWidth,
                            }}
                          >
                            {column.render ? column.render(value, row) : String(value)}
                          </td>
                        )
                      })}
                  </tr>
                  {expandedClients.has(row.clientName) && (
                    <tr className="bg-gray-50">
                      <td colSpan={columns.length} className="p-0 relative">
                        <div className="relative z-0">
                          <MethodPerformanceDropdown
                            methods={getMethodsForClient(row.clientName)}
                            clientName={row.clientName}
                          />
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Context Menu */}
      {contextMenuOpen && (
        <div
          ref={contextMenuRef}
          className="fixed z-50 bg-white border border-gray-200 rounded-md shadow-lg py-1 min-w-[200px]"
          style={{
            left: contextMenuOpen.x,
            top: contextMenuOpen.y,
          }}
        >
          <button
            onClick={() => {
              onClientSelect?.(contextMenuOpen.client)
              closeContextMenu()
            }}
            className="w-full px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 flex items-center"
          >
            <ChartBarIcon className="h-4 w-4 mr-2" />
            View Details
          </button>
          
          <button
            onClick={() => {
              exportClientData(contextMenuOpen.client)
              closeContextMenu()
            }}
            className="w-full px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 flex items-center"
          >
            <DocumentArrowDownIcon className="h-4 w-4 mr-2" />
            Export Data
          </button>
          
          <hr className="my-1" />
          
          <button
            onClick={() => {
              // Could implement comparison functionality here
              closeContextMenu()
            }}
            className="w-full px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 flex items-center"
          >
            <ArrowTrendingUpIcon className="h-4 w-4 mr-2" />
            Compare with Baseline
          </button>
        </div>
      )}
    </div>
  )
}

export default PerClientMetricsTable