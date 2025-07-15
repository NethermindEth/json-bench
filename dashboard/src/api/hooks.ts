/**
 * React hooks for the Benchmark API client
 * 
 * This file provides React hooks that integrate the BenchmarkAPI client
 * with React Query for state management and caching.
 */

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { createBenchmarkAPI, BenchmarkAPIError } from './client'
import type { 
  HistoricRun, 
  BenchmarkResult, 
  TrendData, 
  RunFilter, 
  TrendFilter,
  DashboardStats,
  RegressionReport
} from '../types/api'

// Create a shared API client instance
const api = createBenchmarkAPI(
  (import.meta as any).env?.VITE_API_BASE_URL || 'http://localhost:8082',
  {
    timeout: 15000,
    retries: 3
  }
)

// Query keys for React Query
export const queryKeys = {
  runs: (filter?: RunFilter) => ['runs', filter],
  run: (id: string) => ['run', id],
  runReport: (id: string) => ['runReport', id],
  trends: (filter: TrendFilter) => ['trends', filter],
  methodTrends: (method: string, days: number) => ['methodTrends', method, days],
  clientTrends: (client: string, days: number) => ['clientTrends', client, days],
  baselines: () => ['baselines'],
  regressions: (runId: string) => ['regressions', runId],
  dashboardStats: () => ['dashboardStats'],
  comparison: (runId1: string, runId2: string) => ['comparison', runId1, runId2]
} as const

/**
 * Hook to fetch historical runs with optional filtering
 */
