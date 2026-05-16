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
  /**
   * When set, restrict the table + summary tiles to just this client. Empty
   * string (the dropdown's "All Clients" option) means show all.
   */
  clientFilter?: string
  /**
   * Map of clientName → web3_clientVersion string captured at run start.
   * Rendered under each client name when present.
   */
  clientVersions?: Record<string, string>
}

export function PerClientMetricsTable({
  data,
  sortable = true,
  filterable = true,
  exportable = true,
  onClientSelect,
  clientFilter = '',
  clientVersions,
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

  // Apply the client filter from the parent. Empty string means no filter.
  const filteredMetrics = useMemo<ClientMetrics[]>(() => {
    if (!clientFilter) return clientMetrics
    return clientMetrics.filter(c => c.clientName === clientFilter)
  }, [clientMetrics, clientFilter])

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

  // Anchor the menu to the kebab button so the popup is predictable and stays
  // on-screen regardless of where the table is scrolled. We also stop event
  // propagation so the row's onClick (which toggles selection) doesn't fire.
  const handleContextMenu = useCallback((e: React.MouseEvent, client: ClientMetrics) => {
    e.preventDefault()
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const MENU_W = 220
    const MENU_H = 140
    // Default: drop the menu just below the button, aligned to its right edge.
    let x = rect.right - MENU_W
    let y = rect.bottom + 4
    // Clamp horizontally so it can't run off the right or left.
    const vw = typeof window !== 'undefined' ? window.innerWidth : 1024
    const vh = typeof window !== 'undefined' ? window.innerHeight : 768
    if (x + MENU_W > vw - 8) x = vw - MENU_W - 8
    if (x < 8) x = 8
    // Flip above the button if we'd run off the bottom.
    if (y + MENU_H > vh - 8) y = rect.top - MENU_H - 4
    if (y < 8) y = 8
    setContextMenuOpen({ client: client.clientName, x, y })
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
    // Return method metrics with client-specific data if available
    return data.methodMetrics.map(method => {
      // Check if we have client-specific data in the method
      const clientSpecificData = data.clientMethodMetrics?.[clientName]?.[method.methodName]
      
      if (clientSpecificData) {
        // Return method with client-specific metrics
        return {
          ...method,
          requestCount: clientSpecificData.total_requests || method.requestCount,
          latencyPercentiles: {
            ...method.latencyPercentiles,
            p50: clientSpecificData.p50_latency ?? method.latencyPercentiles.p50,
            p95: clientSpecificData.p95_latency ?? method.latencyPercentiles.p95,
            p99: clientSpecificData.p99_latency ?? method.latencyPercentiles.p99,
            min: clientSpecificData.min_latency ?? method.latencyPercentiles.min,
            max: clientSpecificData.max_latency ?? method.latencyPercentiles.max,
            mean: clientSpecificData.avg_latency ?? method.latencyPercentiles.mean,
          },
          // Update error rate for this client
          errorsByClient: {
            ...method.errorsByClient,
            [clientName]: {
              count: Math.round((clientSpecificData.total_requests || 0) * (clientSpecificData.error_rate || 0) / 100),
              percentage: clientSpecificData.error_rate || 0
            }
          }
        }
      }
      
      return method
    })
  }, [data.methodMetrics, data.clientMethodMetrics])


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
      render: (value, client) => {
        const version = clientVersions?.[client.clientName]
        const showVersion = version && version !== 'unknown'
        return (
          <div className="flex items-center space-x-2">
            <button
              onClick={(e) => {
                e.stopPropagation()
                toggleClientExpansion(client.clientName)
              }}
              className="p-1 hover:bg-gray-200 dark:hover:bg-slate-700 rounded"
            >
              {expandedClients.has(client.clientName) ? (
                <ChevronDownIcon className="h-4 w-4 text-gray-500 dark:text-slate-400" />
              ) : (
                <ChevronRightIcon className="h-4 w-4 text-gray-500 dark:text-slate-400" />
              )}
            </button>
            <div className="flex flex-col leading-tight min-w-0">
              <span className="font-medium text-gray-900 dark:text-slate-100">{value}</span>
              {showVersion && (
                <span
                  className="text-[10px] font-mono text-gray-500 dark:text-slate-400 truncate max-w-[16rem]"
                  title={version}
                >
                  {version}
                </span>
              )}
              {version === 'unknown' && (
                <span className="text-[10px] font-mono text-gray-400 dark:text-slate-500 italic" title="web3_clientVersion did not respond">
                  version unknown
                </span>
              )}
            </div>
          </div>
        )
      }
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
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 dark:text-slate-500 italic'}`}>
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
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 dark:text-slate-500 italic'}`}>
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
        <span className={`font-mono text-sm ${value !== null && value !== undefined ? getLatencyColor(value) : 'text-gray-400 dark:text-slate-500 italic'}`}>
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
          type="button"
          aria-haspopup="menu"
          aria-label={`Actions for ${client.clientName}`}
          onClick={(e) => handleContextMenu(e, client)}
          className="rounded p-1 hover:bg-gray-100 dark:hover:bg-slate-700"
        >
          <EllipsisVerticalIcon className="h-4 w-4 text-gray-500 dark:text-slate-400" />
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
    <div className="px-6 py-4 bg-gray-50 dark:bg-slate-900/60">
      <h4 className="font-medium text-sm text-gray-700 dark:text-slate-300 mb-3">Method Performance for {clientName}</h4>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-slate-700">
          <thead className="bg-gray-100 dark:bg-slate-800">
            <tr>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">Method</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">Requests</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">Success Rate</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">P50</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">P95</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-slate-400 uppercase">P99</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200 dark:bg-slate-900 dark:divide-slate-700">
            {methods.map((method) => {
              // Calculate success rate for this method
              const successRate = method.errorsByClient[clientName] 
                ? (100 - method.errorsByClient[clientName].percentage) 
                : (100 - (method.errorRate || 0))
              
              return (
                <tr key={method.methodName}>
                  <td className="px-4 py-2 text-sm font-medium text-gray-900 dark:text-slate-100">{method.methodName}</td>
                  <td className="px-4 py-2 text-sm text-gray-600 dark:text-slate-300 font-mono">{formatNumber(method.requestCount)}</td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={getSuccessRateColor(successRate)}>
                      {formatPercentage(successRate)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p50 !== null && method.latencyPercentiles.p50 !== undefined ? getLatencyColor(method.latencyPercentiles.p50) : 'text-gray-400 dark:text-slate-500 italic'}>
                      {formatLatencyValue(method.latencyPercentiles.p50)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p95 !== null && method.latencyPercentiles.p95 !== undefined ? getLatencyColor(method.latencyPercentiles.p95) : 'text-gray-400 dark:text-slate-500 italic'}>
                      {formatLatencyValue(method.latencyPercentiles.p95)}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm font-mono">
                    <span className={method.latencyPercentiles.p99 !== null && method.latencyPercentiles.p99 !== undefined ? getLatencyColor(method.latencyPercentiles.p99) : 'text-gray-400 dark:text-slate-500 italic'}>
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
                <p className="text-sm font-medium text-gray-600 dark:text-slate-300">Total Clients</p>
                <p className="text-2xl font-bold text-gray-900 dark:text-slate-100">{summaryStats.totalClients}</p>
              </div>
              <ServerIcon className="h-8 w-8 text-blue-600" />
            </div>
          </div>
        </div>
        
        <div className="card">
          <div className="card-body">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm font-medium text-gray-600 dark:text-slate-300">Avg Success Rate</p>
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
                <p className="text-sm font-medium text-gray-600 dark:text-slate-300">Avg P95 Latency</p>
                <p className={`text-2xl font-bold ${summaryStats.avgLatency !== null && summaryStats.avgLatency !== undefined ? getLatencyColor(summaryStats.avgLatency) : 'text-gray-400 dark:text-slate-500 italic'}`}>
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
                <p className="text-sm font-medium text-gray-600 dark:text-slate-300">Avg Throughput</p>
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
              <h3 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Per-Client Metrics</h3>
              <p className="text-sm text-gray-600 dark:text-slate-300 mt-1">
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
          className="relative border border-gray-200 dark:border-slate-700 rounded-b-lg shadow-inner"
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
            <thead className="table-header sticky top-0 z-10 shadow-sm bg-gray-50 dark:bg-slate-900/80">
              <tr>
                {columns.map((column) => (
                  <th
                    key={column.id}
                    className={`table-header-cell ${column.headerClassName || ''} ${
                      sortable && column.sortable !== false ? 'cursor-pointer hover:bg-gray-100 dark:hover:bg-slate-700' : ''
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
            <tbody className="bg-white divide-y divide-gray-200 dark:bg-slate-900 dark:divide-slate-700">
              {filteredMetrics.map((row, index) => (
                <React.Fragment key={row.clientName}>
                  <tr
                    className={`
                      table-row cursor-pointer
                      ${index % 2 === 0 ? 'bg-gray-50 dark:bg-slate-900/60' : ''}
                      ${selectedClient === row.clientName ? 'bg-blue-50 dark:bg-primary-900/40' : ''}
                      hover:bg-blue-50 dark:hover:bg-primary-900/30
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
                    <tr className="bg-gray-50 dark:bg-slate-900/60">
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
          role="menu"
          className="fixed z-50 min-w-[200px] rounded-md border border-gray-200 bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-900"
          style={{
            left: contextMenuOpen.x,
            top: contextMenuOpen.y,
          }}
        >
          <button
            role="menuitem"
            onClick={() => {
              // "View Details" toggles the row's per-method drilldown — the
              // same drawer that opens when you click the row itself — and
              // also marks this client as the selected one. Previously this
              // only called onClientSelect, which duplicated the header
              // dropdown and looked like a no-op.
              toggleClientExpansion(contextMenuOpen.client)
              onClientSelect?.(contextMenuOpen.client)
              closeContextMenu()
            }}
            className="flex w-full items-center px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            <ChartBarIcon className="mr-2 h-4 w-4" />
            {expandedClients.has(contextMenuOpen.client) ? 'Hide method details' : 'View method details'}
          </button>

          <button
            role="menuitem"
            onClick={() => {
              exportClientData(contextMenuOpen.client)
              closeContextMenu()
            }}
            className="flex w-full items-center px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            <DocumentArrowDownIcon className="mr-2 h-4 w-4" />
            Export Data
          </button>

          <hr className="my-1 border-gray-200 dark:border-slate-700" />

          <button
            role="menuitem"
            onClick={() => {
              // Scroll the page's baseline-comparison panel into view rather
              // than fake a comparison from this row. The panel has the
              // baseline picker the user can drive directly.
              closeContextMenu()
              const target = document.querySelector('[data-testid="baseline-comparison-card"]')
              if (target instanceof HTMLElement) {
                target.scrollIntoView({ behavior: 'smooth', block: 'start' })
                const select = target.querySelector<HTMLSelectElement>('#baseline-compare-select')
                if (select) {
                  select.focus()
                }
              }
            }}
            className="flex w-full items-center px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            <ArrowTrendingUpIcon className="mr-2 h-4 w-4" />
            Compare with Baseline
          </button>
        </div>
      )}
    </div>
  )
}

export default PerClientMetricsTable