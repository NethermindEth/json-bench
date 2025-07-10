import { useMemo, useRef, useCallback } from 'react'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  ChartOptions,
  ChartData,
  TooltipItem,
} from 'chart.js'
import { Bar } from 'react-chartjs-2'
import { ArrowDownTrayIcon, InformationCircleIcon } from '@heroicons/react/24/outline'
import LoadingSpinner from './LoadingSpinner'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend
)

export interface LatencyBucket {
  min: number
  max: number
  count: number
  percentage: number
}

export interface LatencyDistributionProps {
  data: LatencyBucket[]
  title?: string
  height?: number
  loading?: boolean
  error?: string
  className?: string
  showPercentiles?: boolean
  percentiles?: {
    p50: number
    p90: number
    p95: number
    p99: number
  }
  onBucketClick?: (bucket: LatencyBucket) => void
  showStatistics?: boolean
  bucketSize?: number
  unit?: string
}

interface HistogramStats {
  mean: number
  median: number
  mode: number
  stdDev: number
  min: number
  max: number
  totalCount: number
}

const calculateStatistics = (buckets: LatencyBucket[]): HistogramStats => {
  let totalCount = 0
  let sum = 0
  const values: number[] = []

  // Calculate values for each bucket
  buckets.forEach(bucket => {
    const midpoint = (bucket.min + bucket.max) / 2
    totalCount += bucket.count
    sum += midpoint * bucket.count
    
    // Add midpoint values for standard deviation calculation
    for (let i = 0; i < bucket.count; i++) {
      values.push(midpoint)
    }
  })

  const mean = sum / totalCount
  
  // Calculate standard deviation
  const squaredDiffs = values.map(value => Math.pow(value - mean, 2))
  const avgSquaredDiff = squaredDiffs.reduce((sum, value) => sum + value, 0) / values.length
  const stdDev = Math.sqrt(avgSquaredDiff)

  // Find mode (bucket with highest count)
  const maxBucket = buckets.reduce((max, bucket) => 
    bucket.count > max.count ? bucket : max, buckets[0])
  const mode = (maxBucket.min + maxBucket.max) / 2

  // Estimate median
  let cumulativeCount = 0
  const medianPosition = totalCount / 2
  let median = mean
  
  for (const bucket of buckets) {
    cumulativeCount += bucket.count
    if (cumulativeCount >= medianPosition) {
      median = (bucket.min + bucket.max) / 2
      break
    }
  }

  return {
    mean,
    median,
    mode,
    stdDev,
    min: buckets[0]?.min || 0,
    max: buckets[buckets.length - 1]?.max || 0,
    totalCount,
  }
}

const formatLatency = (value: number, unit = 'ms'): string => {
  if (unit === 'ms') {
    if (value < 1000) return `${value.toFixed(1)}ms`
    return `${(value / 1000).toFixed(2)}s`
  }
  return `${value.toFixed(1)}${unit}`
}

