import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams, Link } from 'react-router-dom'
import { Helmet } from 'react-helmet-async'
import {
  ScaleIcon,
  DocumentArrowDownIcon,
  ArrowsRightLeftIcon,
  ChevronUpDownIcon
} from '@heroicons/react/24/outline'
import {
  LoadingSpinner,
  ComparisonView,
  LatencyDistribution
} from '../components'
import Breadcrumb from '../components/Breadcrumb'
import type { HistoricRun, ComparisonResponse } from '../types/api'
import { createBenchmarkAPI } from '../api/client'

// Initialize API client
const api = createBenchmarkAPI(import.meta.env.VITE_API_BASE_URL || 'http://localhost:8082')

export default function Compare() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [run1Id, setRun1Id] = useState(searchParams.get('run1') || '')
  const [run2Id, setRun2Id] = useState(searchParams.get('run2') || '')
  const [sortBy, setSortBy] = useState<'timestamp' | 'testName' | 'branch'>('timestamp')
  const [filterBranch, setFilterBranch] = useState('')
  
  // Update URL params when selection changes
  useEffect(() => {
    const params = new URLSearchParams()
    if (run1Id) params.set('run1', run1Id)
    if (run2Id) params.set('run2', run2Id)
    setSearchParams(params)
  }, [run1Id, run2Id, setSearchParams])

  const { data: availableRuns, isLoading: runsLoading } = useQuery({
    queryKey: ['available-runs'],
    queryFn: () => api.listRuns({ limit: 50 }),
    retry: false,
  })

  const { data: comparisonData, isLoading: comparisonLoading } = useQuery<ComparisonResponse>({
    queryKey: ['comparison', run1Id, run2Id],
    queryFn: () => {
      if (!run1Id || !run2Id) throw new Error('Missing run IDs')
      return api.compareRuns(run1Id, run2Id)
    },
    enabled: !!(run1Id && run2Id),
  })
  
  // Filter and sort available runs
  const filteredRuns = availableRuns?.filter(run => 
    !filterBranch || (run as HistoricRun).gitBranch === filterBranch
  ).sort((a, b) => {
    switch (sortBy) {
      case 'testName':
        return a.testName.localeCompare(b.testName)
      case 'branch':
        return (a as HistoricRun).gitBranch.localeCompare((b as HistoricRun).gitBranch)
      case 'timestamp':
      default:
        return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    }
  })
  
  const swapRuns = () => {
    const temp = run1Id
    setRun1Id(run2Id)
    setRun2Id(temp)
  }
  
  const exportComparison = () => {
    if (!comparisonData) return
    
    const data = {
      comparison: comparisonData,
      exportedAt: new Date().toISOString(),
      metadata: {
        tool: 'JSON-RPC Benchmark Dashboard',
        version: '1.0.0',
      },
    }
    
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `comparison-${run1Id}-vs-${run2Id}.json`
    a.click()
    URL.revokeObjectURL(url)
  }


  if (runsLoading) {
    return (
      <div className="p-6">
        <LoadingSpinner size="lg" className="h-64" />
      </div>
    )
  }

  return (
    <>
      <Helmet>
        <title>Compare Runs</title>
        <meta name="description" content="Compare benchmark runs side-by-side to analyze performance differences" />
      </Helmet>
      
      <div className="p-6">
        <Breadcrumb />
        
        {/* Header */}
        <div className="mb-8">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-gray-900 mb-2">
                Compare Benchmark Runs
              </h1>
              <p className="text-gray-600">
                Select two benchmark runs to compare their performance metrics
              </p>
            </div>
            {comparisonData && (
              <button
                onClick={exportComparison}
                className="btn-outline"
              >
                <DocumentArrowDownIcon className="h-4 w-4 mr-2" />
                Export Comparison
              </button>
            )}
          </div>
        </div>

        {/* Run Selection */}
        <div className="card mb-8">
          <div className="card-header">
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-semibold text-gray-900 flex items-center">
                <ScaleIcon className="h-5 w-5 mr-2" />
                Select Runs to Compare
              </h2>
              {run1Id && run2Id && (
                <button
                  onClick={swapRuns}
                  className="btn-outline text-sm"
                  title="Swap runs"
                >
                  <ArrowsRightLeftIcon className="h-4 w-4 mr-1" />
                  Swap
                </button>
              )}
            </div>
          </div>
          <div className="card-content">
            {/* Filters */}
            <div className="mb-6 grid grid-cols-1 md:grid-cols-3 gap-4">
              <div>
                <label className="label">Filter by Branch</label>
                <select
                  value={filterBranch}
                  onChange={(e) => setFilterBranch(e.target.value)}
                  className="input"
                >
                  <option value="">All branches</option>
                  <option value="main">main</option>
                  <option value="develop">develop</option>
                  <option value="release/v2.1">release/v2.1</option>
                </select>
              </div>
              <div>
                <label className="label">Sort by</label>
                <select
                  value={sortBy}
                  onChange={(e) => setSortBy(e.target.value as any)}
                  className="input"
                >
                  <option value="timestamp">Recent First</option>
                  <option value="testName">Test Name</option>
                  <option value="branch">Branch</option>
                </select>
              </div>
            </div>
            
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              {/* Run 1 Selection */}
              <div>
                <label className="label">Baseline Run</label>
                <select
                  value={run1Id}
                  onChange={(e) => setRun1Id(e.target.value)}
                  className="input"
                  aria-label="Select baseline run"
                >
                  <option value="">Select a run...</option>
                  {filteredRuns?.map((run) => (
                    <option key={run.id} value={run.id}>
                      {run.testName} - {new Date(run.timestamp).toLocaleDateString()} ({run.gitCommit}) [{run.gitBranch}]
                    </option>
                  ))}
                </select>
                {run1Id && (
                  <div className="mt-2 text-sm text-gray-600">
                    <Link to={`/runs/${run1Id}`} className="text-primary-600 hover:text-primary-700">
                      View details →
                    </Link>
                  </div>
                )}
              </div>

              {/* Run 2 Selection */}
              <div>
                <label className="label">Comparison Run</label>
                <select
                  value={run2Id}
                  onChange={(e) => setRun2Id(e.target.value)}
                  className="input"
                  aria-label="Select comparison run"
                >
                  <option value="">Select a run...</option>
                  {filteredRuns?.map((run) => (
                    <option key={run.id} value={run.id} disabled={run.id === run1Id}>
                      {run.testName} - {new Date(run.timestamp).toLocaleDateString()} ({run.gitCommit}) [{run.gitBranch}]
                    </option>
                  ))}
                </select>
                {run2Id && (
                  <div className="mt-2 text-sm text-gray-600">
                    <Link to={`/runs/${run2Id}`} className="text-primary-600 hover:text-primary-700">
                      View details →
                    </Link>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

      {/* Comparison Results */}
      {run1Id && run2Id && (
        <>
          {comparisonLoading ? (
            <div className="card">
              <div className="card-content">
                <LoadingSpinner size="lg" className="h-32" />
              </div>
            </div>
          ) : comparisonData ? (
            <div className="space-y-8">
              {/* Run Overview */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div className="card">
                  <div className="card-header">
                    <h3 className="text-lg font-semibold text-gray-900">Baseline Run</h3>
                  </div>
                  <div className="card-content">
                    <dl className="space-y-2">
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Test Name</dt>
                        <dd className="text-sm text-gray-900">{comparisonData.run1.testName}</dd>
                      </div>
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Timestamp</dt>
                        <dd className="text-sm text-gray-900">
                          {new Date(comparisonData.run1.timestamp).toLocaleString()}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Git Commit</dt>
                        <dd className="text-sm text-gray-900 font-mono">{comparisonData.run1.gitCommit}</dd>
                      </div>
                    </dl>
                  </div>
                </div>

                <div className="card">
                  <div className="card-header">
                    <h3 className="text-lg font-semibold text-gray-900">Comparison Run</h3>
                  </div>
                  <div className="card-content">
                    <dl className="space-y-2">
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Test Name</dt>
                        <dd className="text-sm text-gray-900">{comparisonData.run2.testName}</dd>
                      </div>
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Timestamp</dt>
                        <dd className="text-sm text-gray-900">
                          {new Date(comparisonData.run2.timestamp).toLocaleString()}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-sm font-medium text-gray-500">Git Commit</dt>
                        <dd className="text-sm text-gray-900 font-mono">{comparisonData.run2.gitCommit}</dd>
                      </div>
                    </dl>
                  </div>
                </div>
              </div>

              {/* Summary */}
              {comparisonData.summary && (
                <div className={`card border-l-4 ${
                  comparisonData.summary.includes('improved') 
                    ? 'border-success-500 bg-success-50'
                    : comparisonData.summary.includes('degraded')
                    ? 'border-danger-500 bg-danger-50'
                    : 'border-gray-500 bg-gray-50'
                } mb-6`}>
                  <div className="card-content">
                    <h3 className="font-semibold text-gray-900 mb-2">Comparison Summary</h3>
                    <p className="text-gray-700">{comparisonData.summary}</p>
                  </div>
                </div>
              )}

              {/* Enhanced Comparison View */}
              <ComparisonView
                run1={comparisonData.run1}
                run2={comparisonData.run2}
              />

              {/* Visual Comparison Charts */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mt-8">
                <div className="card">
                  <div className="card-header">
                    <h3 className="text-lg font-semibold text-gray-900">Latency Comparison</h3>
                  </div>
                  <div className="card-content">
                    <LatencyDistribution
                      data={[
                        { min: 0, max: comparisonData.run1.avgLatency * 0.8, count: 5000, percentage: 50 },
                        { min: comparisonData.run1.avgLatency * 0.8, max: comparisonData.run1.avgLatency * 1.2, count: 2500, percentage: 25 },
                        { min: comparisonData.run1.avgLatency * 1.2, max: comparisonData.run1.avgLatency * 2, count: 2000, percentage: 20 },
                        { min: comparisonData.run1.avgLatency * 2, max: Infinity, count: 500, percentage: 5 },
                      ]}
                      title={`Baseline: ${comparisonData.run1.testName}`}
                    />
                  </div>
                </div>
                
                <div className="card">
                  <div className="card-header">
                    <h3 className="text-lg font-semibold text-gray-900">Performance Metrics</h3>
                  </div>
                  <div className="card-content">
                    <div className="space-y-4">
                      {Object.entries(comparisonData.metrics).map(([metric, values]) => {
                        const isImprovement = metric.includes('successRate') || metric.includes('throughput')
                          ? values.delta > 0
                          : values.delta < 0
                        
                        return (
                          <div key={metric} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                            <div>
                              <div className="font-medium text-gray-900 capitalize">
                                {metric.replace(/([A-Z])/g, ' $1').trim()}
                              </div>
                              <div className="text-sm text-gray-500">
                                {values.run1.toFixed(metric.includes('Rate') ? 1 : 2)}
                                {metric.includes('Rate') ? '%' : metric.includes('Latency') ? 'ms' : ''}
                                {' → '}
                                {values.run2.toFixed(metric.includes('Rate') ? 1 : 2)}
                                {metric.includes('Rate') ? '%' : metric.includes('Latency') ? 'ms' : ''}
                              </div>
                            </div>
                            <div className={`text-right font-semibold ${
                              isImprovement ? 'text-success-600' : 'text-danger-600'
                            }`}>
                              {values.percentChange > 0 ? '+' : ''}
                              {values.percentChange.toFixed(1)}%
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <div className="card">
              <div className="card-content text-center py-8">
                <p className="text-gray-500">Failed to load comparison data</p>
              </div>
            </div>
          )}
        </>
      )}

        {/* Empty State */}
        {(!run1Id || !run2Id) && (
          <div className="card">
            <div className="card-content text-center py-12">
              <ScaleIcon className="h-12 w-12 text-gray-400 mx-auto mb-4" />
              <h3 className="text-lg font-medium text-gray-900 mb-2">
                Select Two Runs to Compare
              </h3>
              <p className="text-gray-500 mb-6">
                Choose a baseline run and a comparison run from the dropdowns above to see detailed performance comparisons.
              </p>
              <div className="flex justify-center space-x-4 text-sm text-gray-400">
                <div className="flex items-center">
                  <ChevronUpDownIcon className="h-4 w-4 mr-1" />
                  Sort and filter runs
                </div>
                <div className="flex items-center">
                  <ArrowsRightLeftIcon className="h-4 w-4 mr-1" />
                  Swap runs easily
                </div>
                <div className="flex items-center">
                  <DocumentArrowDownIcon className="h-4 w-4 mr-1" />
                  Export comparisons
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </>
  )
}