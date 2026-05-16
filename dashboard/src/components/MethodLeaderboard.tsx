import { useMemo, useState } from 'react'
import { TrophyIcon } from '@heroicons/react/24/outline'
import type { MethodMetricsData } from '../types/api'
import { formatLatency, formatPercentage } from '../utils/metric-formatters'

type MetricKey = 'p95' | 'p99' | 'avg' | 'throughput' | 'success_rate' | 'error_rate'

const METRICS: { key: MetricKey; label: string; field: string; format: (v: number) => string; lowerIsBetter: boolean }[] = [
  { key: 'p95', label: 'p95 latency', field: 'p95_latency', format: formatLatency, lowerIsBetter: true },
  { key: 'p99', label: 'p99 latency', field: 'p99_latency', format: formatLatency, lowerIsBetter: true },
  { key: 'avg', label: 'Avg latency', field: 'avg_latency', format: formatLatency, lowerIsBetter: true },
  { key: 'throughput', label: 'Throughput', field: 'throughput', format: (v) => `${v.toFixed(1)} req/s`, lowerIsBetter: false },
  { key: 'success_rate', label: 'Success rate', field: 'success_rate', format: formatPercentage, lowerIsBetter: false },
  { key: 'error_rate', label: 'Error rate', field: 'error_rate', format: formatPercentage, lowerIsBetter: true },
]

interface Props {
  /**
   * Pre-fetched methods_by_client maps from one or more runs. Index 0 is
   * treated as the "current" run when caller wants single-run drill-down,
   * but the component aggregates across all entries by averaging metrics
   * weighted by total_requests.
   */
  runs: Pick<MethodMetricsData, 'methods_by_client'>[]
  /** Optional sticky title above the picker. */
  title?: string
}

interface Row { client: string; value: number; samples: number; requests: number }

export default function MethodLeaderboard({ runs, title = 'Per-method leaderboard' }: Props) {
  const methodsByClient = useMemo(
    () => runs.map(r => r.methods_by_client ?? {}),
    [runs],
  )

  // Set of method names across all runs and clients
  const allMethods = useMemo(() => {
    const s = new Set<string>()
    for (const mbc of methodsByClient) {
      for (const methodMap of Object.values(mbc)) {
        Object.keys(methodMap || {}).forEach(m => s.add(m))
      }
    }
    return Array.from(s).sort()
  }, [methodsByClient])

  const [method, setMethod] = useState<string>('')
  const [metric, setMetric] = useState<MetricKey>('p95')

  // First time data arrives, seed a selection.
  if (!method && allMethods.length > 0) {
    setMethod(allMethods[0])
  }

  const ranking = useMemo<Row[]>(() => {
    if (!method) return []
    const m = METRICS.find(x => x.key === metric)!
    const agg = new Map<string, { sum: number; weight: number; samples: number; requests: number }>()
    for (const mbc of methodsByClient) {
      for (const [client, methods] of Object.entries(mbc)) {
        const mm = methods?.[method] as Record<string, number | null> | undefined
        if (!mm) continue
        const raw = mm[m.field]
        if (typeof raw !== 'number' || !Number.isFinite(raw)) continue
        const requests = (mm['total_requests'] as number | null) ?? 1
        const cur = agg.get(client) ?? { sum: 0, weight: 0, samples: 0, requests: 0 }
        cur.sum += raw * requests
        cur.weight += requests
        cur.samples += 1
        cur.requests += requests
        agg.set(client, cur)
      }
    }
    const rows: Row[] = Array.from(agg.entries()).map(([client, v]) => ({
      client,
      value: v.weight > 0 ? v.sum / v.weight : 0,
      samples: v.samples,
      requests: v.requests,
    }))
    rows.sort((a, b) => (m.lowerIsBetter ? a.value - b.value : b.value - a.value))
    return rows
  }, [methodsByClient, method, metric])

  const metricDef = METRICS.find(m => m.key === metric)!
  const maxAbs = ranking.length ? Math.max(...ranking.map(r => Math.abs(r.value))) : 1

  return (
    <section className="card">
      <div className="card-header flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-base font-semibold text-gray-900 dark:text-slate-100">
          <TrophyIcon className="h-4 w-4 text-warning-500" />
          {title}
        </h2>
        <div className="flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-2 text-xs text-gray-500 dark:text-slate-400">
            Method
            <select
              value={method}
              onChange={(e) => setMethod(e.target.value)}
              className="input py-1 text-xs"
              disabled={allMethods.length === 0}
            >
              {allMethods.length === 0 && <option value="">No methods</option>}
              {allMethods.map(m => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </label>
          <div className="flex items-center gap-1 overflow-x-auto" role="tablist" aria-label="Method metric">
            {METRICS.map(m => (
              <button
                key={m.key}
                type="button"
                role="tab"
                aria-selected={metric === m.key}
                onClick={() => setMetric(m.key)}
                className={`whitespace-nowrap rounded px-2 py-1 text-xs font-medium transition-colors ${
                  metric === m.key
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300'
                    : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100'
                }`}
              >
                {m.label}
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="card-content">
        {ranking.length === 0 ? (
          <p className="text-sm text-gray-500 dark:text-slate-400">
            No per-method data available yet for this test.
          </p>
        ) : (
          <ol className="space-y-2">
            {ranking.map((r, i) => {
              const pct = maxAbs > 0 ? (Math.abs(r.value) / maxAbs) * 100 : 0
              return (
                <li key={r.client} className="flex items-center gap-3">
                  <span className="w-5 text-right text-xs font-mono text-gray-400 dark:text-slate-500">
                    {i + 1}
                  </span>
                  <span className="w-28 truncate font-mono text-sm text-gray-900 dark:text-slate-100">
                    {r.client}
                  </span>
                  <div className="relative h-2 flex-1 overflow-hidden rounded bg-gray-100 dark:bg-slate-800">
                    <div
                      className={`absolute inset-y-0 left-0 rounded ${
                        i === 0
                          ? 'bg-success-500'
                          : i === ranking.length - 1
                          ? 'bg-danger-400'
                          : 'bg-primary-400'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <span className="w-24 text-right font-mono text-sm text-gray-900 dark:text-slate-100">
                    {metricDef.format(r.value)}
                  </span>
                  <span className="w-24 text-right text-[11px] text-gray-400 dark:text-slate-500">
                    {r.requests.toLocaleString()} reqs
                  </span>
                </li>
              )
            })}
          </ol>
        )}
      </div>
    </section>
  )
}