export function LatencyDistribution({
  data,
  title = 'Latency Distribution',
  height = 400,
  loading = false,
  error,
  className = '',
  showPercentiles = true,
  percentiles,
  onBucketClick,
  showStatistics = true,
  bucketSize,
  unit = 'ms',
}: LatencyDistributionProps) {
  const chartRef = useRef<ChartJS<'bar'>>(null)

  const stats = useMemo(() => calculateStatistics(data), [data])

  // Prepare chart data
  const chartData: ChartData<'bar'> = useMemo(() => {
    const labels = data.map(bucket => {
      if (bucket.min === bucket.max) {
        return formatLatency(bucket.min, unit)
      }
      return `${formatLatency(bucket.min, unit)} - ${formatLatency(bucket.max, unit)}`
    })

    return {
      labels,
      datasets: [
        {
          label: 'Request Count',
          data: data.map(bucket => bucket.count),
          backgroundColor: 'rgba(59, 130, 246, 0.6)',
          borderColor: 'rgba(59, 130, 246, 1)',
          borderWidth: 1,
          hoverBackgroundColor: 'rgba(59, 130, 246, 0.8)',
          hoverBorderColor: 'rgba(59, 130, 246, 1)',
        },
      ],
    }
  }, [data, unit])

  // Chart options
  const options: ChartOptions<'bar'> = useMemo(() => {
    const annotations: any = {}

    // Add percentile lines if available
    if (showPercentiles && percentiles) {
      Object.entries(percentiles).forEach(([key, value]) => {
        annotations[`percentile-${key}`] = {
          type: 'line',
          scaleID: 'x',
          value: formatLatency(value, unit),
          borderColor: key === 'p99' ? '#ef4444' : 
                      key === 'p95' ? '#f59e0b' : 
                      key === 'p90' ? '#f59e0b' : '#22c55e',
          borderWidth: 2,
          borderDash: [5, 5],
          label: {
            content: `${key.toUpperCase()}: ${formatLatency(value, unit)}`,
            enabled: true,
            position: 'start',
            backgroundColor: 'rgba(0, 0, 0, 0.8)',
            color: 'white',
            font: { size: 11 },
          },
        }
      })
    }

    return {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        title: {
          display: true,
          text: title,
          font: {
            size: 16,
            weight: 'bold',
          },
        },
        legend: {
          display: false,
        },
        tooltip: {
          callbacks: {
            title: (tooltipItems: TooltipItem<'bar'>[]) => {
              const index = tooltipItems[0].dataIndex
              const bucket = data[index]
              return `${formatLatency(bucket.min, unit)} - ${formatLatency(bucket.max, unit)}`
            },
            label: (context: TooltipItem<'bar'>) => {
              const index = context.dataIndex
              const bucket = data[index]
              return [
                `Count: ${bucket.count.toLocaleString()}`,
                `Percentage: ${bucket.percentage.toFixed(2)}%`,
              ]
            },
            afterBody: () => {
              return ['Click to view detailed breakdown']
            },
          },
        },
        annotation: {
          annotations,
        },
      },
      scales: {
        x: {
          title: {
            display: true,
            text: `Latency (${unit})`,
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
        },
        y: {
          title: {
            display: true,
            text: 'Request Count',
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
          beginAtZero: true,
        },
      },
      onClick: (_event, elements) => {
        if (elements.length > 0 && onBucketClick) {
          const element = elements[0]
          const index = element.index
          const bucket = data[index]
          onBucketClick(bucket)
        }
      },
    }
  }, [title, data, showPercentiles, percentiles, onBucketClick, unit])

  // Export chart as PNG
  const exportChart = useCallback(() => {
    if (chartRef.current) {
      const url = chartRef.current.toBase64Image('image/png', 1.0)
      const link = document.createElement('a')
      link.download = `latency_distribution_${new Date().toISOString().split('T')[0]}.png`
      link.href = url
      link.click()
    }
  }, [])

  if (loading) {
    return (
      <div className={`card ${className}`} style={{ height }}>
        <div className="card-content flex items-center justify-center h-full">
          <LoadingSpinner size="lg" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className={`card ${className}`} style={{ height }}>
        <div className="card-content flex items-center justify-center h-full">
          <div className="text-center">
            <div className="text-danger-600 text-lg font-medium mb-2">Error loading distribution</div>
            <div className="text-gray-500">{error}</div>
          </div>
        </div>
      </div>
    )
  }

  if (data.length === 0) {
    return (
      <div className={`card ${className}`} style={{ height }}>
        <div className="card-content flex items-center justify-center h-full">
          <div className="text-center">
            <div className="text-gray-500 text-lg font-medium mb-2">No distribution data</div>
            <div className="text-gray-400">No latency data available for the selected time range</div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`card ${className}`}>
      {/* Header */}
      <div className="card-header flex justify-between items-center">
        <h3 className="text-lg font-medium text-gray-900">{title}</h3>
        <div className="flex items-center space-x-2">
          {bucketSize && (
            <div className="flex items-center text-sm text-gray-500">
              <InformationCircleIcon className="h-4 w-4 mr-1" />
              Bucket size: {formatLatency(bucketSize, unit)}
            </div>
          )}
          <button
            onClick={exportChart}
            className="btn-outline p-2"
            title="Export as PNG"
            aria-label="Export chart as PNG"
          >
            <ArrowDownTrayIcon className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Chart */}
      <div className="card-content">
        <div style={{ height: height - 120 }}>
          <Bar
            ref={chartRef}
            data={chartData}
            options={options}
          />
        </div>
      </div>

      {/* Statistics and percentiles */}
      {(showStatistics || (showPercentiles && percentiles)) && (
        <div className="card-footer">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {showStatistics && (
              <>
                <div className="text-center">
                  <div className="text-2xl font-semibold text-gray-900">
                    {formatLatency(stats.mean, unit)}
                  </div>
                  <div className="text-sm text-gray-500">Mean</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-semibold text-gray-900">
                    {formatLatency(stats.median, unit)}
                  </div>
                  <div className="text-sm text-gray-500">Median</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-semibold text-gray-900">
                    {formatLatency(stats.stdDev, unit)}
                  </div>
                  <div className="text-sm text-gray-500">Std Dev</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-semibold text-gray-900">
                    {stats.totalCount.toLocaleString()}
                  </div>
                  <div className="text-sm text-gray-500">Total Requests</div>
                </div>
              </>
            )}
          </div>

          {showPercentiles && percentiles && (
            <div className="mt-4 pt-4 border-t border-gray-200">
              <div className="grid grid-cols-4 gap-4">
                {Object.entries(percentiles).map(([key, value]) => (
                  <div key={key} className="text-center">
                    <div className="text-lg font-medium text-gray-900">
                      {formatLatency(value, unit)}
                    </div>
                    <div className="text-xs text-gray-500">{key.toUpperCase()}</div>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="mt-4 text-xs text-gray-400 text-center">
            Click bars for detailed breakdown • Hover for percentages
          </div>
        </div>
      )}
    </div>
  )
}

export default LatencyDistribution