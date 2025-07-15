import { useState, FormEvent } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FlagIcon, TrashIcon, PlusIcon } from '@heroicons/react/24/outline'
import LoadingSpinner from '../components/LoadingSpinner'
import type { HistoricRun } from '../types/api'
import { createBenchmarkAPI } from '../api/client'

// Initialize API client
const api = createBenchmarkAPI(import.meta.env.VITE_API_BASE_URL || 'http://localhost:8082')

export default function Baselines() {
  const [showAddForm, setShowAddForm] = useState(false)
  const [selectedRunId, setSelectedRunId] = useState('')
  const [baselineName, setBaselineName] = useState('')
  const [description, setDescription] = useState('')

  const queryClient = useQueryClient()

  const { data: baselines, isLoading } = useQuery({
    queryKey: ['baselines'],
    queryFn: () => api.listBaselines(),
    retry: false,
  })

  const { data: availableRuns, isLoading: runsLoading } = useQuery({
    queryKey: ['available-runs'],
    queryFn: () => api.listRuns({ limit: 50 }),
    retry: false,
  })

  const addBaselineMutation = useMutation({
    mutationFn: async (_data: { runId: string; name: string; description: string }) => {
      // Simulate API call
      await new Promise(resolve => setTimeout(resolve, 1000))
      return { success: true }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['baselines'] })
      setShowAddForm(false)
      setSelectedRunId('')
      setBaselineName('')
      setDescription('')
    },
  })

  const deleteBaselineMutation = useMutation({
    mutationFn: async (_baselineId: string) => {
      // Simulate API call
      await new Promise(resolve => setTimeout(resolve, 500))
      return { success: true }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['baselines'] })
    },
  })

  const handleAddBaseline = (e: FormEvent) => {
    e.preventDefault()
    if (selectedRunId && baselineName) {
      addBaselineMutation.mutate({
        runId: selectedRunId,
        name: baselineName,
        description,
      })
    }
  }

  const handleDeleteBaseline = (baselineId: string) => {
    if (confirm('Are you sure you want to delete this baseline?')) {
      deleteBaselineMutation.mutate(baselineId)
    }
  }

  if (isLoading) {
    return (
      <div className="p-6">
        <LoadingSpinner size="lg" className="h-64" />
      </div>
    )
  }

  return (
    <div className="p-6">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold text-gray-900 mb-2">
              Performance Baselines
            </h1>
            <p className="text-gray-600">
              Manage benchmark baselines for performance comparisons
            </p>
          </div>
          <button
            onClick={() => setShowAddForm(!showAddForm)}
            className="btn-primary"
          >
            <PlusIcon className="h-4 w-4 mr-2" />
            Add Baseline
          </button>
        </div>
      </div>

      {/* Add Baseline Form */}
      {showAddForm && (
        <div className="card mb-8">
          <div className="card-header">
            <h2 className="text-lg font-semibold text-gray-900">Add New Baseline</h2>
          </div>
          <div className="card-content">
            <form onSubmit={handleAddBaseline} className="space-y-4">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="label">Select Run</label>
                  <select
                    value={selectedRunId}
                    onChange={(e) => setSelectedRunId(e.target.value)}
                    className="input"
                    required
                    disabled={runsLoading}
                  >
                    <option value="">Select a run...</option>
                    {availableRuns?.map((run) => (
                      <option key={run.id} value={run.id}>
                        {run.testName} - {new Date(run.timestamp).toLocaleDateString()} ({run.gitCommit})
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="label">Baseline Name</label>
                  <input
                    type="text"
                    value={baselineName}
                    onChange={(e) => setBaselineName(e.target.value)}
                    className="input"
                    placeholder="e.g., Production Baseline"
                    required
                  />
                </div>
              </div>
              <div>
                <label className="label">Description (Optional)</label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="input"
                  rows={3}
                  placeholder="Describe when and why this baseline was created"
                />
              </div>
              <div className="flex space-x-3">
                <button
                  type="submit"
                  disabled={addBaselineMutation.isPending || !selectedRunId || !baselineName}
                  className="btn-primary"
                >
                  {addBaselineMutation.isPending ? (
                    <>
                      <LoadingSpinner size="sm" className="mr-2" />
                      Creating...
                    </>
                  ) : (
                    'Create Baseline'
                  )}
                </button>
                <button
                  type="button"
                  onClick={() => setShowAddForm(false)}
                  className="btn-secondary"
                >
                  Cancel
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Baselines List */}
      <div className="card">
        <div className="card-header">
          <h2 className="text-lg font-semibold text-gray-900 flex items-center">
            <FlagIcon className="h-5 w-5 mr-2" />
            Current Baselines ({baselines?.length || 0})
          </h2>
        </div>
        <div className="card-content p-0">
          {baselines && baselines.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="table">
                <thead className="table-header">
                  <tr>
                    <th className="table-header-cell">Baseline Name</th>
                    <th className="table-header-cell">Test Run</th>
                    <th className="table-header-cell">Git Info</th>
                    <th className="table-header-cell">Performance</th>
                    <th className="table-header-cell">Created</th>
                    <th className="table-header-cell">Actions</th>
                  </tr>
                </thead>
                <tbody className="bg-white divide-y divide-gray-200">
                  {baselines.map((baseline) => (
                    <tr key={baseline.id} className="table-row">
                      <td className="table-cell">
                        <div>
                          <div className="font-medium text-gray-900">{baseline.baselineName}</div>
                          {baseline.description && (
                            <div className="text-sm text-gray-500">{baseline.description}</div>
                          )}
                        </div>
                      </td>
                      <td className="table-cell">
                        <div>
                          <div className="font-medium text-gray-900">{baseline.testName}</div>
                          <div className="text-sm text-gray-500 font-mono">{baseline.id}</div>
                        </div>
                      </td>
                      <td className="table-cell">
                        <div>
                          <div className="font-mono text-gray-900">{baseline.gitCommit}</div>
                          <div className="text-sm text-gray-500">{baseline.gitBranch}</div>
                        </div>
                      </td>
                      <td className="table-cell">
                        <div className="space-y-1">
                          <div className="text-sm">
                            <span className="text-gray-500">Latency:</span> {baseline.avgLatency.toFixed(1)}ms
                          </div>
                          <div className="text-sm">
                            <span className="text-gray-500">Success:</span> {baseline.successRate.toFixed(1)}%
                          </div>
                        </div>
                      </td>
                      <td className="table-cell">
                        {new Date(baseline.timestamp).toLocaleDateString()}
                      </td>
                      <td className="table-cell">
                        <button
                          onClick={() => handleDeleteBaseline(baseline.id)}
                          disabled={deleteBaselineMutation.isPending}
                          className="text-danger-600 hover:text-danger-700 transition-colors"
                          title="Delete baseline"
                        >
                          {deleteBaselineMutation.isPending ? (
                            <LoadingSpinner size="sm" />
                          ) : (
                            <TrashIcon className="h-4 w-4" />
                          )}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="text-center py-12">
              <FlagIcon className="h-12 w-12 text-gray-400 mx-auto mb-4" />
              <h3 className="text-lg font-medium text-gray-900 mb-2">
                No Baselines Created
              </h3>
              <p className="text-gray-500 mb-4">
                Create your first baseline to start tracking performance changes over time.
              </p>
              <button
                onClick={() => setShowAddForm(true)}
                className="btn-primary"
              >
                <PlusIcon className="h-4 w-4 mr-2" />
                Add First Baseline
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}