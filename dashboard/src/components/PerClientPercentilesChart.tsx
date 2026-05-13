import { useMemo, useRef } from 'react'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  type ChartOptions,
} from 'chart.js'
import { Bar } from 'react-chartjs-2'
import type { ClientMetrics } from '../types/detailed-metrics'

ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend)

interface Props {
  clientMetrics: ClientMetrics[]
  height?: number
  onClientClick?: (clientName: string) => void
}

const PERCENTILES = [
  { key: 'p50', label: 'p50', color: '#3b82f6' },
  { key: 'p95', label: 'p95', color: '#8b5cf6' },
  { key: 'p99', label: 'p99', color: '#f59e0b' },
  { key: 'max', label: 'max', color: '#ef4444' },
] as const

/**
 * Compares latency percentiles across clients. The data comes from the run's
 * actual per-client measurements (avg, p50, p95, p99, max) — no synthetic
 * bucketing — so the chart answers "which client is slowest at the tail?"
 * directly.
 */
export default function PerClientPercentilesChart({ clientMetrics, height = 320, onClientClick }: Props) {
  const chartRef = useRef<any>(null)
  const sorted = useMemo(
    () => [...clientMetrics].sort((a, b) => a.clientName.localeCompare(b.clientName)),
    [clientMetrics],
  )

  const data = useMemo(() => ({
    labels: sorted.map(c => c.clientName),
    datasets: PERCENTILES.map(({ key, label, color }) => ({
      label,
      data: sorted.map(c => c.latencyPercentiles[key as keyof typeof c.latencyPercentiles] ?? 0),
      backgroundColor: color,
      borderColor: color,
      borderWidth: 1,
    })),
  }), [sorted])

  const options: ChartOptions<'bar'> = useMemo(() => ({
    responsive: true,
    maintainAspectRatio: false,
    // mode:index lets clicks anywhere over a client's group fire the handler,
    // matching the UX of the throughput chart on Dashboard.
    interaction: { mode: 'index' as const, intersect: false },
    plugins: {
      legend: { position: 'top' as const },
      tooltip: {
        callbacks: {
          label: (ctx) => `${ctx.dataset.label}: ${Number(ctx.parsed.y).toFixed(2)} ms`,
        },
      },
    },
    scales: {
      x: { title: { display: true, text: 'Client' } },
      y: {
        title: { display: true, text: 'Latency (ms)' },
        beginAtZero: true,
      },
    },
    onClick: (_evt, elements) => {
      if (!onClientClick || elements.length === 0) return
      const idx = elements[0].index
      const name = sorted[idx]?.clientName
      if (name) onClientClick(name)
    },
  }), [sorted, onClientClick])

  if (sorted.length === 0) {
    return <p className="text-sm text-gray-500 dark:text-slate-400">No per-client latency data available for this run.</p>
  }

  return (
    <div style={{ height }}>
      <Bar ref={chartRef} data={data} options={options} />
    </div>
  )
}
