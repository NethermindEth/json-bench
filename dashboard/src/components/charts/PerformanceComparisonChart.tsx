import { useMemo, useState, useCallback, useRef } from 'react'
import {
  Chart as ChartJS,
  RadialLinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  CategoryScale,
  LinearScale,
  BarElement,
  ChartOptions,
  TooltipItem,
} from 'chart.js'
import { Radar, Bar } from 'react-chartjs-2'
import { 
  ChartBarIcon, 
  CircleStackIcon, 
  AdjustmentsHorizontalIcon,
  InformationCircleIcon 
} from '@heroicons/react/24/outline'
import { DetailedMetrics, ClientMetrics, MethodMetrics } from '../../types/detailed-metrics'
import { formatLatency, formatThroughput, formatPercentage, formatLatencyValue } from '../../utils/metric-formatters'
import LoadingSpinner from '../LoadingSpinner'

// Register Chart.js components
ChartJS.register(
  RadialLinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  CategoryScale,
  LinearScale,
  BarElement
)

export interface PerformanceComparisonChartProps {
  data: DetailedMetrics
  comparisonType: 'client' | 'method'
  metric: 'latency' | 'throughput' | 'success_rate' | 'all'
  showBaseline?: boolean
  interactive?: boolean
  height?: number
  chartType?: 'radar' | 'bar'
  onDataPointClick?: (item: ComparisonItem, metric: string) => void
  className?: string
}

export interface ComparisonItem {
  name: string
  metrics: {
    latency: number
    throughput: number
    successRate: number
    errorRate: number
    reliability: number
    performanceScore: number
  }
  baseline?: {
    latency: number
    throughput: number
    successRate: number
    errorRate: number
    reliability: number
    performanceScore: number
  }
  rawData: ClientMetrics | MethodMetrics
}

export interface BaselineData {
  latency: number
  throughput: number
  successRate: number
  errorRate: number
  reliability: number
  performanceScore: number
}

