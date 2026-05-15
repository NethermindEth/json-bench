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

const TYPICAL_PERCENTILES = [
  { key: 'p50', label: 'p50', color: '#3b82f6' },
  { key: 'p95', label: 'p95', color: '#8b5cf6' },
  { key: 'p99', label: 'p99', color: '#f59e0b' },
] as const

const MAX_COLOR = '#ef4444'

/**
 * Compares latency percentiles across clients. Split into two panels so the
 * worst-case max (often an order of magnitude above p99) doesn't compress the
 * typical-case bars into the floor of a shared y-axis:
 *
 *   ┌─────────── Typical (p50 / p95 / p99) ───────────┬─── Worst-case (max) ───┐
 *   │                                                  │                        │
 *
 * Each panel has its own y-scale, so both "how fast is the common case?" and
 * "how bad does the tail get?" are readable at the same time. Click any bar
 * to filter the table below.
 */
export default function PerClientPercentilesChart({ clientMetrics, height = 320, onClientClick }: Props) {
  const headRef = useRef<any>(null)
  const tailRef = useRef<any>(null)

  const sorted = useMemo(
    () => [...clientMetrics].sort((a, b) => a.clientName.localeCompare(b.clientName)),
    [clientMetrics],
  )

  // Typical-case data: p50/p95/p99 grouped per client.
  const headData = useMemo(() => ({
    labels: sorted.map(c => c.clientName),
    datasets: TYPICAL_PERCENTILES.map(({ key, label, color }) => ({
      label,
      data: sorted.map(c => c.latencyPercentiles[key as keyof typeof c.latencyPercentiles] ?? 0),
      backgroundColor: color,
      borderColor: color,
      borderWidth: 1,
    })),
  }), [sorted])

  // Worst-case data: just max, one bar per client.
  const tailData = useMemo(() => ({
    labels: sorted.map(c => c.clientName),
    datasets: [{
      label: 'max',
      data: sorted.map(c => c.latencyPercentiles.max ?? 0),
      backgroundColor: MAX_COLOR,
      borderColor: MAX_COLOR,
      borderWidth: 1,
    }],
  }), [sorted])

  const makeOptions = (title: string, showLegend: boolean): ChartOptions<'bar'> => ({
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index' as const, intersect: false },
    plugins: {
      title: { display: true, text: title },
      legend: { position: 'top' as const, display: showLegend },
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
  })

  const headOptions = useMemo(() => makeOptions('Typical (p50 / p95 / p99)', true), [sorted, onClientClick])
  const tailOptions = useMemo(() => makeOptions('Worst-case (max)', false), [sorted, onClientClick])

  if (sorted.length === 0) {
    return <p className="text-sm text-gray-500 dark:text-slate-400">No per-client latency data available for this run.</p>
  }

  return (
    // 2:1 ratio. Typical panel gets twice the horizontal real estate because
    // it carries 3 bars per client; the worst-case panel only has one bar per
    // client, so it doesn't need as much width.
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-4" style={{ height }}>
      <div className="lg:col-span-2" style={{ position: 'relative' }}>
        <Bar ref={headRef} data={headData} options={headOptions} />
      </div>
      <div style={{ position: 'relative' }}>
        <Bar ref={tailRef} data={tailData} options={tailOptions} />
      </div>
    </div>
  )
}