export function useRuns(filter?: RunFilter) {
  return useQuery<HistoricRun[], BenchmarkAPIError>({
    queryKey: queryKeys.runs(filter),
    queryFn: async () => {
      const response = await api.listRuns(filter)
      // API returns { count, limit, runs }, but we need just the runs array
      return response.runs || []
    },
    staleTime: 30 * 1000, // 30 seconds
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch a specific run by ID
 */
export function useRun(id: string, enabled = true) {
  return useQuery<HistoricRun, BenchmarkAPIError>({
    queryKey: queryKeys.run(id),
    queryFn: async () => {
      const response = await api.getRun(id)
      return response.run
    },
    enabled: enabled && !!id,
    staleTime: 5 * 60 * 1000, // 5 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch a run's detailed report
 */
export function useRunReport(id: string, enabled = true) {
  return useQuery<BenchmarkResult, BenchmarkAPIError>({
    queryKey: queryKeys.runReport(id),
    queryFn: () => api.getRunReport(id),
    enabled: enabled && !!id,
    staleTime: 10 * 60 * 1000, // 10 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch trend data
 */
export function useTrends(filter: TrendFilter) {
  return useQuery<TrendData, BenchmarkAPIError>({
    queryKey: queryKeys.trends(filter),
    queryFn: () => api.getTrends(filter),
    staleTime: 2 * 60 * 1000, // 2 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch method trends
 */
export function useMethodTrends(method: string, days: number, enabled = true) {
  return useQuery({
    queryKey: queryKeys.methodTrends(method, days),
    queryFn: () => api.getMethodTrends(method, days),
    enabled: enabled && !!method,
    staleTime: 2 * 60 * 1000, // 2 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch client trends
 */
export function useClientTrends(client: string, days: number, enabled = true) {
  return useQuery({
    queryKey: queryKeys.clientTrends(client, days),
    queryFn: () => api.getClientTrends(client, days),
    enabled: enabled && !!client,
    staleTime: 2 * 60 * 1000, // 2 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch baseline runs
 */
export function useBaselines() {
  return useQuery<HistoricRun[], BenchmarkAPIError>({
    queryKey: queryKeys.baselines(),
    queryFn: () => api.listBaselines(),
    staleTime: 5 * 60 * 1000, // 5 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch regression report for a run
 */
export function useRegressions(runId: string, enabled = true) {
  return useQuery<RegressionReport, BenchmarkAPIError>({
    queryKey: queryKeys.regressions(runId),
    queryFn: () => api.getRegressions(runId),
    enabled: enabled && !!runId,
    staleTime: 5 * 60 * 1000, // 5 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Hook to fetch dashboard statistics
 */
export function useDashboardStats() {
  return useQuery<DashboardStats, BenchmarkAPIError>({
    queryKey: queryKeys.dashboardStats(),
    queryFn: () => api.getDashboardStats(),
    staleTime: 60 * 1000, // 1 minute
    refetchOnWindowFocus: true,
    refetchInterval: 60 * 1000 // Auto-refresh every minute
  })
}

/**
 * Hook to compare two runs
 */
export function useComparison(runId1: string, runId2: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.comparison(runId1, runId2),
    queryFn: () => api.compareRuns(runId1, runId2),
    enabled: enabled && !!runId1 && !!runId2,
    staleTime: 10 * 60 * 1000, // 10 minutes
    refetchOnWindowFocus: false
  })
}

/**
 * Mutation hook to set a baseline
 */
export function useSetBaseline() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ runId, name }: { runId: string; name: string }) =>
      api.setBaseline(runId, name),
    onSuccess: () => {
      // Invalidate and refetch baselines
      queryClient.invalidateQueries({ queryKey: queryKeys.baselines() })
      // Also invalidate runs queries to update baseline flags
      queryClient.invalidateQueries({ queryKey: ['runs'] })
    }
  })
}

/**
 * Mutation hook to remove a baseline
 */
export function useRemoveBaseline() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (runId: string) => api.removeBaseline(runId),
    onSuccess: () => {
      // Invalidate and refetch baselines
      queryClient.invalidateQueries({ queryKey: queryKeys.baselines() })
      // Also invalidate runs queries to update baseline flags
      queryClient.invalidateQueries({ queryKey: ['runs'] })
    }
  })
}

/**
 * Hook to get the API client instance
 * Useful for direct API calls not covered by other hooks
 */
export function useAPI() {
  return api
}

/**
 * Hook to check API health and connection status
 */
export function useHealthCheck() {
  return useQuery<{ status: string; timestamp: string; version?: string; services?: Record<string, string> }, BenchmarkAPIError>({
    queryKey: ['health'],
    queryFn: async () => {
      try {
        console.log('🔍 Health check: Checking API connectivity...')
        const result = await api.healthCheck()
        console.log('✅ Health check: API connection successful', {
          status: result.status,
          version: result.version,
          services: result.services,
          timestamp: result.timestamp
        })
        return result
      } catch (error) {
        console.error('❌ Health check: API connection failed', error)
        
        // Enhanced error reporting for better debugging
        if (error instanceof BenchmarkAPIError) {
          console.error('🔧 Health check: BenchmarkAPIError details', {
            message: error.message,
            status: error.status,
            details: error.details
          })
          throw error
        }
        
        // Handle network errors
        if (error instanceof Error) {
          if (error.message.includes('Network Error') || error.message.includes('ERR_NETWORK')) {
            const networkError = new BenchmarkAPIError('Network connection failed - check if API server is running', 0)
            console.error('🌐 Health check: Network error detected', error.message)
            throw networkError
          }
          if (error.message.includes('timeout')) {
            const timeoutError = new BenchmarkAPIError('Connection timeout - API server not responding', 0)
            console.error('⏱️ Health check: Timeout error detected', error.message)
            throw timeoutError
          }
          if (error.message.includes('ECONNREFUSED')) {
            const refusedError = new BenchmarkAPIError('Connection refused - API server may be down', 0)
            console.error('🚫 Health check: Connection refused', error.message)
            throw refusedError
          }
        }
        
        const unknownError = new BenchmarkAPIError('Health check failed - unknown error', 0)
        console.error('❓ Health check: Unknown error', error)
        throw unknownError
      }
    },
    staleTime: 5 * 1000, // 5 seconds
    refetchInterval: 15 * 1000, // Check every 15 seconds for faster updates
    refetchOnWindowFocus: true,
    refetchIntervalInBackground: true, // Keep checking even when tab is not focused
    retry: (failureCount, error) => {
      // Retry more aggressively for health checks
      if (failureCount < 3) {
        return true
      }
      return false
    },
    retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 30000), // Exponential backoff
  })
}

/**
 * Hook to manage API authentication
 */
export function useAPIAuth() {
  const setToken = (token: string) => {
    api.setAuthToken(token)
  }

  const removeToken = () => {
    api.removeAuthToken()
  }

  return { setToken, removeToken }
}

// Export the API instance for direct usage
export { api }