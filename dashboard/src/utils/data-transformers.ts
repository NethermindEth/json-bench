import { HistoricRun, RunDetailsResponse, ClientMetrics as APIClientMetrics } from '../types/api'
import { DetailedMetrics, ClientMetrics, MethodMetrics } from '../types/detailed-metrics'

/**
 * Parse duration strings like "1m30.5s" to seconds
 */
export function parseDuration(duration: string): number {
  const match = duration.match(/(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?/)
  if (match) {
    const hours = parseInt(match[1] || '0')
    const minutes = parseInt(match[2] || '0')
    const seconds = parseFloat(match[3] || '0')
    return hours * 3600 + minutes * 60 + seconds
  }
  return 60 // Default 1 minute
}

/**
 * Transform HistoricRun data to DetailedMetrics format
 * Extracts detailed metrics from API client_metrics or full_results JSON
 */
export function transformHistoricRunToDetailedMetrics(run: HistoricRun, apiClientMetrics?: Record<string, APIClientMetrics>): DetailedMetrics {
  // Parse full_results if available
  let fullResults: any = {}
  if (run.full_results) {
    try {
      fullResults = JSON.parse(run.full_results)
    } catch (e) {
      console.error('Failed to parse full results:', e)
    }
  }
  
  // Transform client metrics - prioritize API client metrics over full_results
  const clientMetrics: ClientMetrics[] = []
  
  // First try to use client metrics from API response
  if (apiClientMetrics && Object.keys(apiClientMetrics).length > 0) {
    Object.entries(apiClientMetrics).forEach(([clientName, metrics]) => {
      clientMetrics.push({
        clientName,
        totalRequests: metrics.total_requests || 0,
        successRate: metrics.success_rate || 0,
        errorRate: metrics.error_rate || 0,
        avgLatency: metrics.latency?.avg || 0,
        throughput: metrics.latency?.throughput || 0,
        latencyPercentiles: {
          p50: metrics.latency?.p50 ?? 0,
          p75: 0, // Not available in current API
          p90: metrics.latency?.p90 ?? 0,
          p95: metrics.latency?.p95 ?? 0,
          p99: metrics.latency?.p99 ?? 0,
          p999: 0, // Not available in current API
          min: metrics.latency?.min ?? 0,
          max: metrics.latency?.max ?? 0,
          mean: metrics.latency?.avg ?? 0,
          stdDev: metrics.latency?.std_dev ?? 0,
          median: metrics.latency?.p50 ?? 0, // Use p50 as median estimate
          iqr: 0, // Not available from current data
          mad: 0, // Not available from current data
          variance: Math.pow(metrics.latency?.std_dev ?? 0, 2),
          skewness: 0, // Not available from current data
          kurtosis: 0, // Not available from current data
          coefficientOfVariation: (metrics.latency?.avg && metrics.latency?.std_dev) ? (metrics.latency.std_dev / metrics.latency.avg) : 0
        },
        connectionMetrics: {
          activeConnections: 0,
          connectionReuse: 0,
          dnsResolutionTime: 0,
          tcpHandshakeTime: 0,
          tlsHandshakeTime: 0,
          connectionTimeouts: 0,
          keepAliveEnabled: false,
          poolSize: 0,
          poolUtilization: 0,
          requestsPerConnection: 0,
          connectionErrors: 0,
          maxConcurrentConnections: 0
        }
      })
    })
  }
  // Fallback to parsing from full_results if no API client metrics
  else if (fullResults.client_metrics) {
    Object.entries(fullResults.client_metrics).forEach(([clientName, metrics]: [string, any]) => {
      clientMetrics.push({
        clientName,
        totalRequests: metrics.total_requests || 0,
        successRate: 100 - (metrics.error_rate || 0),
        errorRate: metrics.error_rate || 0,
        avgLatency: metrics.latency?.avg || 0,
        throughput: metrics.throughput || 0,
        latencyPercentiles: {
          p50: metrics.latency?.p50 ?? 0,
          p75: metrics.latency?.p75 ?? 0,
          p90: metrics.latency?.p90 ?? 0,
          p95: metrics.latency?.p95 ?? 0,
          p99: metrics.latency?.p99 ?? 0,
          p999: metrics.latency?.p999 ?? 0,
          min: metrics.latency?.min ?? 0,
          max: metrics.latency?.max ?? 0,
          mean: metrics.latency?.avg ?? 0,
          stdDev: metrics.latency?.std_dev ?? 0,
          median: metrics.latency?.p50 ?? 0, // Use p50 as median estimate
          iqr: 0, // Not available from current data
          mad: 0, // Not available from current data
          variance: Math.pow(metrics.latency?.std_dev ?? 0, 2),
          skewness: 0, // Not available from current data
          kurtosis: 0, // Not available from current data
          coefficientOfVariation: (metrics.latency?.avg && metrics.latency?.std_dev) ? (metrics.latency.std_dev / metrics.latency.avg) : 0
        },
        connectionMetrics: {
          activeConnections: metrics.connection_metrics?.active_connections || 0,
          connectionReuse: metrics.connection_metrics?.connection_reuse || 0,
          dnsResolutionTime: metrics.connection_metrics?.dns_resolution_time || 0,
          tcpHandshakeTime: metrics.connection_metrics?.tcp_handshake_time || 0,
          tlsHandshakeTime: metrics.connection_metrics?.tls_handshake_time || 0,
          connectionTimeouts: metrics.connection_metrics?.connection_timeouts || 0,
          keepAliveEnabled: metrics.connection_metrics?.keep_alive_enabled || false,
          poolSize: metrics.connection_metrics?.pool_size || 0,
          poolUtilization: metrics.connection_metrics?.pool_utilization || 0,
          requestsPerConnection: metrics.connection_metrics?.requests_per_connection || 0,
          connectionErrors: metrics.connection_metrics?.connection_errors || 0,
          maxConcurrentConnections: metrics.connection_metrics?.max_concurrent_connections || 0
        }
      })
    })
  }
  
  // If no detailed client metrics, log warning instead of creating synthetic data
  if (clientMetrics.length === 0 && run.clients.length > 0) {
    console.warn('No per-client metrics available for run', run.id)
  }
  
  // Transform method metrics
  const methodMetrics: MethodMetrics[] = []
  const methodMap = new Map<string, any>()
  
  // First check if we have method_metrics from the report endpoint
  if ((run as any).method_metrics && Array.isArray((run as any).method_metrics)) {
    (run as any).method_metrics.forEach((method: any) => {
      methodMetrics.push({
        methodName: method.name,
        requestCount: method.total_requests || 0,
        latencyPercentiles: {
          p50: method.p50_latency ?? 0,
          p75: (method.p50_latency ?? 0) * 1.2, // Estimate p75
          p90: (method.p50_latency ?? 0) * 1.5, // Estimate p90
          p95: method.p95_latency ?? 0,
          p99: method.p99_latency ?? 0,
          p999: method.max_latency ?? 0,
          min: method.min_latency ?? 0,
          max: method.max_latency ?? 0,
          mean: method.avg_latency ?? 0,
          stdDev: method.std_dev ?? 0,
          median: method.p50_latency ?? 0,
          iqr: ((method.p95_latency ?? 0) - (method.p50_latency ?? 0)) * 0.5,
          mad: (method.std_dev ?? 0) * 0.5,
          variance: Math.pow(method.std_dev ?? 0, 2),
          skewness: 0,
          kurtosis: 0,
          coefficientOfVariation: (method.avg_latency && method.std_dev) ? (method.std_dev / method.avg_latency) : 0
        },
        errorsByClient: {},
        errorRate: method.error_rate ?? 0,
        complexity: 0,
        reliability: {
          uptime: 100,
          availability: 100,
          mttr: 0,
          mtbf: 0,
          errorBudget: 0,
          slaCompliance: 100,
          serviceLevel: {
            objective: 99.9,
            actual: method.success_rate || 100,
            errorBudget: 0,
            burnRate: 0,
            violations: 0,
            period: '24h'
          },
          incidents: []
        },
        trendIndicator: {
          direction: 'stable' as const,
          confidence: 0,
          changeRate: 0,
          forecast: [],
          anomalies: []
        },
        parameters: []
      })
    })
  } else if (fullResults.client_metrics) {
    Object.values(fullResults.client_metrics).forEach((clientMetrics: any) => {
      if (clientMetrics.methods) {
        Object.entries(clientMetrics.methods).forEach(([methodName, metrics]: [string, any]) => {
          if (!methodMap.has(methodName)) {
            methodMap.set(methodName, {
              methodName,
              requestCount: 0,
              totalLatency: 0,
              errorCount: 0,
              latencies: [],
              clients: []
            })
          }
          
          const method = methodMap.get(methodName)
          method.requestCount += metrics.count || 0
          method.errorCount += metrics.error_count || 0
          method.latencies.push(metrics)
          method.clients.push(clientMetrics.name)
        })
      }
    })
    
    methodMap.forEach((data, methodName) => {
      const successRate = data.requestCount > 0 
        ? ((data.requestCount - data.errorCount) / data.requestCount) * 100 
        : 0
        
      // Average the latencies from all clients
      const avgLatencies = data.latencies.length > 0 ? {
        p50: data.latencies.reduce((sum: number, l: any) => sum + (l.p50 ?? 0), 0) / data.latencies.length,
        p75: data.latencies.reduce((sum: number, l: any) => sum + (l.p75 ?? 0), 0) / data.latencies.length,
        p90: data.latencies.reduce((sum: number, l: any) => sum + (l.p90 ?? 0), 0) / data.latencies.length,
        p95: data.latencies.reduce((sum: number, l: any) => sum + (l.p95 ?? 0), 0) / data.latencies.length,
        p99: data.latencies.reduce((sum: number, l: any) => sum + (l.p99 ?? 0), 0) / data.latencies.length,
        p999: data.latencies.reduce((sum: number, l: any) => sum + (l.p999 ?? 0), 0) / data.latencies.length,
        min: data.latencies.length > 0 ? Math.min(...data.latencies.map((l: any) => l.min ?? 0)) : 0,
        max: data.latencies.length > 0 ? Math.max(...data.latencies.map((l: any) => l.max ?? 0)) : 0,
        mean: data.latencies.reduce((sum: number, l: any) => sum + (l.avg ?? 0), 0) / data.latencies.length,
        stdDev: data.latencies.reduce((sum: number, l: any) => sum + (l.std_dev ?? 0), 0) / data.latencies.length,
        median: data.latencies.reduce((sum: number, l: any) => sum + (l.p50 ?? 0), 0) / data.latencies.length,
        iqr: 0, // Not available from aggregated data
        mad: 0, // Not available from aggregated data
        variance: Math.pow(data.latencies.reduce((sum: number, l: any) => sum + (l.std_dev ?? 0), 0) / data.latencies.length, 2),
        skewness: 0,
        kurtosis: 0,
        coefficientOfVariation: 0
      } : {
        p50: 0,
        p75: 0,
        p90: 0,
        p95: 0,
        p99: 0,
        p999: 0,
        min: 0,
        max: 0,
        mean: 0,
        stdDev: 0,
        median: 0,
        iqr: 0,
        mad: 0,
        variance: 0,
        skewness: 0,
        kurtosis: 0,
        coefficientOfVariation: 0
      }
        
      methodMetrics.push({
        methodName,
        requestCount: data.requestCount,
        latencyPercentiles: avgLatencies,
        errorsByClient: {},
        complexity: 0,
        reliability: {
          uptime: 100,
          availability: 100,
          mttr: 0,
          mtbf: 0,
          errorBudget: 0,
          slaCompliance: 100,
          serviceLevel: {
            objective: 99.9,
            actual: 100,
            errorBudget: 0,
            burnRate: 0,
            violations: 0,
            period: '24h'
          },
          incidents: []
        },
        trendIndicator: {
          direction: 'stable' as const,
          confidence: 0,
          changeRate: 0,
          forecast: [],
          anomalies: []
        },
        parameters: []
      })
    })
  }
  
  // If no detailed method metrics, create from aggregate data
  if (methodMetrics.length === 0 && run.methods.length > 0) {
    run.methods.forEach(methodName => {
      methodMetrics.push({
        methodName,
        requestCount: run.methods.length > 0 
          ? Math.floor(run.total_requests / run.methods.length)
          : 0,
        latencyPercentiles: {
          p50: run.avg_latency_ms * 0.7,
          p75: run.avg_latency_ms * 0.9,
          p90: run.avg_latency_ms * 1.2,
          p95: run.p95_latency_ms,
          p99: run.p99_latency_ms,
          p999: run.max_latency_ms,
          min: run.avg_latency_ms * 0.1,
          max: run.max_latency_ms,
          mean: run.avg_latency_ms,
          stdDev: run.avg_latency_ms * 0.3,
          median: run.avg_latency_ms * 0.7, // Use p50 as median estimate
          iqr: run.avg_latency_ms * 0.5, // Estimate IQR
          mad: run.avg_latency_ms * 0.2, // Estimate MAD
          variance: Math.pow(run.avg_latency_ms * 0.3, 2),
          skewness: 0,
          kurtosis: 0,
          coefficientOfVariation: 0.3 // Estimated
        },
        errorsByClient: {},
        complexity: 0,
        reliability: {
          uptime: 100,
          availability: 100,
          mttr: 0,
          mtbf: 0,
          errorBudget: 0,
          slaCompliance: 100,
          serviceLevel: {
            objective: 99.9,
            actual: 100,
            errorBudget: 0,
            burnRate: 0,
            violations: 0,
            period: '24h'
          },
          incidents: []
        },
        trendIndicator: {
          direction: 'stable' as const,
          confidence: 0,
          changeRate: 0,
          forecast: [],
          anomalies: []
        },
        parameters: []
      })
    })
  }
  
  return {
    clientMetrics,
    methodMetrics,
    errorAnalysis: {
      totalErrors: run.total_errors || 0,
      errorsByType: {},
      errorsByClient: {},
      errorsByMethod: {},
      errorTrends: []
    },
    systemMetrics: {
      cpuUsage: 0,
      memoryUsage: 0,
      networkIO: { bytesIn: 0, bytesOut: 0, packetsIn: 0, packetsOut: 0, latency: 0, jitter: 0, packetLoss: 0 },
      diskIO: { readOps: 0, writeOps: 0, readBytes: 0, writeBytes: 0, iops: 0, queueDepth: 0, utilization: 0, avgServiceTime: 0 },
      environment: { 
        os: fullResults.environment?.os || 'Unknown',
        cpuModel: fullResults.environment?.cpu || 'Unknown',
        cpuCores: fullResults.environment?.cpu_cores || 0,
        totalMemory: fullResults.environment?.memory || 0,
        nodeVersion: fullResults.environment?.node_version || 'Unknown',
        kernelVersion: '',
        hostname: '',
        uptime: 0,
        loadAverage: [0, 0, 0]
      }
    },
    latencyDistribution: { 
      buckets: [],
      percentiles: {
        p50: run.avg_latency_ms * 0.7,
        p75: run.avg_latency_ms * 0.9,
        p90: run.avg_latency_ms * 1.2,
        p95: run.p95_latency_ms,
        p99: run.p99_latency_ms,
        p999: run.max_latency_ms
      },
      histogram: [],
      outliers: [],
      skewness: 0,
      kurtosis: 0,
      mean: run.avg_latency_ms,
      median: run.avg_latency_ms * 0.7,
      mode: run.avg_latency_ms * 0.7,
      variance: 0,
      confidenceInterval: { lower: 0, upper: 0, level: 0.95 }
    },
    capacityMetrics: {
      maxThroughput: 0,
      saturationPoint: 0,
      scalabilityFactor: 0,
      efficiencyRatio: 0,
      utilizationPercentage: 0,
      headroom: 0,
      bottlenecks: []
    },
    reliabilityMetrics: {
      uptime: 100,
      mtbf: 0,
      mttr: 0,
      availability: 100,
      durability: 100,
      faultTolerance: 0,
      recoveryTime: 0,
      dataIntegrity: 100,
      consistencyScore: 100
    },
    comparisonMetrics: {
      baselineId: '',
      improvementPercentage: 0,
      regressionCount: 0,
      significantChanges: []
    },
    timeSeries: {
      timestamps: [],
      latencies: [],
      throughput: [],
      errorRates: [],
      activeConnections: []
    }
  }
}