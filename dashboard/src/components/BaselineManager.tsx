import { useState, useMemo } from 'react'
import {
  FlagIcon,
  PlusIcon,
  TrashIcon,
  PencilIcon,
  MagnifyingGlassIcon,
  CalendarIcon,
  CheckIcon,
  XMarkIcon,
  ArrowsRightLeftIcon,
  ExclamationTriangleIcon,
} from '@heroicons/react/24/outline'
import { HistoricRun } from '../types/api'
import LoadingSpinner from './LoadingSpinner'
import { formatPercentage, formatLatency } from '../utils/metric-formatters'

export interface BaselineManagerProps {
  baselines: HistoricRun[]
  availableRuns: HistoricRun[]
  onUpdate: (action: BaselineAction, data: any) => Promise<void>
  loading?: boolean
  error?: string | null
  className?: string
}

export interface BaselineAction {
  type: 'create' | 'update' | 'delete' | 'compare'
  runId?: string
  baselineName?: string
  newName?: string
}

interface NewBaselineForm {
  runId: string
  name: string
}

interface EditBaselineForm {
  baselineId: string
  newName: string
}

// Helper to safely get fields from baselines that might be HistoricRun or Baseline objects
// NOTE: These helpers handle both camelCase and snake_case field names due to API response
// inconsistencies. This is a defensive workaround that masks an underlying API design issue.
// TODO: The backend API should be standardized to use a consistent naming convention
// (either always camelCase for JavaScript/TypeScript frontends or always snake_case throughout)
// to eliminate this complexity and potential for bugs. Once standardized, these helper
// functions can be removed and replaced with direct field access.
function getBaselineField(baseline: any, camelCase: string, snakeCase: string, defaultValue = ''): any {
  return baseline[camelCase] ?? baseline[snakeCase] ?? defaultValue
}

// Helper to get metrics from baseline_metrics nested object
function getBaselineMetric(baseline: any, metricName: string, defaultValue: number = 0): number {
  // Try baseline_metrics (snake_case from API)
  const metrics = baseline.baseline_metrics ?? baseline.baselineMetrics ?? {}
  
  // Map common metric names to their possible field names
  const metricMappings: Record<string, string[]> = {
    'avgLatency': ['avg_latency_ms', 'avgLatencyMs', 'avg_latency'],
    'successRate': ['success_rate', 'successRate'],
    'errorRate': ['overall_error_rate', 'overallErrorRate', 'error_rate', 'errorRate'],
    'p95Latency': ['p95_latency_ms', 'p95LatencyMs', 'p95_latency'],
    'p99Latency': ['p99_latency_ms', 'p99LatencyMs', 'p99_latency'],
  }
  
  const possibleNames = metricMappings[metricName] || [metricName]
  
  for (const name of possibleNames) {
    if (metrics[name] !== undefined && metrics[name] !== null) {
      return Number(metrics[name])
    }
  }
  
  // Also check top-level fields (for HistoricRun objects used as baselines)
  for (const name of possibleNames) {
    if (baseline[name] !== undefined && baseline[name] !== null) {
      return Number(baseline[name])
    }
  }
  
  return defaultValue
}

/**
 * BaselineManager component provides CRUD operations for performance baselines
 * including creation from existing runs, editing, deletion, and comparison functionality
 */
