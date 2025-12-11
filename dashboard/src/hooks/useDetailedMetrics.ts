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
        
        // Try to get method metrics - now includes per-client data
        let clientMethodMetrics: Record<string, Record<string, any>> = {}
        try {
          const methodData: MethodMetricsData = await api.getRunMethods(runId)
          
          if (methodData && methodData.methods_by_client && typeof methodData.methods_by_client === 'object') {
            // New response format with per-client method metrics
            clientMethodMetrics = methodData.methods_by_client
            
            // Also create aggregate method metrics for backward compatibility
            // Use plain object instead of Map to avoid iterator issues with certain
            // build configurations or browser environments where Map.forEach may not
            // work as expected with React state updates
            const aggregateMethodMetrics: Record<string, any> = {}
            
            try {
              const clientNames = Object.keys(methodData.methods_by_client)
              for (let i = 0; i < clientNames.length; i++) {
                const client = clientNames[i]
                const methods = methodData.methods_by_client[client]
                if (!methods || typeof methods !== 'object') continue
                
                const methodNames = Object.keys(methods)
                for (let j = 0; j < methodNames.length; j++) {
                  const methodName = methodNames[j]
                  const metrics = methods[methodName]
                  if (!metrics || typeof metrics !== 'object') continue
                  
                  if (!aggregateMethodMetrics[methodName]) {
                    aggregateMethodMetrics[methodName] = {
                      name: methodName,
                      total_requests: 0,
                      success_rate: 0,
                      avg_latency: 0,
                      p50_latency: 0,
                      p95_latency: 0,
                      p99_latency: 0,
                      min_latency: Infinity,
                      max_latency: 0,
                      error_rate: 0,
                      throughput: 0,
                      count: 0
                    }
                  }
                  
                  const agg = aggregateMethodMetrics[methodName]
                  agg.total_requests += metrics.total_requests || 0
                  agg.success_rate += metrics.success_rate || 0
                  agg.avg_latency += metrics.avg_latency || 0
                  agg.p50_latency += metrics.p50_latency || 0
                  agg.p95_latency += metrics.p95_latency || 0
                  agg.p99_latency += metrics.p99_latency || 0
                  agg.min_latency = Math.min(agg.min_latency, metrics.min_latency || Infinity)
                  agg.max_latency = Math.max(agg.max_latency, metrics.max_latency || 0)
                  agg.error_rate += metrics.error_rate || 0
                  agg.throughput += metrics.throughput || 0
                  agg.count++
                }
              }
              
              // Average the aggregated values using plain for loop
              const methodArray: any[] = []
              const aggKeys = Object.keys(aggregateMethodMetrics)
              for (let k = 0; k < aggKeys.length; k++) {
                const agg = aggregateMethodMetrics[aggKeys[k]]
                methodArray.push({
                  name: agg.name,
                  total_requests: agg.total_requests,
                  success_rate: agg.count > 0 ? agg.success_rate / agg.count : 0,
                  avg_latency: agg.count > 0 ? agg.avg_latency / agg.count : 0,
                  p50_latency: agg.count > 0 ? agg.p50_latency / agg.count : 0,
                  p95_latency: agg.count > 0 ? agg.p95_latency / agg.count : 0,
                  p99_latency: agg.count > 0 ? agg.p99_latency / agg.count : 0,
                  min_latency: agg.min_latency === Infinity ? 0 : agg.min_latency,
                  max_latency: agg.max_latency,
                  error_rate: agg.count > 0 ? agg.error_rate / agg.count : 0,
                  throughput: agg.throughput,
                  std_dev: 0
                })
              }
              
              (run as any).method_metrics = methodArray
            } catch (aggError) {
              // Log the error for debugging but also propagate it so the user knows aggregation failed
              console.error('Failed to aggregate method metrics:', aggError)
              // Set an empty array so the UI can handle the missing data gracefully
              (run as any).method_metrics = []
              // Re-throw to ensure the error is not silently ignored
              throw new Error(`Method metrics aggregation failed: ${aggError instanceof Error ? aggError.message : String(aggError)}`)
            }
          } else if (methodData && methodData.methods) {
            // Old response format - single client or aggregate
            (run as any).method_metrics = Object.entries(methodData.methods).map(([name, metrics]) => ({
              name,
              total_requests: metrics.total_requests ?? 1000,
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
        
        // Add per-client method metrics if available
        if (Object.keys(clientMethodMetrics).length > 0) {
          detailedMetrics.clientMethodMetrics = clientMethodMetrics
        }
        
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