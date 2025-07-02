import { useRef, useCallback, useMemo } from 'react'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  TimeScale,
  ChartOptions,
  ChartData,
  TooltipItem,
  Filler,
} from 'chart.js'
import { Line } from 'react-chartjs-2'
import 'chartjs-adapter-date-fns'
import zoomPlugin from 'chartjs-plugin-zoom'
import annotationPlugin from 'chartjs-plugin-annotation'
import { 
  ArrowDownTrayIcon, 
  MagnifyingGlassMinusIcon, 
  MagnifyingGlassPlusIcon,
  ExclamationTriangleIcon 
} from '@heroicons/react/24/outline'
import { ErrorSummary } from '../types/api'
import LoadingSpinner from './LoadingSpinner'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  TimeScale,
  Filler,
  zoomPlugin,
  annotationPlugin
)

export interface ErrorDataPoint {
  timestamp: string
  errorRate: number
  totalErrors: number
  totalRequests: number
  runId?: string
  errors?: ErrorSummary[]
  metadata?: Record<string, any>
}

export interface ErrorRateChartProps {
  data: ErrorDataPoint[]
  title?: string
  height?: number
  loading?: boolean
  error?: string
  className?: string
  showFill?: boolean
  showThreshold?: boolean
  errorThreshold?: number
  onPointClick?: (point: ErrorDataPoint) => void
  groupBy?: 'client' | 'method' | 'errorType'
  showErrorTypes?: boolean
  maxErrorRate?: number
}

const ERROR_COLORS = [
  '#ef4444', // red-500
  '#f59e0b', // amber-500
  '#8b5cf6', // violet-500
  '#06b6d4', // cyan-500
  '#10b981', // emerald-500
  '#f97316', // orange-500
  '#ec4899', // pink-500
  '#6366f1', // indigo-500
]

