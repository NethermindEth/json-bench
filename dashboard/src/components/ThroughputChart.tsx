import { useRef, useCallback, useMemo } from 'react'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  TimeScale,
  ChartOptions,
  ChartData,
  TooltipItem,
} from 'chart.js'
import { Bar } from 'react-chartjs-2'
import 'chartjs-adapter-date-fns'
import zoomPlugin from 'chartjs-plugin-zoom'
import annotationPlugin from 'chartjs-plugin-annotation'
import { 
  ArrowDownTrayIcon, 
  MagnifyingGlassMinusIcon, 
  MagnifyingGlassPlusIcon,
  ArrowTrendingUpIcon,
  ArrowTrendingDownIcon
} from '@heroicons/react/24/outline'
import LoadingSpinner from './LoadingSpinner'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  TimeScale,
  zoomPlugin,
  annotationPlugin
)

export interface ThroughputDataPoint {
  timestamp: string
  throughput: number
  totalRequests: number
  duration: number
  runId?: string
  metadata?: Record<string, any>
}

export interface ThroughputChartProps {
  data: ThroughputDataPoint[]
  title?: string
  height?: number
  loading?: boolean
  error?: string
  className?: string
  groupBy?: 'client' | 'method' | 'time'
  showTarget?: boolean
  targetThroughput?: number
  onBarClick?: (point: ThroughputDataPoint) => void
  timeGranularity?: 'hour' | 'day' | 'week'
  showTrend?: boolean
  unit?: string
}

const THROUGHPUT_COLORS = [
  '#3b82f6', // blue-500
  '#10b981', // emerald-500
  '#f59e0b', // amber-500
  '#ef4444', // red-500
  '#8b5cf6', // violet-500
  '#06b6d4', // cyan-500
  '#f97316', // orange-500
  '#ec4899', // pink-500
]

