import { useParams, Link, useNavigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { Helmet } from 'react-helmet-async'
import {
  FlagIcon,
  DocumentArrowDownIcon,
  ShareIcon,
  ClipboardDocumentIcon,
  ChevronDownIcon
} from '@heroicons/react/24/outline'
import {
  LoadingSpinner,
  ErrorRateChart,
  BaselineManager
} from '../components'
import PerClientPercentilesChart from '../components/PerClientPercentilesChart'
import BaselineComparisonView from '../components/BaselineComparisonView'
import Breadcrumb from '../components/Breadcrumb'
import { useDetailedMetrics } from '../hooks/useDetailedMetrics'
import { ExpandableSection } from '../components/ui/ExpandableSection'
import { PerClientMetricsTable } from '../components/metrics'
import { ExportButton } from '../components/ui'
import type { HistoricRun, BenchmarkResult } from '../types/api'
import { useRun, useSetBaseline, useRemoveBaseline, useBaselines, useRuns, useBaselineComparison } from '../api/hooks'
import { formatPercentage, formatLatency } from '../utils/metric-formatters'

export default function RunDetails() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  // Use shared API hook for basic run data
  const { data: run, isLoading, error } = useRun(id || '', !!id)
  
  // Use detailed metrics hook for comprehensive data
  const { 
    data: detailedMetrics, 
    isLoading: detailedLoading, 
    error: detailedError 
  } = useDetailedMetrics(id || '', !!id)
  
  // State management for filters and expansions
  const [expandedSections, setExpandedSections] = useState<string[]>(['overview'])
  const [clientFilter, setClientFilter] = useState<string>('')
  const [selectedBaselineName, setSelectedBaselineName] = useState<string>('')
  
  // Log errors for debugging
  useEffect(() => {
    if (detailedError) {
      console.error('Failed to load detailed metrics:', detailedError)
    }
  }, [detailedError])

  // For now, use the run data directly since the report endpoint doesn't exist
  // TODO: Implement detailed results when the API endpoint is available
  const detailedResults = run ? {
    runId: run.id,
    timestamp: run.timestamp,
    totalRequests: run.total_requests,
    successRate: run.success_rate,
    avgLatency: run.avg_latency_ms,
    p95Latency: run.p95_latency_ms,
    errorRate: 100 - run.success_rate,
    methods: run.methods || [],
    clients: run.clients || [],
    // Calculate throughput and failed requests
    throughput: run.duration ? (() => {
      // Parse duration like "1m1.080094458s" to seconds
      const match = run.duration.match(/(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?/)
      if (match) {
        const hours = parseInt(match[1] || '0')
        const minutes = parseInt(match[2] || '0')
        const seconds = parseFloat(match[3] || '0')
        const totalSeconds = hours * 3600 + minutes * 60 + seconds
        // Add validation to prevent division by zero
        if (totalSeconds > 0 && run.total_requests >= 0) {
          return run.total_requests / totalSeconds
        }
      }
      return 0
    })() : 0,
    failedRequests: Math.round(run.total_requests * (100 - run.success_rate) / 100),
    // Add missing properties for compatibility
    errors: [],
    clientResults: []
  } : null

  // Use proper API hooks for baseline management
  const setBaselineMutation = useSetBaseline()
  const removeBaselineMutation = useRemoveBaseline()

  // Fetch baselines and available runs for the BaselineManager
  const { data: baselines = [] } = useBaselines()
  const { data: availableRuns = [] } = useRuns({ limit: 50 })

  // Baselines applicable to this run's test (the backend only allows comparing
  // a run to a baseline with the same test name).
  const baselinesForThisTest = run
    ? baselines.filter(b => b.test_name === run.test_name)
    : []

  const {
    data: baselineComparison,
    isFetching: comparisonLoading,
    error: comparisonError,
  } = useBaselineComparison(id || '', selectedBaselineName, !!id && !!selectedBaselineName)

  // Helper function to toggle section expansion
  const toggleSection = (section: string) => {
    setExpandedSections(prev => 
      prev.includes(section) 
        ? prev.filter(s => s !== section)
        : [...prev, section]
    )
  }

  // Simple filter dropdown component
  const ClientFilterDropdown = ({ 
    clients, 
    selected, 
    onChange 
  }: { 
    clients: string[], 
    selected: string, 
    onChange: (value: string) => void 
  }) => (
    <div className="relative">
      <select
        value={selected}
        onChange={(e) => onChange(e.target.value)}
        className="block w-full pl-3 pr-10 py-2 text-base border-gray-300 dark:border-slate-600 focus:outline-none focus:ring-primary-500 focus:border-primary-500 sm:text-sm rounded-md"
      >
        <option value="">All Clients</option>
        {clients.map(client => (
          <option key={client} value={client}>{client}</option>
        ))}
      </select>
    </div>
  )


  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      // Show success toast
    } catch (err) {
      console.error('Failed to copy to clipboard:', err)
    }
  }

  const shareRun = async () => {
    const url = window.location.href
    if (navigator.share) {
      try {
        await navigator.share({
          title: `Benchmark Run: ${run?.test_name}`,
          text: `View benchmark results for ${run?.test_name}`,
          url,
        })
      } catch (err) {
        copyToClipboard(url)
      }
    } else {
      copyToClipboard(url)
    }
  }

  if (isLoading) {
    return (
      <div className="p-6">
        <LoadingSpinner size="lg" className="h-64" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6">
        <div className="card p-8 text-center">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-slate-100 mb-2">Run Not Found</h1>
          <p className="text-gray-600 dark:text-slate-300 mb-4">
            The benchmark run "{id}" could not be found.
          </p>
          <Link to="/" className="btn-primary">
            Back to Dashboard
          </Link>
        </div>
      </div>
    )
  }

  // Show detailed metrics errors as warnings but don't block the page
  const showDetailedError = detailedError && !detailedLoading

  if (!run) return null

  return (
    <>
      <Helmet>
        <title>{run?.test_name ? `${run.test_name} - Run ${run.id}` : 'Run Details'}</title>
        <meta name="description" content={`Detailed benchmark results for ${run?.test_name || 'benchmark run'} from ${run?.timestamp ? new Date(run.timestamp).toLocaleDateString() : 'unknown date'}`} />
        <style>
          {`
            @media print {
              .btn-outline, .btn-primary, button {
                display: none !important;
              }
              .card {
                break-inside: avoid;
                page-break-inside: avoid;
              }
              .space-y-6 > * {
                page-break-inside: avoid;
              }
              .grid {
                display: block !important;
              }
              .grid > * {
                margin-bottom: 1rem;
              }
              .hidden-print {
                display: none !important;
              }
              body {
                print-color-adjust: exact;
                -webkit-print-color-adjust: exact;
              }
            }
          `}
        </style>
      </Helmet>
      
      <div className="p-6">
        <Breadcrumb items={[
          { label: 'Dashboard', href: '/' },
          // Link back to the test detail page so users can jump to other runs
          // of the same scenario without back-button gymnastics. Omitted while
          // the run is still loading.
          ...(run?.test_name
            ? [{ label: run.test_name, href: `/tests/${encodeURIComponent(run.test_name)}` }]
            : []),
          { label: run ? run.id.substring(0, 8) + '...' : 'Loading...' },
        ]} />
        
        {/* Detailed Metrics Error Warning */}
        {showDetailedError && (
          <div className="mb-6 bg-warning-50 border border-warning-200 rounded-md p-4">
            <div className="flex">
              <div className="flex-shrink-0">
                <svg className="h-5 w-5 text-warning-400" viewBox="0 0 20 20" fill="currentColor">
                  <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                </svg>
              </div>
              <div className="ml-3">
                <h3 className="text-sm font-medium text-warning-800">
                  Detailed Metrics Unavailable
                </h3>
                <div className="mt-2 text-sm text-warning-700">
                  <p>
                    Some advanced metrics and analysis features are not available for this run. 
                    Basic run information and standard charts are still accessible below.
                  </p>
                </div>
              </div>
            </div>
          </div>
        )}
        
        {/* Header */}
        <div className="mb-6">
          <div className="flex items-start justify-between">
            <div className="flex-1">
              <div className="flex items-center space-x-3 mb-2">
                <h1 className="text-3xl font-bold text-gray-900 dark:text-slate-100">{run.test_name}</h1>
                {run.is_baseline && (
                  <span className="badge badge-info">
                    <FlagIcon className="h-4 w-4 mr-1" />
                    Baseline
                  </span>
                )}
              </div>
              <div className="space-y-1">
                <p className="text-gray-600 dark:text-slate-300">
                  Run ID: 
                  <button
                    onClick={() => copyToClipboard(run.id)}
                    className="font-mono ml-1 hover:text-primary-600 transition-colors"
                    title="Click to copy"
                  >
                    {run.id}
                  </button>
                </p>
                <p className="text-sm text-gray-500 dark:text-slate-400">
                  Executed on {new Date(run.timestamp).toLocaleDateString()} at {new Date(run.timestamp).toLocaleTimeString()}
                </p>
              </div>
            </div>
            
            <div className="flex flex-col sm:flex-row gap-2">
              {!run.is_baseline && (
                <button
                  onClick={() => setBaselineMutation.mutate()}
                  disabled={setBaselineMutation.isPending}
                  className="btn-outline"
                >
                  <FlagIcon className="h-4 w-4 mr-2" />
                  {setBaselineMutation.isPending ? 'Setting...' : 'Set as Baseline'}
                </button>
              )}
              
              <button
                onClick={shareRun}
                className="btn-outline"
                title="Share this run"
              >
                <ShareIcon className="h-4 w-4 mr-2" />
                Share
              </button>
              
              <ExportButton
                data={{
                  run,
                  detailedResults,
                  detailedMetrics: detailedMetrics || {},
                  filters: { clientFilter },
                  timestamp: new Date().toISOString()
                }}
                filename={`comprehensive-report-${run.id}`}
                formats={['json', 'csv', 'xlsx']}
                className="btn-outline"
              >
                <DocumentArrowDownIcon className="h-4 w-4 mr-2" />
                Export Report
              </ExportButton>
            </div>
          </div>
        </div>

        {/* Quick Actions Bar */}
        <div className="mb-6 flex flex-wrap items-center gap-2 text-sm">
          <button
            onClick={() => copyToClipboard(run.git_commit)}
            className="inline-flex items-center px-2 py-1 bg-gray-100 dark:bg-slate-800 hover:bg-gray-200 dark:bg-slate-700 rounded transition-colors"
            title="Copy commit hash"
          >
            <ClipboardDocumentIcon className="h-3 w-3 mr-1" />
            {run.git_commit}
          </button>
          <span className="text-gray-400 dark:text-slate-500">on</span>
          <span className="px-2 py-1 bg-primary-100 text-primary-700 rounded">
            {run.git_branch}
          </span>
          {run.description && (
            <>
              <span className="text-gray-400 dark:text-slate-500">•</span>
              <span className="text-gray-600 dark:text-slate-300">{run.description}</span>
            </>
          )}
        </div>

        {/* Overview Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <div className="card p-6">
            <h3 className="text-sm font-medium text-gray-500 dark:text-slate-400">Duration</h3>
            <p className="text-2xl font-bold text-gray-900 dark:text-slate-100 mt-1">{run.duration}</p>
            <p className="text-xs text-gray-400 dark:text-slate-500 mt-1">Test execution time</p>
          </div>
          <div className="card p-6">
            <h3 className="text-sm font-medium text-gray-500 dark:text-slate-400">Total Requests</h3>
            <p className="text-2xl font-bold text-gray-900 dark:text-slate-100 mt-1">{run.total_requests.toLocaleString()}</p>
            {detailedResults && (
              <p className="text-xs text-gray-400 dark:text-slate-500 mt-1">
                {detailedResults.throughput.toFixed(1)} req/s
              </p>
            )}
          </div>
          <div className="card p-6">
            <h3 className="text-sm font-medium text-gray-500 dark:text-slate-400">Success Rate</h3>
            <p className={`text-2xl font-bold mt-1 ${
              run.success_rate >= 99 ? 'text-success-600' :
              run.success_rate >= 95 ? 'text-warning-600' : 'text-danger-600'
            }`}>{formatPercentage(run.success_rate)}</p>
            {detailedResults && (
              <p className="text-xs text-gray-400 dark:text-slate-500 mt-1">
                {detailedResults.failedRequests.toLocaleString()} failed
              </p>
            )}
          </div>
          <div className="card p-6">
            <h3 className="text-sm font-medium text-gray-500 dark:text-slate-400">Avg Latency</h3>
            <p className={`text-2xl font-bold mt-1 ${
              run.avg_latency_ms > 300 ? 'text-danger-600' :
              run.avg_latency_ms > 200 ? 'text-warning-600' : 'text-success-600'
            }`}>{formatLatency(run.avg_latency_ms)}</p>
            <p className="text-xs text-gray-400 dark:text-slate-500 mt-1">
              p95: {run.p95_latency_ms}ms
            </p>
          </div>
        </div>

        {/* Error notification for detailed metrics */}
        {detailedError && (
          <div className="mt-6 p-4 bg-warning-50 border border-warning-200 rounded-lg">
            <p className="text-warning-800">
              Note: Detailed metrics are currently unavailable. Showing basic run information only.
            </p>
          </div>
        )}

        {/* Comprehensive Detailed Metrics Sections */}
        <div className="space-y-6 mb-8">
          {/* Client Performance Analysis */}
          <ExpandableSection 
            title="Client Performance Analysis"
            subtitle="Comprehensive performance metrics by client implementation"
            defaultExpanded={expandedSections.includes('client-performance')}
            isLoading={detailedLoading}
            error={detailedError ? "Unable to load client metrics" : undefined}
            headerActions={
              <div className="flex space-x-2">
                <ClientFilterDropdown 
                  clients={detailedMetrics?.clientMetrics?.map(c => c.clientName) || run?.clients || []}
                  selected={clientFilter}
                  onChange={setClientFilter}
                />
                <ExportButton 
                  data={detailedMetrics?.clientMetrics || []}
                  filename={`client-metrics-${run?.id}`}
                  formats={['csv', 'json', 'xlsx']}
                />
              </div>
            }
            onToggle={() => toggleSection('client-performance')}
          >
            {detailedMetrics && (
              <PerClientMetricsTable
                data={detailedMetrics}
                onClientSelect={setClientFilter}
                clientFilter={clientFilter}
                clientVersions={run?.client_versions}
              />
            )}
          </ExpandableSection>

        </div>

      {/* Details Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Run Information */}
        <div className="card">
          <div className="card-header">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Run Information</h2>
          </div>
          <div className="card-content">
            <dl className="space-y-4">
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Timestamp</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">
                  {new Date(run.timestamp).toLocaleString()}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Git Commit</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1 font-mono">{run.git_commit}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Git Branch</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{run.git_branch}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Description</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{run.description}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Config Hash</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1 font-mono break-all">{run.config_hash}</dd>
              </div>
            </dl>
          </div>
        </div>

        {/* Performance Metrics */}
        <div className="card">
          <div className="card-header">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Performance Metrics</h2>
          </div>
          <div className="card-content">
            <dl className="space-y-4">
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Average Latency</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{formatLatency(run.avg_latency_ms)}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">95th Percentile Latency</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{run.p95_latency_ms}ms</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Success Rate</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{formatPercentage(run.success_rate)}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500 dark:text-slate-400">Total Requests</dt>
                <dd className="text-sm text-gray-900 dark:text-slate-100 mt-1">{run.total_requests.toLocaleString()}</dd>
              </div>
            </dl>
          </div>
        </div>

        {/* Clients Tested */}
        <div className="card">
          <div className="card-header">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Clients Tested</h2>
          </div>
          <div className="card-content">
            <div className="flex flex-wrap gap-2">
              {run.clients.map((client) => (
                <span key={client} className="badge badge-info">
                  {client}
                </span>
              ))}
            </div>
          </div>
        </div>

        {/* Methods Tested */}
        <div className="card">
          <div className="card-header">
            <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Methods Tested</h2>
          </div>
          <div className="card-content">
            <div className="space-y-2">
              {run.methods.map((method) => (
                <div key={method} className="text-sm font-mono text-gray-900 dark:text-slate-100 bg-gray-50 dark:bg-slate-900/60 px-2 py-1 rounded">
                  {method}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

        {/* Performance Charts */}
        {detailedResults && (
          <div className="mt-8 space-y-8">
            {/* Latency Distribution */}
            <div className="card">
              <div className="card-header">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Latency Percentiles by Client</h2>
                <p className="text-sm text-gray-500 dark:text-slate-400">Real per-client p50 / p95 / p99 / max. Click a bar to filter the table below.</p>
              </div>
              <div className="card-content">
                {detailedMetrics?.clientMetrics && detailedMetrics.clientMetrics.length > 0 ? (
                  <PerClientPercentilesChart
                    clientMetrics={detailedMetrics.clientMetrics}
                    onClientClick={(name) => setClientFilter(name === clientFilter ? '' : name)}
                  />
                ) : (
                  <p className="text-sm text-gray-500 dark:text-slate-400">Per-client latency data not available for this run.</p>
                )}
                {clientFilter && (
                  <div className="mt-3 text-xs text-gray-500 dark:text-slate-400">
                    Filtering subsequent tables to client: <span className="font-mono text-gray-900 dark:text-slate-100">{clientFilter}</span>
                    <button onClick={() => setClientFilter('')} className="ml-2 text-primary-600 hover:underline">clear</button>
                  </div>
                )}
              </div>
            </div>

            {/* Error Rate Chart */}
            {detailedResults.errors.length > 0 && (
              <div className="card">
                <div className="card-header">
                  <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Error Analysis</h2>
                  <p className="text-sm text-gray-500 dark:text-slate-400">Error distribution and trends</p>
                </div>
                <div className="card-content">
                  <ErrorRateChart
                    data={detailedResults.errors.map(error => ({
                      timestamp: new Date().toISOString(),
                      errorRate: error.percentage,
                      totalErrors: error.count,
                      totalRequests: detailedResults.totalRequests,
                    }))}
                    title="Error Rate Over Time"
                  />
                </div>
              </div>
            )}

            {/* Client Performance Comparison */}
            {detailedResults.clientResults.length > 1 && (
              <div className="card">
                <div className="card-header">
                  <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Client Performance</h2>
                  <p className="text-sm text-gray-500 dark:text-slate-400">Performance by client implementation</p>
                </div>
                <div className="card-content">
                  <div className="overflow-x-auto">
                    <table className="table">
                      <thead className="table-header">
                        <tr>
                          <th className="table-header-cell">Client</th>
                          <th className="table-header-cell">Requests</th>
                          <th className="table-header-cell">Success Rate</th>
                          <th className="table-header-cell">Avg Latency</th>
                          <th className="table-header-cell">P95 Latency</th>
                          <th className="table-header-cell">Errors</th>
                        </tr>
                      </thead>
                      <tbody className="bg-white divide-y divide-gray-200 dark:divide-slate-700">
                        {detailedResults.clientResults.map((client) => (
                          <tr key={client.client} className="table-row">
                            <td className="table-cell font-medium text-gray-900 dark:text-slate-100">
                              {client.client}
                            </td>
                            <td className="table-cell">
                              {client.requests.toLocaleString()}
                            </td>
                            <td className="table-cell">
                              <span className={`badge ${
                                client.successRate >= 99 ? 'badge-success' :
                                client.successRate >= 95 ? 'badge-warning' : 'badge-danger'
                              }`}>
                                {formatPercentage(client.successRate)}
                              </span>
                            </td>
                            <td className="table-cell">
                              <span className={`font-medium ${
                                client.avgLatency > 300 ? 'text-danger-600' :
                                client.avgLatency > 200 ? 'text-warning-600' : 'text-success-600'
                              }`}>
                                {formatLatency(client.avgLatency)}
                              </span>
                            </td>
                            <td className="table-cell">
                              {formatLatency(client.p95Latency)}
                            </td>
                            <td className="table-cell">
                              {client.errors}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
            )}

            {/* Compare against a saved baseline */}
            <div className="card" data-testid="baseline-comparison-card">
              <div className="card-header">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-slate-100">Compare against baseline</h2>
                <p className="text-sm text-gray-500 dark:text-slate-400">
                  Pick a baseline saved for this test to see overall and per-client deltas.
                </p>
              </div>
              <div className="card-content space-y-3">
                <div className="flex items-center gap-2">
                  <label htmlFor="baseline-compare-select" className="text-sm text-gray-700 dark:text-slate-300">Baseline:</label>
                  <select
                    id="baseline-compare-select"
                    className="input max-w-md"
                    value={selectedBaselineName}
                    onChange={(e) => setSelectedBaselineName(e.target.value)}
                  >
                    <option value="">— Select a baseline —</option>
                    {baselinesForThisTest.map(b => (
                      <option key={b.id} value={b.name}>
                        {b.name}{b.git_branch ? ` (${b.git_branch})` : ''}
                      </option>
                    ))}
                  </select>
                  {selectedBaselineName && (
                    <button
                      type="button"
                      onClick={() => setSelectedBaselineName('')}
                      className="text-xs text-gray-500 dark:text-slate-400 hover:text-gray-700 dark:text-slate-300"
                    >
                      Clear
                    </button>
                  )}
                </div>

                {!selectedBaselineName && baselinesForThisTest.length === 0 && (
                  <p className="text-sm text-gray-500 dark:text-slate-400">
                    No baselines exist for this test yet — create one below.
                  </p>
                )}

                {selectedBaselineName && comparisonLoading && (
                  <LoadingSpinner size="md" />
                )}

                {selectedBaselineName && comparisonError && (
                  <div className="rounded border border-danger-200 bg-danger-50 p-3 text-sm text-danger-700">
                    Failed to load comparison: {(comparisonError as Error).message}
                  </div>
                )}

                {selectedBaselineName && !comparisonLoading && baselineComparison && (
                  <BaselineComparisonView comparison={baselineComparison} />
                )}
              </div>
            </div>

            {/* Baseline Manager */}
            <BaselineManager
              baselines={baselines}
              availableRuns={availableRuns}
              loading={setBaselineMutation.isPending || removeBaselineMutation.isPending}
              error={setBaselineMutation.error?.message || removeBaselineMutation.error?.message || null}
              onUpdate={async (action) => {
                if (action.type === 'create' && action.runId && action.baselineName) {
                  await setBaselineMutation.mutateAsync({ 
                    runId: action.runId, 
                    name: action.baselineName 
                  })
                } else if (action.type === 'delete' && action.baselineName) {
                  await removeBaselineMutation.mutateAsync(action.baselineName)
                }
              }}
            />
          </div>
        )}
      </div>
    </>
  )
}