export function ErrorRateChart({
  data,
  title = 'Error Rate Over Time',
  height = 400,
  loading = false,
  error,
  className = '',
  showFill = true,
  showThreshold = true,
  errorThreshold = 0.05, // 5% default threshold
  onPointClick,
  groupBy,
  showErrorTypes = false,
  maxErrorRate = 1.0,
}: ErrorRateChartProps) {
  const chartRef = useRef<ChartJS<'line'>>(null)

  // Group data if needed
  const groupedData = useMemo(() => {
    if (!groupBy) {
      return { 'Error Rate': data }
    }

    const groups: Record<string, ErrorDataPoint[]> = {}
    
    data.forEach(point => {
      let groupKey = 'Unknown'
      
      if (groupBy === 'client' && point.metadata?.client) {
        groupKey = point.metadata.client
      } else if (groupBy === 'method' && point.metadata?.method) {
        groupKey = point.metadata.method
      } else if (groupBy === 'errorType' && point.errors && point.errors.length > 0) {
        // Group by most common error type
        const topError = point.errors.reduce((max, curr) => 
          curr.count > max.count ? curr : max
        )
        groupKey = topError.error
      }

      if (!groups[groupKey]) {
        groups[groupKey] = []
      }
      groups[groupKey].push(point)
    })

    return groups
  }, [data, groupBy])

  // Note: errorBreakdown removed as it's not currently used

  // Create chart data
  const chartData: ChartData<'line'> = useMemo(() => {
    const datasets = Object.entries(groupedData).map(([label, points], index) => {
      const color = ERROR_COLORS[index % ERROR_COLORS.length]
      
      return {
        label,
        data: points.map(point => ({
          x: new Date(point.timestamp).getTime(),
          y: point.errorRate * 100, // Convert to percentage
          metadata: point,
        })),
        borderColor: color,
        backgroundColor: showFill ? `${color}20` : color,
        borderWidth: 2,
        pointRadius: 4,
        pointHoverRadius: 6,
        tension: 0.1,
        fill: showFill,
      }
    })

    return { datasets }
  }, [groupedData, showFill])

  // Chart options
  const options: ChartOptions<'line'> = useMemo(() => {
    const annotations: any = {}

    // Add error threshold line
    if (showThreshold) {
      annotations.threshold = {
        type: 'line',
        yMin: errorThreshold * 100,
        yMax: errorThreshold * 100,
        borderColor: '#dc2626',
        borderWidth: 2,
        borderDash: [5, 5],
        label: {
          content: `Threshold: ${(errorThreshold * 100).toFixed(1)}%`,
          enabled: true,
          position: 'end',
          backgroundColor: 'rgba(220, 38, 38, 0.8)',
          color: 'white',
          font: { size: 11 },
        },
      }
    }

    return {
      responsive: true,
      maintainAspectRatio: false,
      interaction: {
        mode: 'index' as const,
        intersect: false,
      },
      scales: {
        x: {
          type: 'time',
          time: {
            displayFormats: {
              hour: 'MMM d, HH:mm',
              day: 'MMM d',
              week: 'MMM d',
              month: 'MMM yyyy',
            },
          },
          title: {
            display: true,
            text: 'Time',
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
        },
        y: {
          title: {
            display: true,
            text: 'Error Rate (%)',
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
          beginAtZero: true,
          max: maxErrorRate * 100,
          ticks: {
            callback: function(value) {
              return `${value}%`
            },
          },
        },
      },
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
          display: true,
          position: 'top' as const,
        },
        tooltip: {
          callbacks: {
            title: (tooltipItems: TooltipItem<'line'>[]) => {
              const date = new Date(tooltipItems[0].parsed.x)
              return date.toLocaleString()
            },
            label: (context: TooltipItem<'line'>) => {
              const point = (context.raw as any)?.metadata as ErrorDataPoint
              if (!point) return ''

              const lines = [
                `${context.dataset.label}: ${context.parsed.y.toFixed(2)}%`,
                `Errors: ${point.totalErrors.toLocaleString()}`,
                `Total Requests: ${point.totalRequests.toLocaleString()}`,
              ]

              if (point.runId) {
                lines.push(`Run: ${point.runId.substring(0, 8)}`)
              }

              return lines
            },
            afterBody: (tooltipItems: TooltipItem<'line'>[]) => {
              if (!showErrorTypes) return []

              const point = (tooltipItems[0].raw as any)?.metadata as ErrorDataPoint
              if (!point?.errors || point.errors.length === 0) return []

              const lines = ['', 'Error Breakdown:']
              point.errors
                .sort((a, b) => b.count - a.count)
                .slice(0, 5) // Show top 5 errors
                .forEach(errorSummary => {
                  lines.push(`  ${errorSummary.error}: ${errorSummary.count} (${errorSummary.percentage.toFixed(1)}%)`)
                })

              if (point.errors.length > 5) {
                lines.push(`  ... and ${point.errors.length - 5} more`)
              }

              return lines
            },
          },
        },
        zoom: {
          pan: {
            enabled: true,
            mode: 'xy' as const,
          },
          zoom: {
            wheel: {
              enabled: true,
            },
            pinch: {
              enabled: true,
            },
            mode: 'xy' as const,
          },
        },
        annotation: {
          annotations,
        },
      },
      onClick: (_event, elements) => {
        if (elements.length > 0 && onPointClick) {
          const element = elements[0]
          const point = (chartData.datasets[element.datasetIndex].data[element.index] as any)?.metadata as ErrorDataPoint
          if (point) {
            onPointClick(point)
          }
        }
      },
    }
  }, [title, showThreshold, errorThreshold, maxErrorRate, showErrorTypes, onPointClick, chartData])

  // Export chart as PNG
  const exportChart = useCallback(() => {
    if (chartRef.current) {
      const url = chartRef.current.toBase64Image('image/png', 1.0)
      const link = document.createElement('a')
      link.download = `error_rate_${new Date().toISOString().split('T')[0]}.png`
      link.href = url
      link.click()
    }
  }, [])

  // Reset zoom
  const resetZoom = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.resetZoom()
    }
  }, [])

  // Zoom controls
  const zoomIn = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.zoom(1.1)
    }
  }, [])

  const zoomOut = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.zoom(0.9)
    }
  }, [])

  // Calculate summary stats
  const stats = useMemo(() => {
    if (data.length === 0) return null

    const totalErrors = data.reduce((sum, point) => sum + point.totalErrors, 0)
    const totalRequests = data.reduce((sum, point) => sum + point.totalRequests, 0)
    const avgErrorRate = totalRequests > 0 ? totalErrors / totalRequests : 0
    const maxErrorRate = Math.max(...data.map(point => point.errorRate))
    const thresholdViolations = data.filter(point => point.errorRate > errorThreshold).length

    return {
      avgErrorRate,
      maxErrorRate,
      totalErrors,
      totalRequests,
      thresholdViolations,
    }
  }, [data, errorThreshold])

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
            <div className="text-danger-600 text-lg font-medium mb-2">Error loading chart</div>
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
            <div className="text-gray-500 text-lg font-medium mb-2">No error data available</div>
            <div className="text-gray-400">No error rate data for the selected time range</div>
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
          <button
            onClick={zoomIn}
            className="btn-outline p-2"
            title="Zoom In"
            aria-label="Zoom In"
          >
            <MagnifyingGlassPlusIcon className="h-4 w-4" />
          </button>
          <button
            onClick={zoomOut}
            className="btn-outline p-2"
            title="Zoom Out"
            aria-label="Zoom Out"
          >
            <MagnifyingGlassMinusIcon className="h-4 w-4" />
          </button>
          <button
            onClick={resetZoom}
            className="btn-secondary text-xs px-2 py-1"
            title="Reset Zoom"
          >
            Reset
          </button>
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
          <Line
            ref={chartRef}
            data={chartData}
            options={options}
          />
        </div>
      </div>

      {/* Summary statistics */}
      {stats && (
        <div className="card-footer">
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mb-4">
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {(stats.avgErrorRate * 100).toFixed(2)}%
              </div>
              <div className="text-sm text-gray-500">Avg Error Rate</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {(stats.maxErrorRate * 100).toFixed(2)}%
              </div>
              <div className="text-sm text-gray-500">Max Error Rate</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.totalErrors.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Total Errors</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.totalRequests.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Total Requests</div>
            </div>
            <div className="text-center">
              <div className={`text-2xl font-semibold ${stats.thresholdViolations > 0 ? 'text-danger-600' : 'text-success-600'}`}>
                {stats.thresholdViolations}
              </div>
              <div className="text-sm text-gray-500">
                <div className="flex items-center justify-center space-x-1">
                  {stats.thresholdViolations > 0 && <ExclamationTriangleIcon className="h-4 w-4 text-danger-600" />}
                  <span>Threshold Violations</span>
                </div>
              </div>
            </div>
          </div>
          
          <div className="text-xs text-gray-400 text-center">
            Drag to pan • Scroll to zoom • Click points for details
          </div>
        </div>
      )}
    </div>
  )
}

export default ErrorRateChart