export function PerformanceComparisonChart({
  data,
  comparisonType,
  metric,
  showBaseline = true,
  interactive = true,
  height = 400,
  chartType = 'radar',
  onDataPointClick,
  className = '',
}: PerformanceComparisonChartProps) {
  const chartRef = useRef<ChartJS<'radar' | 'bar'>>(null)
  const [selectedMetric, setSelectedMetric] = useState<string>(metric)
  const [selectedItem, setSelectedItem] = useState<ComparisonItem | null>(null)
  const [currentChartType, setCurrentChartType] = useState<'radar' | 'bar'>(chartType)
  const [normalizeData, setNormalizeData] = useState(false)

  // Process comparison data
  const comparisonData = useMemo((): ComparisonItem[] => {
    const items: ComparisonItem[] = []

    if (comparisonType === 'client') {
      data.clientMetrics.forEach(client => {
        const item: ComparisonItem = {
          name: client.clientName,
          metrics: {
            latency: client.latencyPercentiles.p95,
            throughput: client.throughput,
            successRate: client.successRate,
            errorRate: client.errorRate,
            reliability: client.reliability.availability,
            performanceScore: client.performanceScore,
          },
          rawData: client,
        }

        // Add baseline data if available
        if (showBaseline && data.clientMetrics.length > 0) {
          const avgLatency = data.clientMetrics.reduce((sum, c) => sum + c.latencyPercentiles.p95, 0) / data.clientMetrics.length
          const avgThroughput = data.clientMetrics.reduce((sum, c) => sum + c.throughput, 0) / data.clientMetrics.length
          const avgSuccessRate = data.clientMetrics.reduce((sum, c) => sum + c.successRate, 0) / data.clientMetrics.length
          const avgErrorRate = data.clientMetrics.reduce((sum, c) => sum + c.errorRate, 0) / data.clientMetrics.length
          const avgReliability = data.clientMetrics.reduce((sum, c) => sum + c.reliability.availability, 0) / data.clientMetrics.length
          const avgPerformanceScore = data.clientMetrics.reduce((sum, c) => sum + c.performanceScore, 0) / data.clientMetrics.length

          item.baseline = {
            latency: avgLatency,
            throughput: avgThroughput,
            successRate: avgSuccessRate,
            errorRate: avgErrorRate,
            reliability: avgReliability,
            performanceScore: avgPerformanceScore,
          }
        }

        items.push(item)
      })
    } else {
      data.methodMetrics.forEach(method => {
        const item: ComparisonItem = {
          name: method.methodName,
          metrics: {
            latency: method.latencyPercentiles.p95,
            throughput: method.requestCount / (data.duration / 1000), // requests per second
            successRate: ((method.requestCount - Object.values(method.errorsByClient).reduce((sum, e) => sum + e.count, 0)) / method.requestCount) * 100,
            errorRate: (Object.values(method.errorsByClient).reduce((sum, e) => sum + e.count, 0) / method.requestCount) * 100,
            reliability: method.reliability.availability,
            performanceScore: method.performanceScore,
          },
          rawData: method,
        }

        // Add baseline data if available
        if (showBaseline && data.methodMetrics.length > 0) {
          const avgLatency = data.methodMetrics.reduce((sum, m) => sum + m.latencyPercentiles.p95, 0) / data.methodMetrics.length
          const avgThroughput = data.methodMetrics.reduce((sum, m) => sum + (m.requestCount / (data.duration / 1000)), 0) / data.methodMetrics.length
          const avgSuccessRate = data.methodMetrics.reduce((sum, m) => {
            const errorCount = Object.values(m.errorsByClient).reduce((sum, e) => sum + e.count, 0)
            return sum + ((m.requestCount - errorCount) / m.requestCount) * 100
          }, 0) / data.methodMetrics.length
          const avgErrorRate = data.methodMetrics.reduce((sum, m) => {
            const errorCount = Object.values(m.errorsByClient).reduce((sum, e) => sum + e.count, 0)
            return sum + (errorCount / m.requestCount) * 100
          }, 0) / data.methodMetrics.length
          const avgReliability = data.methodMetrics.reduce((sum, m) => sum + m.reliability.availability, 0) / data.methodMetrics.length
          const avgPerformanceScore = data.methodMetrics.reduce((sum, m) => sum + m.performanceScore, 0) / data.methodMetrics.length

          item.baseline = {
            latency: avgLatency,
            throughput: avgThroughput,
            successRate: avgSuccessRate,
            errorRate: avgErrorRate,
            reliability: avgReliability,
            performanceScore: avgPerformanceScore,
          }
        }

        items.push(item)
      })
    }

    return items
  }, [data, comparisonType, showBaseline])

  // Normalize data for better comparison
  const normalizedData = useMemo(() => {
    if (!normalizeData) return comparisonData

    const metrics = ['latency', 'throughput', 'successRate', 'errorRate', 'reliability', 'performanceScore']
    const maxValues: Record<string, number> = {}
    const minValues: Record<string, number> = {}

    // Find min/max values for each metric
    metrics.forEach(metric => {
      const values = comparisonData.map(item => item.metrics[metric as keyof ComparisonItem['metrics']])
      maxValues[metric] = Math.max(...values)
      minValues[metric] = Math.min(...values)
    })

    // Normalize data
    return comparisonData.map(item => ({
      ...item,
      metrics: {
        ...item.metrics,
        latency: minValues.latency === maxValues.latency ? 50 : 
                 ((maxValues.latency - item.metrics.latency) / (maxValues.latency - minValues.latency)) * 100, // Invert for latency (lower is better)
        throughput: minValues.throughput === maxValues.throughput ? 50 :
                   ((item.metrics.throughput - minValues.throughput) / (maxValues.throughput - minValues.throughput)) * 100,
        successRate: minValues.successRate === maxValues.successRate ? 50 :
                    ((item.metrics.successRate - minValues.successRate) / (maxValues.successRate - minValues.successRate)) * 100,
        errorRate: minValues.errorRate === maxValues.errorRate ? 50 :
                  ((maxValues.errorRate - item.metrics.errorRate) / (maxValues.errorRate - minValues.errorRate)) * 100, // Invert for error rate (lower is better)
        reliability: minValues.reliability === maxValues.reliability ? 50 :
                    ((item.metrics.reliability - minValues.reliability) / (maxValues.reliability - minValues.reliability)) * 100,
        performanceScore: minValues.performanceScore === maxValues.performanceScore ? 50 :
                         ((item.metrics.performanceScore - minValues.performanceScore) / (maxValues.performanceScore - minValues.performanceScore)) * 100,
      }
    }))
  }, [comparisonData, normalizeData])

  // Chart data generation
  const chartData = useMemo(() => {
    const dataToUse = normalizedData
    const colors = [
      'rgba(59, 130, 246, 0.6)',   // Blue
      'rgba(34, 197, 94, 0.6)',    // Green
      'rgba(239, 68, 68, 0.6)',    // Red
      'rgba(245, 158, 11, 0.6)',   // Amber
      'rgba(139, 92, 246, 0.6)',   // Purple
      'rgba(236, 72, 153, 0.6)',   // Pink
      'rgba(20, 184, 166, 0.6)',   // Teal
      'rgba(251, 146, 60, 0.6)',   // Orange
    ]

    const borderColors = colors.map(color => color.replace('0.6', '1'))

    if (currentChartType === 'radar') {
      const labels = selectedMetric === 'all' 
        ? ['Latency', 'Throughput', 'Success Rate', 'Error Rate', 'Reliability', 'Performance Score']
        : [selectedMetric.charAt(0).toUpperCase() + selectedMetric.slice(1)]

      const datasets = dataToUse.map((item, index) => ({
        label: item.name,
        data: selectedMetric === 'all' 
          ? [
              item.metrics.latency,
              item.metrics.throughput,
              item.metrics.successRate,
              item.metrics.errorRate,
              item.metrics.reliability,
              item.metrics.performanceScore,
            ]
          : [item.metrics[selectedMetric as keyof ComparisonItem['metrics']]],
        backgroundColor: colors[index % colors.length],
        borderColor: borderColors[index % borderColors.length],
        borderWidth: 2,
        pointRadius: 4,
        pointHoverRadius: 6,
      }))

      // Add baseline dataset if enabled
      if (showBaseline && dataToUse[0]?.baseline) {
        datasets.push({
          label: 'Baseline',
          data: selectedMetric === 'all' 
            ? [
                dataToUse[0].baseline.latency,
                dataToUse[0].baseline.throughput,
                dataToUse[0].baseline.successRate,
                dataToUse[0].baseline.errorRate,
                dataToUse[0].baseline.reliability,
                dataToUse[0].baseline.performanceScore,
              ]
            : [dataToUse[0].baseline[selectedMetric as keyof BaselineData]],
          backgroundColor: 'rgba(107, 114, 128, 0.2)',
          borderColor: 'rgba(107, 114, 128, 1)',
          borderWidth: 2,
          pointRadius: 3,
          pointHoverRadius: 5,
        } as any)
      }

      return { labels, datasets }
    } else {
      // Bar chart
      const labels = dataToUse.map(item => item.name)
      const datasets = selectedMetric === 'all' 
        ? [
            {
              label: 'Latency',
              data: dataToUse.map(item => item.metrics.latency),
              backgroundColor: colors[0],
              borderColor: borderColors[0],
              borderWidth: 1,
            },
            {
              label: 'Throughput',
              data: dataToUse.map(item => item.metrics.throughput),
              backgroundColor: colors[1],
              borderColor: borderColors[1],
              borderWidth: 1,
            },
            {
              label: 'Success Rate',
              data: dataToUse.map(item => item.metrics.successRate),
              backgroundColor: colors[2],
              borderColor: borderColors[2],
              borderWidth: 1,
            },
            {
              label: 'Error Rate',
              data: dataToUse.map(item => item.metrics.errorRate),
              backgroundColor: colors[3],
              borderColor: borderColors[3],
              borderWidth: 1,
            },
            {
              label: 'Reliability',
              data: dataToUse.map(item => item.metrics.reliability),
              backgroundColor: colors[4],
              borderColor: borderColors[4],
              borderWidth: 1,
            },
            {
              label: 'Performance Score',
              data: dataToUse.map(item => item.metrics.performanceScore),
              backgroundColor: colors[5],
              borderColor: borderColors[5],
              borderWidth: 1,
            },
          ]
        : [
            {
              label: selectedMetric.charAt(0).toUpperCase() + selectedMetric.slice(1),
              data: dataToUse.map(item => item.metrics[selectedMetric as keyof ComparisonItem['metrics']]),
              backgroundColor: colors[0],
              borderColor: borderColors[0],
              borderWidth: 1,
            },
          ]

      return { labels, datasets }
    }
  }, [normalizedData, selectedMetric, currentChartType, showBaseline])

  // Chart options
  const chartOptions: ChartOptions<'radar' | 'bar'> = useMemo(() => {
    const baseOptions = {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        title: {
          display: true,
          text: `Performance Comparison${selectedMetric !== 'all' ? ` - ${selectedMetric}` : ''}`,
          font: { size: 16, weight: 'bold' },
        },
        legend: {
          display: true,
          position: 'top' as const,
        },
        tooltip: {
          callbacks: {
            label: (context: TooltipItem<'radar' | 'bar'>) => {
              const value = context.parsed.y ?? context.parsed.r
              const metricName = context.dataset.label || selectedMetric
              
              // Format value based on metric type
              let formattedValue: string
              if (normalizeData) {
                formattedValue = `${value.toFixed(1)}%`
              } else {
                switch (metricName.toLowerCase()) {
                  case 'latency':
                    formattedValue = formatLatency(value)
                    break
                  case 'throughput':
                    formattedValue = formatThroughput(value)
                    break
                  case 'success rate':
                  case 'error rate':
                  case 'reliability':
                    formattedValue = formatPercentage(value)
                    break
                  case 'performance score':
                    formattedValue = `${Math.round(value)}/100`
                    break
                  default:
                    formattedValue = value.toFixed(2)
                }
              }

              return `${metricName}: ${formattedValue}`
            },
            afterLabel: (context: TooltipItem<'radar' | 'bar'>) => {
              const item = normalizedData[context.dataIndex] || normalizedData[0]
              const lines: string[] = []
              
              if (item.baseline && showBaseline) {
                const baselineValue = item.baseline[selectedMetric as keyof BaselineData]
                const currentValue = item.metrics[selectedMetric as keyof ComparisonItem['metrics']]
                const delta = ((currentValue - baselineValue) / baselineValue) * 100
                lines.push(`Baseline: ${baselineValue.toFixed(2)}`)
                lines.push(`Delta: ${delta > 0 ? '+' : ''}${delta.toFixed(1)}%`)
              }

              if (interactive) {
                lines.push('Click for details')
              }

              return lines
            },
          },
        },
      },
      onClick: interactive ? (_event: any, elements: any[]) => {
        if (elements.length > 0) {
          const element = elements[0]
          const item = normalizedData[element.index]
          setSelectedItem(item)
          onDataPointClick?.(item, selectedMetric)
        }
      } : undefined,
    }

    if (currentChartType === 'radar') {
      return {
        ...baseOptions,
        scales: {
          r: {
            beginAtZero: true,
            max: normalizeData ? 100 : undefined,
            ticks: {
              callback: (value: any) => normalizeData ? `${value}%` : value,
            },
          },
        },
      } as ChartOptions<'radar'>
    } else {
      return {
        ...baseOptions,
        scales: {
          x: {
            title: {
              display: true,
              text: comparisonType === 'client' ? 'Clients' : 'Methods',
            },
          },
          y: {
            title: {
              display: true,
              text: normalizeData ? 'Normalized Score (%)' : 'Value',
            },
            beginAtZero: true,
            max: normalizeData ? 100 : undefined,
            ticks: {
              callback: (value: any) => normalizeData ? `${value}%` : value,
            },
          },
        },
      } as ChartOptions<'bar'>
    }
  }, [normalizedData, selectedMetric, currentChartType, showBaseline, interactive, comparisonType, normalizeData, onDataPointClick])

  // Event handlers
  const handleMetricChange = useCallback((newMetric: string) => {
    setSelectedMetric(newMetric)
  }, [])

  const handleChartTypeChange = useCallback(() => {
    setCurrentChartType(prev => prev === 'radar' ? 'bar' : 'radar')
  }, [])

  const handleNormalizeToggle = useCallback(() => {
    setNormalizeData(prev => !prev)
  }, [])

  if (!data || comparisonData.length === 0) {
    return (
      <div className={`card ${className}`} style={{ height }}>
        <div className="card-content flex items-center justify-center h-full">
          <LoadingSpinner size="lg" />
        </div>
      </div>
    )
  }

  return (
    <div className={`card ${className}`}>
      {/* Controls */}
      <div className="card-header space-y-4">
        <div className="flex justify-between items-center">
          <h3 className="text-lg font-medium text-gray-900">
            Performance Comparison
          </h3>
          <div className="flex items-center space-x-2">
            <button
              onClick={handleNormalizeToggle}
              className={`btn-outline p-2 ${normalizeData ? 'bg-blue-50' : ''}`}
              title="Normalize Data"
            >
              <AdjustmentsHorizontalIcon className="h-4 w-4" />
            </button>
            <button
              onClick={handleChartTypeChange}
              className="btn-outline p-2"
              title={`Switch to ${currentChartType === 'radar' ? 'Bar' : 'Radar'} Chart`}
            >
              {currentChartType === 'radar' ? <ChartBarIcon className="h-4 w-4" /> : <CircleStackIcon className="h-4 w-4" />}
            </button>
          </div>
        </div>

        {/* Metric Selection */}
        <div className="flex flex-wrap gap-2">
          {['all', 'latency', 'throughput', 'success_rate', 'reliability', 'performance_score'].map(metricOption => (
            <button
              key={metricOption}
              onClick={() => handleMetricChange(metricOption)}
              className={`btn-outline text-sm ${selectedMetric === metricOption ? 'bg-blue-50 border-blue-300' : ''}`}
            >
              {metricOption === 'all' ? 'All Metrics' : metricOption.replace('_', ' ').replace(/\b\w/g, l => l.toUpperCase())}
            </button>
          ))}
        </div>
      </div>

      {/* Chart */}
      <div className="card-content">
        <div style={{ height: height - 160 }}>
          {currentChartType === 'radar' ? (
            <Radar
              ref={chartRef as React.RefObject<ChartJS<'radar'>>}
              data={chartData}
              options={chartOptions as ChartOptions<'radar'>}
            />
          ) : (
            <Bar
              ref={chartRef as React.RefObject<ChartJS<'bar'>>}
              data={chartData}
              options={chartOptions as ChartOptions<'bar'>}
            />
          )}
        </div>
      </div>

      {/* Summary Stats */}
      <div className="card-footer">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="text-center">
            <div className="text-2xl font-semibold text-gray-900">
              {comparisonData.length}
            </div>
            <div className="text-sm text-gray-500">
              {comparisonType === 'client' ? 'Clients' : 'Methods'}
            </div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-semibold text-gray-900">
              {Math.round(comparisonData.reduce((sum, item) => sum + item.metrics.performanceScore, 0) / comparisonData.length)}
            </div>
            <div className="text-sm text-gray-500">Avg Score</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-semibold text-gray-900">
              {comparisonData.length > 0 
                ? formatLatencyValue(comparisonData.reduce((sum, item) => sum + item.metrics.latency, 0) / comparisonData.length)
                : 'N/A'
              }
            </div>
            <div className="text-sm text-gray-500">Avg Latency</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-semibold text-gray-900">
              {formatPercentage(comparisonData.reduce((sum, item) => sum + item.metrics.successRate, 0) / comparisonData.length)}
            </div>
            <div className="text-sm text-gray-500">Avg Success Rate</div>
          </div>
        </div>

        {normalizeData && (
          <div className="mt-4 p-2 bg-blue-50 rounded-lg">
            <div className="flex items-center text-sm text-blue-700">
              <InformationCircleIcon className="h-4 w-4 mr-2" />
              Data is normalized to 0-100% scale for better comparison across different metrics
            </div>
          </div>
        )}
      </div>

      {/* Selected Item Details */}
      {selectedItem && (
        <div className="card-footer border-t">
          <div className="bg-gray-50 p-3 rounded-lg">
            <div className="text-sm font-medium text-gray-900 mb-2">
              {selectedItem.name} Details
            </div>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-4 text-sm">
              <div>
                <span className="text-gray-500">Latency:</span> {formatLatencyValue(selectedItem.metrics.latency)}
              </div>
              <div>
                <span className="text-gray-500">Throughput:</span> {formatThroughput(selectedItem.metrics.throughput)}
              </div>
              <div>
                <span className="text-gray-500">Success Rate:</span> {formatPercentage(selectedItem.metrics.successRate)}
              </div>
              <div>
                <span className="text-gray-500">Error Rate:</span> {formatPercentage(selectedItem.metrics.errorRate)}
              </div>
              <div>
                <span className="text-gray-500">Reliability:</span> {formatPercentage(selectedItem.metrics.reliability)}
              </div>
              <div>
                <span className="text-gray-500">Performance Score:</span> {Math.round(selectedItem.metrics.performanceScore)}/100
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default PerformanceComparisonChart