import { useQuery } from '@tanstack/react-query'
import { useAPI } from '../api/hooks'
import { DetailedMetrics } from '../types/detailed-metrics'
import { HistoricRun, MethodMetricsData } from '../types/api'
import { transformHistoricRunToDetailedMetrics } from '../utils/data-transformers'

/**
 * Hook to fetch and transform detailed metrics for a specific run
 * Uses the /api/runs/{id}/report endpoint to get full method metrics with latencies
 * 
 * @param runId - The ID of the run to fetch detailed metrics for
 * @param enabled - Whether the query should be enabled
 * @returns React Query result with DetailedMetrics data
 */
export function useDetailedMetrics(runId: string, enabled: boolean = true) {
  const api = useAPI()
  
  return useQuery<DetailedMetrics, Error>({
    queryKey: ['detailed-metrics', runId],
    queryFn: async () => {
      try {
        // Get the run data with client metrics
        const runResponse = await api.getRun(runId)
        const run = runResponse.run
        const clientMetrics = runResponse.client_metrics
        
        // Try to get method metrics
        try {
          const methodData: MethodMetricsData = await api.getRunMethods(runId)
          if (methodData && methodData.methods) {
            // Convert to array format expected by transformer
            (run as any).method_metrics = Object.entries(methodData.methods).map(([name, metrics]) => ({
              name,
              total_requests: metrics.total_requests ?? 1000, // Estimate since not stored
              success_rate: metrics.success_rate ?? 100,
              avg_latency: metrics.avg_latency ?? 0,
              p50_latency: metrics.p50_latency ?? 0,
              p95_latency: metrics.p95_latency ?? 0,
              p99_latency: metrics.p99_latency ?? 0,
              min_latency: metrics.min_latency ?? 0,
              max_latency: metrics.max_latency ?? 0,
              error_rate: metrics.error_rate ?? (100 - (metrics.success_rate ?? 100)),
              throughput: metrics.throughput ?? 0,
              std_dev: metrics.std_dev ?? 0
            }))
          }
        } catch (error) {
          console.warn('Method metrics not available:', error)
        }
        
        // Transform the data to DetailedMetrics format with client metrics from API
        const detailedMetrics = transformHistoricRunToDetailedMetrics(run, clientMetrics)
        
        return detailedMetrics
      } catch (error) {
        console.error('Failed to fetch detailed metrics:', error)
        throw new Error(`Failed to load metrics for run ${runId}`)
      }
    },
    enabled: enabled && !!runId,
    staleTime: 5 * 60 * 1000, // 5 minutes
    retry: 1, // Only retry once
    retryDelay: 1000
  })
}