export default function BaselineManager({
  baselines,
  availableRuns,
  onUpdate,
  loading = false,
  error = null,
  className = '',
}: BaselineManagerProps) {
  const [searchTerm, setSearchTerm] = useState('')
  const [sortBy, setSortBy] = useState<'name' | 'timestamp' | 'testName'>('timestamp')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')
  const [selectedBaselines, setSelectedBaselines] = useState<Set<string>>(new Set())
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [editingBaseline, setEditingBaseline] = useState<string | null>(null)
  const [newBaselineForm, setNewBaselineForm] = useState<NewBaselineForm>({
    runId: '',
    name: '',
  })
  const [editForm, setEditForm] = useState<EditBaselineForm>({
    baselineId: '',
    newName: '',
  })
  const [actionLoading, setActionLoading] = useState<string | null>(null)

  // Filter and sort baselines
  // Note: baselines can be either HistoricRun[] or Baseline[] objects from the API
  const filteredBaselines = useMemo(() => {
    if (!Array.isArray(baselines)) return []
    
    const searchLower = searchTerm.toLowerCase()
    const filtered = baselines.filter(baseline => {
      // Handle both camelCase (HistoricRun) and snake_case (Baseline) field names
      const name = (baseline as any).baselineName || (baseline as any).name || ''
      const testName = (baseline as any).testName || (baseline as any).test_name || ''
      const gitBranch = (baseline as any).gitBranch || (baseline as any).git_branch || ''
      
      return name.toLowerCase().includes(searchLower) ||
        testName.toLowerCase().includes(searchLower) ||
        gitBranch.toLowerCase().includes(searchLower)
    })

    return filtered.sort((a, b) => {
      let comparison = 0
      const aName = (a as any).baselineName || (a as any).name || ''
      const bName = (b as any).baselineName || (b as any).name || ''
      const aTestName = (a as any).testName || (a as any).test_name || ''
      const bTestName = (b as any).testName || (b as any).test_name || ''
      const aTimestamp = (a as any).timestamp || (a as any).created_at || ''
      const bTimestamp = (b as any).timestamp || (b as any).created_at || ''
      
      switch (sortBy) {
        case 'name':
          comparison = aName.localeCompare(bName)
          break
        case 'timestamp':
          comparison = new Date(aTimestamp).getTime() - new Date(bTimestamp).getTime()
          break
        case 'testName':
          comparison = aTestName.localeCompare(bTestName)
          break
      }
      
      return sortOrder === 'desc' ? -comparison : comparison
    })
  }, [baselines, searchTerm, sortBy, sortOrder])

  // Filter available runs (excluding those already baselines)
  const availableForBaseline = useMemo(() => {
    const baselineRunIds = new Set(baselines.map(b => b.id))
    return availableRuns.filter(run => !baselineRunIds.has(run.id))
  }, [availableRuns, baselines])

  const handleSort = (newSortBy: typeof sortBy) => {
    if (sortBy === newSortBy) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc')
    } else {
      setSortBy(newSortBy)
      setSortOrder('desc')
    }
  }

  const handleSelectBaseline = (baselineId: string) => {
    const newSelected = new Set(selectedBaselines)
    if (newSelected.has(baselineId)) {
      newSelected.delete(baselineId)
    } else {
      newSelected.add(baselineId)
    }
    setSelectedBaselines(newSelected)
  }

  const handleSelectAll = () => {
    if (selectedBaselines.size === filteredBaselines.length) {
      setSelectedBaselines(new Set())
    } else {
      setSelectedBaselines(new Set(filteredBaselines.map(b => b.id)))
    }
  }

  const handleCreateBaseline = async () => {
    if (!newBaselineForm.runId || !newBaselineForm.name.trim()) return

    setActionLoading('create')
    try {
      await onUpdate({
        type: 'create',
        runId: newBaselineForm.runId,
        baselineName: newBaselineForm.name.trim(),
      }, null)

      setNewBaselineForm({ runId: '', name: '' })
      setShowCreateForm(false)
    } catch (error) {
      console.error('Failed to create baseline:', error)
    } finally {
      setActionLoading(null)
    }
  }

  const handleUpdateBaseline = async () => {
    if (!editForm.baselineId || !editForm.newName.trim()) return

    setActionLoading(`edit-${editForm.baselineId}`)
    try {
      await onUpdate({
        type: 'update',
        runId: editForm.baselineId,
        newName: editForm.newName.trim(),
      }, null)

      setEditForm({ baselineId: '', newName: '' })
      setEditingBaseline(null)
    } catch (error) {
      console.error('Failed to update baseline:', error)
    } finally {
      setActionLoading(null)
    }
  }

  const handleDeleteBaseline = async (baseline: any) => {
    const baselineName = getBaselineField(baseline, 'baselineName', 'name')
    if (!confirm(`Are you sure you want to delete baseline "${baselineName}"? This action cannot be undone.`)) {
      return
    }

    setActionLoading(`delete-${baseline.id}`)
    try {
      await onUpdate({
        type: 'delete',
        runId: baseline.id,
        baselineName: baselineName, // Pass the name for the backend
      }, null)
    } catch (error) {
      console.error('Failed to delete baseline:', error)
    } finally {
      setActionLoading(null)
    }
  }

  const handleDeleteSelected = async () => {
    if (selectedBaselines.size === 0) return

    const count = selectedBaselines.size
    if (!confirm(`Are you sure you want to delete ${count} baseline${count > 1 ? 's' : ''}? This action cannot be undone.`)) {
      return
    }

    setActionLoading('delete-multiple')
    try {
      for (const baselineId of selectedBaselines) {
        await onUpdate({
          type: 'delete',
          runId: baselineId,
        }, null)
      }
      setSelectedBaselines(new Set())
    } catch (error) {
      console.error('Failed to delete baselines:', error)
    } finally {
      setActionLoading(null)
    }
  }

  const handleCompareBaselines = () => {
    if (selectedBaselines.size !== 2) return

    const [baseline1, baseline2] = Array.from(selectedBaselines)
    onUpdate({
      type: 'compare',
      runId: baseline1,
      baselineName: baseline2,
    }, null)
  }

  const startEditing = (baseline: HistoricRun) => {
    setEditingBaseline(baseline.id)
    setEditForm({
      baselineId: baseline.id,
      newName: getBaselineField(baseline, 'baselineName', 'name'),
    })
  }

  const cancelEditing = () => {
    setEditingBaseline(null)
    setEditForm({ baselineId: '', newName: '' })
  }

  const formatTimestamp = (timestamp: string): string => {
    return new Date(timestamp).toLocaleString()
  }

  if (loading) {
    return (
      <div className={`card ${className}`}>
        <div className="card-content">
          <LoadingSpinner size="lg" className="py-8" />
          <p className="text-center text-gray-500 mt-4">Loading baselines...</p>
        </div>
      </div>
    )
  }

  return (
    <div className={`space-y-6 ${className}`}>
      {/* Header */}
      <div className="card">
        <div className="card-header">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2">
              <FlagIcon className="h-6 w-6 text-primary-600" />
              <h2 className="text-xl font-bold text-gray-900">Baseline Management</h2>
              <span className="badge badge-info">{baselines.length}</span>
            </div>
            <button
              onClick={() => setShowCreateForm(true)}
              className="btn btn-primary"
              disabled={availableForBaseline.length === 0}
            >
              <PlusIcon className="h-4 w-4 mr-2" />
              New Baseline
            </button>
          </div>
        </div>
      </div>

      {/* Error Display */}
      {error && (
        <div className="bg-danger-50 border border-danger-200 rounded-lg p-4">
          <div className="flex items-center">
            <ExclamationTriangleIcon className="h-5 w-5 text-danger-500 mr-2" />
            <span className="text-danger-800">{error}</span>
          </div>
        </div>
      )}

      {/* Create Baseline Form */}
      {showCreateForm && (
        <div className="card">
          <div className="card-header">
            <h3 className="text-lg font-semibold text-gray-900">Create New Baseline</h3>
          </div>
          <div className="card-content">
            <div className="space-y-4">
              <div>
                <label htmlFor="run-select" className="label">Select Run</label>
                <select
                  id="run-select"
                  value={newBaselineForm.runId}
                  onChange={(e) => setNewBaselineForm(prev => ({ ...prev, runId: e.target.value }))}
                  className="input"
                >
                  <option value="">Choose a run...</option>
                  {availableForBaseline.map((run) => {
                    // Handle both camelCase and snake_case field names
                    const runAny = run as any
                    const testName = runAny.testName || runAny.test_name || 'Unnamed Test'
                    const branch = runAny.gitBranch || runAny.git_branch || 'no branch'
                    const latency = runAny.avgLatencyMs ?? runAny.avg_latency_ms ?? runAny.avgLatency
                    const latencyStr = latency ? `${Number(latency).toFixed(1)}ms` : ''
                    const rate = runAny.successRate ?? runAny.success_rate
                    const successRateStr = rate !== undefined ? `${Number(rate).toFixed(1)}%` : ''
                    const metrics = [latencyStr, successRateStr].filter(Boolean).join(', ')
                    
                    return (
                      <option key={run.id} value={run.id}>
                        {testName} • {formatTimestamp(run.timestamp)} • {branch}{metrics ? ` • ${metrics}` : ''}
                      </option>
                    )
                  })}
                </select>
                
                {/* Selected run preview */}
                {newBaselineForm.runId && (() => {
                  const selectedRun = availableForBaseline.find(r => r.id === newBaselineForm.runId)
                  if (!selectedRun) return null
                  
                  // Handle both camelCase and snake_case field names
                  const runAny = selectedRun as any
                  const testName = runAny.testName || runAny.test_name || 'Unnamed'
                  const branch = runAny.gitBranch || runAny.git_branch || 'N/A'
                  const latency = runAny.avgLatencyMs ?? runAny.avg_latency_ms ?? runAny.avgLatency ?? 0
                  const rate = runAny.successRate ?? runAny.success_rate
                  const totalReqs = runAny.totalRequests ?? runAny.total_requests
                  
                  return (
                    <div className="mt-3 p-3 bg-gray-50 rounded-lg border border-gray-200">
                      <div className="text-sm font-medium text-gray-700 mb-2">Selected Run Details</div>
                      <div className="grid grid-cols-2 gap-2 text-sm">
                        <div>
                          <span className="text-gray-500">Test:</span>
                          <span className="ml-2 font-medium">{testName}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">Branch:</span>
                          <span className="ml-2 font-medium">{branch}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">Date:</span>
                          <span className="ml-2 font-medium">{formatTimestamp(selectedRun.timestamp)}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">Avg Latency:</span>
                          <span className="ml-2 font-medium">{formatLatency(latency)}</span>
                        </div>
                        {rate !== undefined && (
                          <div>
                            <span className="text-gray-500">Success Rate:</span>
                            <span className="ml-2 font-medium">{formatPercentage(rate)}</span>
                          </div>
                        )}
                        {totalReqs !== undefined && (
                          <div>
                            <span className="text-gray-500">Total Requests:</span>
                            <span className="ml-2 font-medium">{Number(totalReqs).toLocaleString()}</span>
                          </div>
                        )}
                      </div>
                    </div>
                  )
                })()}
              </div>

              <div>
                <label htmlFor="baseline-name" className="label">Baseline Name</label>
                <input
                  id="baseline-name"
                  type="text"
                  value={newBaselineForm.name}
                  onChange={(e) => setNewBaselineForm(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="e.g., Production v2.1.0"
                  className="input"
                />
              </div>

              <div className="flex justify-end space-x-2">
                <button
                  onClick={() => setShowCreateForm(false)}
                  className="btn btn-secondary"
                  disabled={actionLoading === 'create'}
                >
                  Cancel
                </button>
                <button
                  onClick={handleCreateBaseline}
                  className="btn btn-primary"
                  disabled={!newBaselineForm.runId || !newBaselineForm.name.trim() || actionLoading === 'create'}
                >
                  {actionLoading === 'create' ? (
                    <LoadingSpinner size="sm" className="mr-2" />
                  ) : (
                    <CheckIcon className="h-4 w-4 mr-2" />
                  )}
                  Create Baseline
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Controls */}
      <div className="card">
        <div className="card-content">
          <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center justify-between">
            {/* Search */}
            <div className="relative flex-1 max-w-md">
              <MagnifyingGlassIcon className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-400" />
              <input
                type="text"
                placeholder="Search baselines..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="input pl-10"
              />
            </div>

            {/* Sort Controls */}
            <div className="flex items-center space-x-2">
              <span className="text-sm text-gray-500">Sort by:</span>
              {(['name', 'timestamp', 'testName'] as const).map((option) => (
                <button
                  key={option}
                  onClick={() => handleSort(option)}
                  className={`text-sm px-3 py-1 rounded ${
                    sortBy === option
                      ? 'bg-primary-100 text-primary-700'
                      : 'text-gray-500 hover:text-gray-700'
                  }`}
                >
                  {option === 'timestamp' ? 'Date' : option.charAt(0).toUpperCase() + option.slice(1)}
                  {sortBy === option && (sortOrder === 'asc' ? ' ↑' : ' ↓')}
                </button>
              ))}
            </div>
          </div>

          {/* Bulk Actions */}
          {selectedBaselines.size > 0 && (
            <div className="mt-4 p-3 bg-primary-50 border border-primary-200 rounded-lg">
              <div className="flex items-center justify-between">
                <span className="text-sm text-primary-700">
                  {selectedBaselines.size} baseline{selectedBaselines.size > 1 ? 's' : ''} selected
                </span>
                <div className="flex items-center space-x-2">
                  {selectedBaselines.size === 2 && (
                    <button
                      onClick={handleCompareBaselines}
                      className="btn btn-outline btn-sm"
                    >
                      <ArrowsRightLeftIcon className="h-4 w-4 mr-1" />
                      Compare
                    </button>
                  )}
                  <button
                    onClick={handleDeleteSelected}
                    className="btn btn-danger btn-sm"
                    disabled={actionLoading === 'delete-multiple'}
                  >
                    {actionLoading === 'delete-multiple' ? (
                      <LoadingSpinner size="sm" className="mr-1" />
                    ) : (
                      <TrashIcon className="h-4 w-4 mr-1" />
                    )}
                    Delete
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Baselines List */}
      <div className="card">
        {filteredBaselines.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="table">
              <thead className="table-header">
                <tr>
                  <th className="table-header-cell w-12">
                    <input
                      type="checkbox"
                      checked={selectedBaselines.size === filteredBaselines.length && filteredBaselines.length > 0}
                      onChange={handleSelectAll}
                      className="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    />
                  </th>
                  <th className="table-header-cell">Name</th>
                  <th className="table-header-cell">Test</th>
                  <th className="table-header-cell">Branch</th>
                  <th className="table-header-cell">Created</th>
                  <th className="table-header-cell">Performance</th>
                  <th className="table-header-cell">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {filteredBaselines.map((baseline) => (
                  <tr key={baseline.id} className="table-row">
                    <td className="table-cell">
                      <input
                        type="checkbox"
                        checked={selectedBaselines.has(baseline.id)}
                        onChange={() => handleSelectBaseline(baseline.id)}
                        className="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      />
                    </td>
                    <td className="table-cell">
                      {editingBaseline === baseline.id ? (
                        <div className="flex items-center space-x-2">
                          <input
                            type="text"
                            value={editForm.newName}
                            onChange={(e) => setEditForm(prev => ({ ...prev, newName: e.target.value }))}
                            className="input text-sm py-1"
                            autoFocus
                          />
                          <button
                            onClick={handleUpdateBaseline}
                            className="p-1 text-success-600 hover:text-success-700"
                            disabled={!editForm.newName.trim() || actionLoading === `edit-${baseline.id}`}
                            title="Submit edit"
                            aria-label="Submit edit"
                          >
                            {actionLoading === `edit-${baseline.id}` ? (
                              <LoadingSpinner size="sm" />
                            ) : (
                              <CheckIcon className="h-4 w-4" />
                            )}
                          </button>
                          <button
                            onClick={cancelEditing}
                            className="p-1 text-gray-400 hover:text-gray-600"
                            title="Cancel edit"
                            aria-label="Cancel edit"
                          >
                            <XMarkIcon className="h-4 w-4" />
                          </button>
                        </div>
                      ) : (
                        <div className="flex items-center space-x-2">
                          <FlagIcon className="h-4 w-4 text-warning-500" />
                          <span className="font-medium">{getBaselineField(baseline, 'baselineName', 'name')}</span>
                        </div>
                      )}
                    </td>
                    <td className="table-cell">
                      <span className="text-gray-900">{getBaselineField(baseline, 'testName', 'test_name')}</span>
                    </td>
                    <td className="table-cell">
                      <span className="badge badge-info">{getBaselineField(baseline, 'gitBranch', 'git_branch', 'N/A')}</span>
                    </td>
                    <td className="table-cell">
                      <div className="flex items-center space-x-1 text-gray-500">
                        <CalendarIcon className="h-4 w-4" />
                        <span className="text-sm">{formatTimestamp(getBaselineField(baseline, 'timestamp', 'created_at'))}</span>
                      </div>
                    </td>
                    <td className="table-cell">
                      <div className="space-y-1 text-xs">
                        <div className="flex justify-between">
                          <span className="text-gray-500">Success:</span>
                          <span className="font-mono">
                            {(() => {
                              const errorRate = getBaselineMetric(baseline, 'errorRate')
                              return typeof errorRate === 'number' && !isNaN(errorRate)
                                ? formatPercentage(100 - errorRate)
                                : 'N/A'
                            })()}
                          </span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-500">Latency:</span>
                          <span className="font-mono">{formatLatency(getBaselineMetric(baseline, 'avgLatency', 0))}</span>
                        </div>
                      </div>
                    </td>
                    <td className="table-cell">
                      <div className="flex items-center space-x-1">
                        {editingBaseline !== baseline.id && (
                          <button
                            onClick={() => startEditing(baseline)}
                            className="p-1 text-gray-400 hover:text-gray-600"
                            title="Edit baseline"
                          >
                            <PencilIcon className="h-4 w-4" />
                          </button>
                        )}
                        <button
                          onClick={() => handleDeleteBaseline(baseline)}
                          className="p-1 text-gray-400 hover:text-danger-600"
                          title="Delete baseline"
                          disabled={actionLoading === `delete-${baseline.id}`}
                        >
                          {actionLoading === `delete-${baseline.id}` ? (
                            <LoadingSpinner size="sm" />
                          ) : (
                            <TrashIcon className="h-4 w-4" />
                          )}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="card-content text-center py-12">
            {searchTerm ? (
              <div>
                <MagnifyingGlassIcon className="h-12 w-12 text-gray-400 mx-auto mb-3" />
                <h3 className="text-lg font-medium text-gray-900 mb-1">No matching baselines</h3>
                <p className="text-gray-500">Try adjusting your search terms.</p>
              </div>
            ) : (
              <div>
                <FlagIcon className="h-12 w-12 text-gray-400 mx-auto mb-3" />
                <h3 className="text-lg font-medium text-gray-900 mb-1">No baselines created</h3>
                <p className="text-gray-500 mb-4">
                  Create your first baseline to track performance over time.
                </p>
                <button
                  onClick={() => setShowCreateForm(true)}
                  className="btn btn-primary"
                  disabled={availableForBaseline.length === 0}
                >
                  <PlusIcon className="h-4 w-4 mr-2" />
                  Create Baseline
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Help Text */}
      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
        <h4 className="text-sm font-semibold text-blue-800 mb-2">About Baselines</h4>
        <div className="text-sm text-blue-700 space-y-1">
          <p>• Baselines are reference points for performance comparison</p>
          <p>• Use them to detect regressions and track improvements</p>
          <p>• Select two baselines to compare their performance metrics</p>
          <p>• Consider creating baselines for major releases or stable builds</p>
        </div>
      </div>
    </div>
  )
}

