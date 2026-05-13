import { useMemo, useRef } from 'react'
import {
  Chart as ChartJS,
  LineElement,
  PointElement,
  CategoryScale,
  LinearScale,
  TimeScale,
  Tooltip,
  Legend,
  type ChartOptions,
} from 'chart.js'
import 'chartjs-adapter-date-fns'
import { Line } from 'react-chartjs-2'
import { useTheme } from '../contexts/ThemeContext'

ChartJS.register(LineElement, PointElement, CategoryScale, LinearScale, TimeScale, Tooltip, Legend)

export interface ClientTrendPoint {
  t: string
  v: number
  // Optional metadata. `versionChange` marks the run at which this client
  // switched to a new web3_clientVersion string; the chart draws a heavier
  // point + reveals the version in the tooltip.
  versionChange?: string
  version?: string
}

export interface ClientTrendSeries {
  client: string
  // Points must be sorted oldest → newest by timestamp.
  points: ClientTrendPoint[]
}

interface Props {
  series: ClientTrendSeries[]
  metricLabel: string
  unit?: string
  height?: number
}

const PALETTE = ['#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#a855f7', '#0ea5e9', '#14b8a6']

export default function PerClientTrendChart({ series, metricLabel, unit = 'ms', height = 280 }: Props) {
  const chartRef = useRef<any>(null)
  const { isDark } = useTheme()

  const data = useMemo(() => ({
    datasets: series.map((s, i) => {
      const color = PALETTE[i % PALETTE.length]
      return {
        label: s.client,
        data: s.points.map(p => ({
          x: p.t,
          y: p.v,
          // Pipe metadata onto the point so the tooltip callback can reach it.
          versionChange: p.versionChange,
          version: p.version,
        })),
        borderColor: color,
        backgroundColor: color,
        tension: 0.25,
        // Enlarge & ring the point when it marks a version transition.
        pointRadius: s.points.map(p => (p.versionChange ? 6 : 3)),
        pointHoverRadius: s.points.map(p => (p.versionChange ? 8 : 5)),
        pointStyle: s.points.map(p => (p.versionChange ? 'rectRot' : 'circle')),
        pointBackgroundColor: s.points.map(p => (p.versionChange ? '#fbbf24' : color)),
        pointBorderColor: s.points.map(p => (p.versionChange ? color : 'transparent')),
        pointBorderWidth: s.points.map(p => (p.versionChange ? 2 : 0)),
        borderWidth: 2,
      }
    }),
  }), [series])

  const options: ChartOptions<'line'> = useMemo(() => {
    const gridCol = isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(148, 163, 184, 0.25)'
    const tickCol = isDark ? '#94a3b8' : '#475569'
    return {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index' as const, intersect: false },
      plugins: {
        legend: {
          position: 'top' as const,
          labels: { color: tickCol, boxWidth: 12, boxHeight: 4 },
        },
        tooltip: {
          callbacks: {
            label: (ctx) => {
              const lines = [`${ctx.dataset.label}: ${(ctx.parsed.y as number).toFixed(2)}${unit}`]
              const raw = ctx.raw as { versionChange?: string; version?: string } | undefined
              if (raw?.versionChange) {
                lines.push(`▲ version → ${raw.versionChange}`)
              } else if (raw?.version) {
                lines.push(`build: ${raw.version}`)
              }
              return lines
            },
          },
        },
      },
      scales: {
        x: {
          type: 'time',
          time: { tooltipFormat: 'PP HH:mm' },
          grid: { color: gridCol },
          ticks: { color: tickCol, maxRotation: 0, autoSkip: true, maxTicksLimit: 6 },
        },
        y: {
          beginAtZero: true,
          grid: { color: gridCol },
          ticks: {
            color: tickCol,
            callback: (v) => `${Number(v).toFixed(0)}${unit}`,
          },
          title: { display: true, text: metricLabel, color: tickCol },
        },
      },
    }
  }, [isDark, metricLabel, unit])

  if (series.length === 0 || series.every(s => s.points.length === 0)) {
    return <p className="text-sm text-gray-500 dark:text-slate-400">No per-client trend data available yet.</p>
  }

  return (
    <div style={{ height }}>
      <Line ref={chartRef} data={data} options={options} />
    </div>
  )
}
