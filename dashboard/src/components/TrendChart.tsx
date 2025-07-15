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
} from 'chart.js'
import { Line } from 'react-chartjs-2'
import 'chartjs-adapter-date-fns'
import zoomPlugin from 'chartjs-plugin-zoom'
import annotationPlugin from 'chartjs-plugin-annotation'
import { ArrowDownTrayIcon, MagnifyingGlassMinusIcon, MagnifyingGlassPlusIcon } from '@heroicons/react/24/outline'
import { TrendPoint, Regression } from '../types/api'
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
  zoomPlugin,
  annotationPlugin
)

export interface TrendChartProps {
  data: TrendPoint[]
  title: string
  metric: string
  showBaseline?: boolean
  height?: number
  loading?: boolean
  error?: string
  baselineData?: TrendPoint[]
  regressions?: Regression[]
  deployments?: Array<{
    timestamp: string
    version: string
    description?: string
  }>
  className?: string
  onPointClick?: (point: TrendPoint) => void
}

interface ChartDataset {
  label: string
  data: Array<{ x: number; y: number; metadata?: any }>
  borderColor: string
  backgroundColor: string
  borderWidth: number
  pointRadius: number
  pointHoverRadius: number
  tension: number
  fill: boolean
}

export function TrendChart({
  data,
  title,
  metric,
  showBaseline = false,
  height = 400,
  loading = false,
  error,
  baselineData = [],
  regressions = [],
  deployments = [],
  className = '',
  onPointClick,
}: TrendChartProps) {
  const chartRef = useRef<ChartJS<'line'>>(null)

  // Color scheme for different percentiles
  const colorScheme = {
    p50: { border: '#3b82f6', background: 'rgba(59, 130, 246, 0.1)' },
    p90: { border: '#f59e0b', background: 'rgba(245, 158, 11, 0.1)' },
    p95: { border: '#ef4444', background: 'rgba(239, 68, 68, 0.1)' },
    p99: { border: '#8b5cf6', background: 'rgba(139, 92, 246, 0.1)' },
    baseline: { border: '#6b7280', background: 'rgba(107, 114, 128, 0.1)' },
  }

  // Group data by percentile if metric contains percentile info
  const groupedData = useMemo(() => {
    const groups: Record<string, TrendPoint[]> = {}
    
    data.forEach(point => {
      // Extract percentile from metadata or use default
      const percentile = point.metadata?.percentile as string || 'p50'
      if (!groups[percentile]) {
        groups[percentile] = []
      }
      groups[percentile].push(point)
    })

    return groups
  }, [data])

  // Create datasets for the chart
  const chartData: ChartData<'line'> = useMemo(() => {
    const datasets: ChartDataset[] = []

    // Add main data series
    Object.entries(groupedData).forEach(([percentile, points]) => {
      const color = colorScheme[percentile as keyof typeof colorScheme] || colorScheme.p50
      
      datasets.push({
        label: percentile.toUpperCase(),
        data: points.map(point => ({
          x: new Date(point.timestamp).getTime(),
          y: point.value,
          metadata: point.metadata,
        })),
        borderColor: color.border,
        backgroundColor: color.background,
        borderWidth: 2,
        pointRadius: 4,
        pointHoverRadius: 6,
        tension: 0.1,
        fill: false,
      })
    })

    // Add baseline data if enabled
    if (showBaseline && baselineData.length > 0) {
      datasets.push({
        label: 'Baseline',
        data: baselineData.map(point => ({
          x: new Date(point.timestamp).getTime(),
          y: point.value,
          metadata: point.metadata,
        })),
        borderColor: colorScheme.baseline.border,
        backgroundColor: colorScheme.baseline.background,
        borderWidth: 2,
        pointRadius: 3,
        pointHoverRadius: 5,
        tension: 0,
        fill: false,
      })
    }

    return { datasets }
  }, [groupedData, showBaseline, baselineData])

  // Chart configuration
  const options: ChartOptions<'line'> = useMemo(() => {
    const annotations: any = {}

    // Add deployment annotations
    deployments.forEach((deployment, index) => {
      annotations[`deployment-${index}`] = {
        type: 'line',
        xMin: deployment.timestamp,
        xMax: deployment.timestamp,
        borderColor: '#22c55e',
        borderWidth: 2,
        borderDash: [5, 5],
        label: {
          content: `Deploy: ${deployment.version}`,
          enabled: true,
          position: 'top',
          backgroundColor: 'rgba(34, 197, 94, 0.8)',
          color: 'white',
          font: { size: 11 },
        },
      }
    })

    // Add regression markers
    regressions.forEach((regression, index) => {
      const point = data.find(p => p.runId === regression.runId)
      if (point) {
        annotations[`regression-${index}`] = {
          type: 'point',
          xValue: point.timestamp,
          yValue: point.value,
          backgroundColor: regression.severity === 'critical' ? '#ef4444' : 
                          regression.severity === 'major' ? '#f59e0b' : '#fbbf24',
          borderColor: 'white',
          borderWidth: 2,
          radius: 8,
          label: {
            content: `Regression: ${regression.percentChange.toFixed(1)}%`,
            enabled: true,
            position: 'top',
          },
        }
      }
    })

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
            text: metric,
          },
          grid: {
            color: 'rgba(0, 0, 0, 0.05)',
          },
          beginAtZero: false,
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
              const dataset = context.dataset
              const value = context.parsed.y
              const runId = (context.raw as any)?.metadata?.runId
              
              let label = `${dataset.label}: ${value.toFixed(2)}`
              if (runId) {
                label += ` (Run: ${runId.substring(0, 8)})`
              }
              return label
            },
            afterBody: (tooltipItems: TooltipItem<'line'>[]) => {
              const point = tooltipItems[0]
              const metadata = (point.raw as any)?.metadata
              
              if (metadata) {
                const details: string[] = []
                if (metadata.gitCommit) {
                  details.push(`Commit: ${metadata.gitCommit.substring(0, 8)}`)
                }
                if (metadata.gitBranch) {
                  details.push(`Branch: ${metadata.gitBranch}`)
                }
                if (metadata.testName) {
                  details.push(`Test: ${metadata.testName}`)
                }
                return details
              }
              return []
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
          const datasetIndex = element.datasetIndex
          const index = element.index
          const point = chartData.datasets[datasetIndex].data[index]
          
          // Find the original TrendPoint
          const originalPoint = data.find(p => 
            p.timestamp === (point as any).x && p.value === (point as any).y
          )
          
          if (originalPoint) {
            onPointClick(originalPoint)
          }
        }
      },
    }
  }, [title, metric, data, regressions, deployments, onPointClick, chartData])

  // Export chart as PNG
  const exportChart = useCallback(() => {
    if (chartRef.current) {
      const url = chartRef.current.toBase64Image('image/png', 1.0)
      const link = document.createElement('a')
      link.download = `${title.replace(/\s+/g, '_').toLowerCase()}_${new Date().toISOString().split('T')[0]}.png`
      link.href = url
      link.click()
    }
  }, [title])

  // Reset zoom
  const resetZoom = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.resetZoom()
    }
  }, [])

  // Zoom in
  const zoomIn = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.zoom(1.1)
    }
  }, [])

  // Zoom out
  const zoomOut = useCallback(() => {
    if (chartRef.current) {
      chartRef.current.zoom(0.9)
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
            <div className="text-gray-500 text-lg font-medium mb-2">No data available</div>
            <div className="text-gray-400">Try adjusting your filters or time range</div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`card ${className}`}>
      {/* Chart controls */}
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
        <div style={{ height: height - 80 }}>
          <Line
            ref={chartRef}
            data={chartData}
            options={options}
          />
        </div>
      </div>

      {/* Chart legend/info */}
      {(regressions.length > 0 || deployments.length > 0) && (
        <div className="card-footer">
          <div className="flex flex-wrap gap-4 text-xs text-gray-600">
            {regressions.length > 0 && (
              <div className="flex items-center space-x-1">
                <div className="w-3 h-3 rounded-full bg-danger-500"></div>
                <span>{regressions.length} regression{regressions.length !== 1 ? 's' : ''}</span>
              </div>
            )}
            {deployments.length > 0 && (
              <div className="flex items-center space-x-1">
                <div className="w-3 h-1 bg-success-500"></div>
                <span>{deployments.length} deployment{deployments.length !== 1 ? 's' : ''}</span>
              </div>
            )}
            <div className="text-gray-400">
              Drag to pan • Scroll to zoom • Click points for details
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default TrendChart