export function ThroughputChart({
  data,
  title = 'Request Throughput',
  height = 400,
  loading = false,
  error,
  className = '',
  groupBy = 'time',
  showTarget = false,
  targetThroughput,
  onBarClick,
  timeGranularity = 'hour',
  showTrend = true,
  unit = 'req/s',
}: ThroughputChartProps) {
  const chartRef = useRef<ChartJS<'bar'>>(null)

  // Group data if needed
  const groupedData = useMemo(() => {
    if (groupBy === 'time') {
      return { 'Throughput': data }
    }

    const groups: Record<string, ThroughputDataPoint[]> = {}
    
    data.forEach(point => {
      let groupKey = 'Unknown'
      
      if (groupBy === 'client' && point.metadata?.client) {
        groupKey = point.metadata.client
      } else if (groupBy === 'method' && point.metadata?.method) {
        groupKey = point.metadata.method
      }

      if (!groups[groupKey]) {
        groups[groupKey] = []
      }
      groups[groupKey].push(point)
    })

    return groups
  }, [data, groupBy])

  // Calculate trend
  const trendData = useMemo(() => {
    if (!showTrend || data.length < 2) return null

    const sortedData = [...data].sort((a, b) => 
      new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
    )

    // Simple linear regression
    const n = sortedData.length
    const timestamps = sortedData.map((_, i) => i)
    const throughputs = sortedData.map(d => d.throughput)

    const sumX = timestamps.reduce((sum, x) => sum + x, 0)
    const sumY = throughputs.reduce((sum, y) => sum + y, 0)
    const sumXY = timestamps.reduce((sum, x, i) => sum + x * throughputs[i], 0)
    const sumXX = timestamps.reduce((sum, x) => sum + x * x, 0)

    const slope = (n * sumXY - sumX * sumY) / (n * sumXX - sumX * sumX)
    const intercept = (sumY - slope * sumX) / n

    return {
      slope,
      intercept,
      direction: slope > 0 ? 'up' : slope < 0 ? 'down' : 'stable',
      change: Math.abs(slope),
    }
  }, [data, showTrend])

  // Create chart data
  const chartData: ChartData<'bar'> = useMemo(() => {
    const datasets = Object.entries(groupedData).map(([label, points], index) => {
      const color = THROUGHPUT_COLORS[index % THROUGHPUT_COLORS.length]
      
      return {
        label,
        data: points.map(point => point.throughput),
        backgroundColor: `${color}CC`, // 80% opacity
        borderColor: color,
        borderWidth: 1,
        hoverBackgroundColor: color,
        hoverBorderColor: color,
        borderRadius: 2,
        borderSkipped: false,
      }
    })

    const labels = groupedData[Object.keys(groupedData)[0]]?.map(point => point.timestamp) || []

    return { labels, datasets }
  }, [groupedData])

  // Chart options
  const options: ChartOptions<'bar'> = useMemo(() => {
    const annotations: any = {}

    // Add target throughput line
    if (showTarget && targetThroughput) {
      annotations.target = {
        type: 'line',
        yMin: targetThroughput,
        yMax: targetThroughput,
        borderColor: '#22c55e',
        borderWidth: 2,
        borderDash: [5, 5],
        label: {
          content: `Target: ${targetThroughput.toLocaleString()} ${unit}`,
          enabled: true,
          position: 'end',
          backgroundColor: 'rgba(34, 197, 94, 0.8)',
          color: 'white',
          font: { size: 11 },
        },
      }
    }

    // Add trend line if available
    if (trendData && data.length > 1) {
      const sortedData = [...data].sort((a, b) => 
        new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
      )
      
      const firstTimestamp = sortedData[0].timestamp
      const lastTimestamp = sortedData[sortedData.length - 1].timestamp
      const firstY = trendData.intercept
      const lastY = trendData.intercept + trendData.slope * (sortedData.length - 1)

      annotations.trend = {
        type: 'line',
        xMin: firstTimestamp,
        xMax: lastTimestamp,
        yMin: firstY,
        yMax: lastY,
        borderColor: trendData.direction === 'up' ? '#22c55e' : 
                    trendData.direction === 'down' ? '#ef4444' : '#6b7280',
        borderWidth: 2,
        label: {
          content: `Trend: ${trendData.direction}`,
          enabled: true,
          position: 'center',
          backgroundColor: 'rgba(0, 0, 0, 0.8)',
          color: 'white',
          font: { size: 10 },
        },
      }
    }

    const timeDisplayFormats = {
      hour: timeGranularity === 'hour' ? 'MMM d, HH:mm' : 'MMM d',
      day: 'MMM d',
      week: 'MMM d',
      month: 'MMM yyyy',
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
            displayFormats: timeDisplayFormats,
            unit: timeGranularity,
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
            text: `Throughput (${unit})`,
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
          beginAtZero: true,
          ticks: {
            callback: function(value) {
              return `${(value as number).toLocaleString()}`
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
          display: Object.keys(groupedData).length > 1,
          position: 'top' as const,
        },
        tooltip: {
          callbacks: {
            title: (tooltipItems: TooltipItem<'bar'>[]) => {
              const date = new Date(tooltipItems[0].parsed.x)
              return date.toLocaleString()
            },
            label: (context: TooltipItem<'bar'>) => {
              const datasetIndex = context.datasetIndex
              const index = context.dataIndex
              const points = Object.values(groupedData)[datasetIndex] || []
              const point = points[index]
              
              if (!point) return ''

              const lines = [
                `${context.dataset.label}: ${context.parsed.y.toLocaleString()} ${unit}`,
                `Total Requests: ${point.totalRequests.toLocaleString()}`,
                `Duration: ${point.duration.toFixed(2)}s`,
              ]

              if (point.runId) {
                lines.push(`Run: ${point.runId.substring(0, 8)}`)
              }

              return lines
            },
            afterBody: (tooltipItems: TooltipItem<'bar'>[]) => {
              const datasetIndex = tooltipItems[0].datasetIndex
              const index = tooltipItems[0].dataIndex
              const points = Object.values(groupedData)[datasetIndex] || []
              const point = points[index]
              
              if (!point?.metadata) return []

              const lines: string[] = []
              if (point.metadata.client) {
                lines.push(`Client: ${point.metadata.client}`)
              }
              if (point.metadata.method) {
                lines.push(`Method: ${point.metadata.method}`)
              }
              if (point.metadata.testName) {
                lines.push(`Test: ${point.metadata.testName}`)
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
        if (elements.length > 0 && onBarClick) {
          const element = elements[0]
          const datasetIndex = element.datasetIndex
          const index = element.index
          const points = Object.values(groupedData)[datasetIndex] || []
          const point = points[index]
          
          if (point) {
            onBarClick(point)
          }
        }
      },
    }
  }, [title, groupedData, showTarget, targetThroughput, timeGranularity, trendData, unit, onBarClick])

  // Export chart as PNG
  const exportChart = useCallback(() => {
    if (chartRef.current) {
      const url = chartRef.current.toBase64Image('image/png', 1.0)
      const link = document.createElement('a')
      link.download = `throughput_${new Date().toISOString().split('T')[0]}.png`
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

    const throughputs = data.map(point => point.throughput)
    const totalRequests = data.reduce((sum, point) => sum + point.totalRequests, 0)
    const avgThroughput = throughputs.reduce((sum, t) => sum + t, 0) / throughputs.length
    const maxThroughput = Math.max(...throughputs)
    const minThroughput = Math.min(...throughputs)

    let targetAchievements = 0
    if (targetThroughput) {
      targetAchievements = data.filter(point => point.throughput >= targetThroughput).length
    }

    return {
      avgThroughput,
      maxThroughput,
      minThroughput,
      totalRequests,
      targetAchievements,
      targetAchievementRate: targetThroughput ? (targetAchievements / data.length) * 100 : 0,
    }
  }, [data, targetThroughput])

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
            <div className="text-gray-500 text-lg font-medium mb-2">No throughput data</div>
            <div className="text-gray-400">No throughput data available for the selected time range</div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`card ${className}`}>
      {/* Header */}
      <div className="card-header flex justify-between items-center">
        <div className="flex items-center space-x-3">
          <h3 className="text-lg font-medium text-gray-900">{title}</h3>
          {trendData && (
            <div className={`flex items-center space-x-1 text-sm ${
              trendData.direction === 'up' ? 'text-success-600' : 
              trendData.direction === 'down' ? 'text-danger-600' : 'text-gray-500'
            }`}>
              {trendData.direction === 'up' ? (
                <ArrowTrendingUpIcon className="h-4 w-4" />
              ) : trendData.direction === 'down' ? (
                <ArrowTrendingDownIcon className="h-4 w-4" />
              ) : null}
              <span>Trending {trendData.direction}</span>
            </div>
          )}
        </div>
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
          <Bar
            ref={chartRef}
            data={chartData}
            options={options}
          />
        </div>
      </div>

      {/* Summary statistics */}
      {stats && (
        <div className="card-footer">
          <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4 mb-4">
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.avgThroughput.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Avg {unit}</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.maxThroughput.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Max {unit}</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.minThroughput.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Min {unit}</div>
            </div>
            <div className="text-center">
              <div className="text-2xl font-semibold text-gray-900">
                {stats.totalRequests.toLocaleString()}
              </div>
              <div className="text-sm text-gray-500">Total Requests</div>
            </div>
            {targetThroughput && (
              <>
                <div className="text-center">
                  <div className="text-2xl font-semibold text-gray-900">
                    {stats.targetAchievements}
                  </div>
                  <div className="text-sm text-gray-500">Target Hits</div>
                </div>
                <div className="text-center">
                  <div className={`text-2xl font-semibold ${
                    stats.targetAchievementRate >= 90 ? 'text-success-600' : 
                    stats.targetAchievementRate >= 70 ? 'text-warning-600' : 'text-danger-600'
                  }`}>
                    {stats.targetAchievementRate.toFixed(1)}%
                  </div>
                  <div className="text-sm text-gray-500">Target Rate</div>
                </div>
              </>
            )}
          </div>
          
          <div className="text-xs text-gray-400 text-center">
            Drag to pan • Scroll to zoom • Click bars for details
          </div>
        </div>
      )}
    </div>
  )
}

export default ThroughputChart