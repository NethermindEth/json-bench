import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { Helmet } from 'react-helmet-async'
import { 
  CalendarIcon, 
  ClockIcon, 
  CheckCircleIcon, 
  ExclamationTriangleIcon,
  ChartBarIcon,
  MagnifyingGlassIcon,
  FunnelIcon,
  EyeIcon
} from '@heroicons/react/24/outline'
import {
  TrendChart,
  MetricCard,
  RegressionAlertList,
  ThroughputChart
} from '../components'
import Breadcrumb from '../components/Breadcrumb'
import type { HistoricRun, DashboardStats, TrendData } from '../types/api'
import { useRuns, useAPI } from '../api/hooks'
import { formatPercentage } from '../utils/metric-formatters'

export default function Dashboard() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [timeRange, setTimeRange] = useState(searchParams.get('timeRange') || '7d')
  const [searchQuery, setSearchQuery] = useState(searchParams.get('search') || '')
  const [filterBranch, setFilterBranch] = useState(searchParams.get('branch') || '')
  const [filterTest, setFilterTest] = useState(searchParams.get('test') || '')
  const [showFilters, setShowFilters] = useState(false)

  // Update URL parameters when filters change
  const updateSearchParams = (updates: Record<string, string>) => {
    const newParams = new URLSearchParams(searchParams)
    Object.entries(updates).forEach(([key, value]) => {
      if (value) {
        newParams.set(key, value)
      } else {
        newParams.delete(key)
      }
    })
    setSearchParams(newParams)
  }

  // Get API client for dashboard stats and trends (not covered by useRuns hook)
  const api = useAPI()

  // Use shared API hook for runs data
  const { data: recentRuns, isLoading: runsLoading, error: runsError } = useRuns({ 
    limit: 10,
    testName: filterTest || undefined,
    gitBranch: filterBranch || undefined,
    search: searchQuery || undefined 
  })

  // Get all runs for calculating stats
  const { data: allRuns, isLoading: allRunsLoading } = useRuns({ limit: 100 })

  // Calculate dashboard stats from runs data (fallback since /api/dashboard/stats doesn't exist)
  const stats = useMemo(() => {
    if (!allRuns || allRuns.length === 0) return null
    
    const totalRuns = allRuns.length
    const successfulRuns = allRuns.filter(run => run.success_rate > 95).length
    const successRate = Math.round((successfulRuns / totalRuns) * 100)
    const lastRun = allRuns[0] // API returns newest first
    const activeRegressions = 0 // TODO: Implement regression detection
    
    return {
      totalRuns,
      successRate,
      activeRegressions,
      lastRunTime: lastRun?.timestamp,
      avgLatencyTrend: 0 // TODO: Calculate trend
    }
  }, [allRuns])

  const statsLoading = allRunsLoading

  const { data: latencyTrend, error: trendError } = useQuery<TrendData>({
    queryKey: ['latency-trend', timeRange],
    queryFn: () => api.getTrends({ 
      metric: 'avg_latency',
      period: timeRange,
      limit: 30 
    }),
    retry: false,
  })

  const { data: regressions, error: regressionsError } = useQuery({
    queryKey: ['active-regressions'],
    queryFn: async () => {
      // Get recent run and check for regressions
      const response = await api.listRuns({ limit: 1 })
      if (!response.runs || response.runs.length === 0) return []
      // For now, return empty array since regression API doesn't exist
      // TODO: Implement regression detection
      return []
    },
    retry: false,
  })

  // Filter runs based on search and filters
  const filteredRuns = useMemo(() => {
    if (!recentRuns) return []
    
    return recentRuns.filter(run => {
      const matchesSearch = !searchQuery || 
        run.test_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        run.git_commit.toLowerCase().includes(searchQuery.toLowerCase()) ||
        run.id.toLowerCase().includes(searchQuery.toLowerCase())
      
      const matchesBranch = !filterBranch || run.git_branch === filterBranch
      const matchesTest = !filterTest || run.test_name === filterTest
      
      return matchesSearch && matchesBranch && matchesTest
    })
  }, [recentRuns, searchQuery, filterBranch, filterTest])

  if (statsLoading) {
    return (
      <div className="p-6">
        <div className="animate-pulse">
          <div className="h-8 bg-gray-200 rounded w-1/4 mb-6"></div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="card p-6">
                <div className="h-4 bg-gray-200 rounded w-1/2 mb-4"></div>
                <div className="h-8 bg-gray-200 rounded w-3/4"></div>
              </div>
            ))}
          </div>
        </div>
      </div>
    )
  }

  return (
    <>
      <Helmet>
        <title>Dashboard</title>
        <meta name="description" content="JSON-RPC benchmark performance dashboard showing recent runs, trends, and regressions" />
      </Helmet>
      
      <div className="p-6">
        <Breadcrumb />
        
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900 mb-2">
            Performance Dashboard
          </h1>
          <p className="text-gray-600">
            Track JSON-RPC benchmark performance over time
          </p>
        </div>

        {/* Controls */}
        <div className="mb-6 space-y-4">
          {/* Time Range and Filters */}
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
            {/* Time Range Selector */}
            <div className="flex space-x-2">
              {['24h', '7d', '30d', '90d'].map((range) => (
                <button
                  key={range}
                  onClick={() => {
                    setTimeRange(range)
                    updateSearchParams({ timeRange: range })
                  }}
                  className={`px-3 py-1 text-sm rounded-md transition-colors ${
                    timeRange === range
                      ? 'bg-primary-100 text-primary-700 border border-primary-300'
                      : 'bg-white text-gray-600 border border-gray-300 hover:bg-gray-50'
                  }`}
                >
                  {range}
                </button>
              ))}
            </div>

            {/* Search and Filter Toggle */}
            <div className="flex items-center space-x-2">
              <button
                onClick={() => setShowFilters(!showFilters)}
                className={`btn-outline text-sm ${
                  showFilters ? 'bg-primary-50 border-primary-300' : ''
                }`}
                aria-expanded={showFilters}
                aria-controls="dashboard-filters"
              >
                <FunnelIcon className="h-4 w-4 mr-1" />
                Filters
              </button>
            </div>
          </div>

          {/* Search and Filters */}
          {showFilters && (
            <div id="dashboard-filters" className="card p-4">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div>
                  <label htmlFor="search" className="label">
                    Search Runs
                  </label>
                  <div className="relative">
                    <MagnifyingGlassIcon className="h-4 w-4 absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400" />
                    <input
                      id="search"
                      type="text"
                      value={searchQuery}
                      onChange={(e) => {
                        setSearchQuery(e.target.value)
                        updateSearchParams({ search: e.target.value })
                      }}
                      placeholder="Search by test name, commit, or run ID..."
                      className="input pl-10"
                    />
                  </div>
                </div>
                
                <div>
                  <label htmlFor="branch-filter" className="label">
                    Git Branch
                  </label>
                  <select
                    id="branch-filter"
                    value={filterBranch}
                    onChange={(e) => {
                      setFilterBranch(e.target.value)
                      updateSearchParams({ branch: e.target.value })
                    }}
                    className="input"
                  >
                    <option value="">All branches</option>
                    <option value="main">main</option>
                    <option value="develop">develop</option>
                    <option value="release/v2.1">release/v2.1</option>
                  </select>
                </div>
                
                <div>
                  <label htmlFor="test-filter" className="label">
                    Test Type
                  </label>
                  <select
                    id="test-filter"
                    value={filterTest}
                    onChange={(e) => {
                      setFilterTest(e.target.value)
                      updateSearchParams({ test: e.target.value })
                    }}
                    className="input"
                  >
                    <option value="">All tests</option>
                    <option value="Standard RPC Benchmark">Standard RPC Benchmark</option>
                    <option value="Load Test">Load Test</option>
                    <option value="Stress Test">Stress Test</option>
                  </select>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <MetricCard
            title="Total Runs"
            value={stats?.totalRuns?.toString() || '0'}
            icon={ChartBarIcon}
            trend={stats?.avgLatencyTrend ? (stats.avgLatencyTrend > 0 ? "up" : stats.avgLatencyTrend < 0 ? "down" : "stable") : undefined}
          />
          
          <MetricCard
            title="Success Rate"
            value={`${stats?.successRate || 0}%`}
            icon={CheckCircleIcon}
            variant="success"
          />
          
          <MetricCard
            title="Active Regressions"
            value={stats?.activeRegressions?.toString() || '0'}
            icon={ExclamationTriangleIcon}
            variant={stats?.activeRegressions && stats.activeRegressions > 0 ? "danger" : "success"}
            onClick={undefined}
          />
          
          <MetricCard
            title="Last Run"
            value={stats?.lastRunTime ? new Date(stats.lastRunTime).toLocaleDateString() : 'N/A'}
            icon={ClockIcon}
            subtitle={stats?.lastRunTime ? new Date(stats.lastRunTime).toLocaleTimeString() : undefined}
          />
        </div>

        {/* Active Regressions */}
        {regressions && regressions.length > 0 && (
          <div className="mb-8">
            <RegressionAlertList 
              regressions={regressions}
            />
          </div>
        )}

        {/* Recent Runs */}
        <div className="card">
          <div className="card-header flex items-center justify-between">
            <h2 className="text-lg font-semibold text-gray-900">
              Recent Benchmark Runs
              {filteredRuns.length !== recentRuns?.length && (
                <span className="ml-2 text-sm text-gray-500">
                  ({filteredRuns.length} of {recentRuns?.length} shown)
                </span>
              )}
            </h2>
          </div>
          <div className="card-content p-0">
            {runsLoading ? (
              <div className="p-6">
                <div className="animate-pulse space-y-4">
                  {[...Array(3)].map((_, i) => (
                    <div key={i} className="flex items-center space-x-4">
                      <div className="h-4 bg-gray-200 rounded w-1/4"></div>
                      <div className="h-4 bg-gray-200 rounded w-1/4"></div>
                      <div className="h-4 bg-gray-200 rounded w-1/4"></div>
                      <div className="h-4 bg-gray-200 rounded w-1/4"></div>
                    </div>
                  ))}
                </div>
              </div>
            ) : filteredRuns.length === 0 ? (
              <div className="text-center py-12">
                <ChartBarIcon className="h-12 w-12 text-gray-400 mx-auto mb-4" />
                <h3 className="text-lg font-medium text-gray-900 mb-2">
                  No runs found
                </h3>
                <p className="text-gray-500">
                  {searchQuery || filterBranch || filterTest
                    ? 'No runs match your current filters. Try adjusting your search criteria.'
                    : 'No benchmark runs have been completed yet.'}
                </p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table">
                  <thead className="table-header">
                    <tr>
                      <th className="table-header-cell">Time</th>
                      <th className="table-header-cell">Test Name</th>
                      <th className="table-header-cell">Git Info</th>
                      <th className="table-header-cell">Avg Latency</th>
                      <th className="table-header-cell">Success Rate</th>
                      <th className="table-header-cell">Status</th>
                      <th className="table-header-cell">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {filteredRuns.map((run) => (
                      <tr key={run.id} className="table-row hover:bg-gray-50">
                        <td className="table-cell">
                          <div className="flex items-center">
                            <CalendarIcon className="h-4 w-4 text-gray-400 mr-2" />
                            <time dateTime={run.timestamp}>
                              {new Date(run.timestamp).toLocaleString()}
                            </time>
                          </div>
                        </td>
                        <td className="table-cell">
                          <Link 
                            to={`/runs/${run.id}`}
                            className="font-medium text-primary-600 hover:text-primary-700"
                          >
                            {run.test_name}
                          </Link>
                        </td>
                        <td className="table-cell">
                          <div className="text-sm">
                            <div className="font-mono text-gray-900">{run.git_commit}</div>
                            <div className="text-gray-500">{run.git_branch}</div>
                          </div>
                        </td>
                        <td className="table-cell">
                          <span className={`font-medium ${
                            run.avg_latency_ms > 300 ? 'text-danger-600' :
                            run.avg_latency_ms > 200 ? 'text-warning-600' : 'text-success-600'
                          }`}>
                            {run.avg_latency_ms.toFixed(1)}ms
                          </span>
                        </td>
                        <td className="table-cell">
                          <span className={`badge ${
                            run.success_rate >= 99 ? 'badge-success' :
                            run.success_rate >= 95 ? 'badge-warning' : 'badge-danger'
                          }`}>
                            {formatPercentage(run.success_rate)}
                          </span>
                        </td>
                        <td className="table-cell">
                          <div className="flex items-center space-x-2">
                            {run.is_baseline && (
                              <span className="badge badge-info">Baseline</span>
                            )}
                            {!run.is_baseline && (
                              <span className="badge badge-success">Normal</span>
                            )}
                          </div>
                        </td>
                        <td className="table-cell">
                          <Link
                            to={`/runs/${run.id}`}
                            className="text-gray-400 hover:text-primary-600 transition-colors"
                            title="View details"
                          >
                            <EyeIcon className="h-4 w-4" />
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Performance Charts */}
        <div className="mt-8 grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div className="card">
            <div className="card-header">
              <h3 className="text-lg font-semibold text-gray-900">Latency Trends</h3>
              <p className="text-sm text-gray-500">Average response time over {timeRange}</p>
            </div>
            <div className="card-content">
              {latencyTrend ? (
                <TrendChart
                  data={latencyTrend.trendPoints}
                  title="Average Latency"
                  metric="latency"
                  height={300}
                />
              ) : (
                <div className="h-64 flex items-center justify-center bg-gray-50 rounded-lg">
                  <p className="text-gray-500">Loading trend data...</p>
                </div>
              )}
            </div>
          </div>

          <div className="card">
            <div className="card-header">
              <h3 className="text-lg font-semibold text-gray-900">Throughput Trends</h3>
              <p className="text-sm text-gray-500">Requests per second over {timeRange}</p>
            </div>
            <div className="card-content">
              {latencyTrend ? (
                <ThroughputChart
                  data={latencyTrend.trendPoints.map(point => ({
                    timestamp: point.timestamp,
                    throughput: 1000 / point.value, // Convert latency to rough throughput
                    totalRequests: 10000,
                    duration: 5 * 60, // 5 minutes in seconds
                  }))}
                  height={300}
                />
              ) : (
                <div className="h-64 flex items-center justify-center bg-gray-50 rounded-lg">
                  <p className="text-gray-500">Loading throughput data...</p>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </>
  )
}