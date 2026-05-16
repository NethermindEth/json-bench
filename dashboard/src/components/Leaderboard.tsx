import { useMemo, useState } from 'react'
import { useQueries } from '@tanstack/react-query'
import { TrophyIcon } from '@heroicons/react/24/outline'
import { useAPI } from '../api/hooks'
import { formatLatency, formatPercentage } from '../utils/metric-formatters'
import type { TestGroup } from '../utils/run-aggregations'

type MetricKey = 'p95' | 'p99' | 'avg' | 'throughput' | 'success_rate'

const METRICS: { key: MetricKey; label: string; format: (v: number) => string; lowerIsBetter: boolean }[] = [
  { key: 'p95', label: 'p95 latency', format: formatLatency, lowerIsBetter: true },
  { key: 'p99', label: 'p99 latency', format: formatLatency, lowerIsBetter: true },
  { key: 'avg', label: 'Avg latency', format: formatLatency, lowerIsBetter: true },
  { key: 'throughput', label: 'Throughput', format: (v) => `${v.toFixed(1)} req/s`, lowerIsBetter: false },
  { key: 'success_rate', label: 'Success rate', format: formatPercentage, lowerIsBetter: false },
]

interface Props {
  groups: TestGroup[]
  clientFilter?: string | null
}

interface PerClientAgg {
  client: string
  sum: number
  weight: number
  samples: number
}

/**
 * Pulls per-client metrics from the latest run of each test group and
 * computes a request-weighted average per client across all tests.
 *
 * Caveats:
 *   - Only inspects the latest run per test, not the full history.
 *   - Uses simple weighted averages (not percentile-of-percentiles math).
 *     Good enough for a "who is winning right now" view.
 */
export default function Leaderboard({ groups, clientFilter }: Props) {
  const api = useAPI()
  const [metric, setMetric] = useState<MetricKey>('p95')

  // Fetch the detailed run (with client_metrics) for the latest run in each group
  const queries = useQueries({
    queries: groups.map(g => ({
      queryKey: ['run-detail', g.latestRun.id],
      queryFn: () => api.getRun(g.latestRun.id),
      staleTime: 5 * 60 * 1000,
    })),
  })

  const isLoading = queries.some(q => q.isLoading)

  const ranking = useMemo(() => {
    const agg = new Map<string, PerClientAgg>()
    for (const q of queries) {
      const detail = q.data
      if (!detail?.client_metrics) continue
      for (const [client, m] of Object.entries(detail.client_metrics)) {
        // Don't drop clients from the ranking when a chip filter is active.
        // The leaderboard's value is the comparison; filtering it to one
        // client would just show that client vs itself. We highlight the
        // filtered client's row below instead.
        const cm = m as Record<string, unknown>
        const latency = cm.latency as Record<string, number> | undefined
        let value: number | undefined
        switch (metric) {
          case 'p95': value = latency?.p95; break
          case 'p99': value = latency?.p99; break
          case 'avg': value = latency?.avg; break
          case 'throughput': value = (cm.throughput as number) ?? latency?.throughput; break
          case 'success_rate': value = cm.success_rate as number; break
        }
        if (typeof value !== 'number' || !Number.isFinite(value) || value === 0) {
          if (metric !== 'success_rate') continue
        }
        const weight = (cm.total_requests as number) || 1
        const cur = agg.get(client) ?? { client, sum: 0, weight: 0, samples: 0 }
        cur.sum += (value ?? 0) * weight
        cur.weight += weight
        cur.samples += 1
        agg.set(client, cur)
      }
    }
    const m = METRICS.find(x => x.key === metric)!
    const rows = Array.from(agg.values())
      .map(a => ({
        client: a.client,
        value: a.weight > 0 ? a.sum / a.weight : 0,
        samples: a.samples,
      }))
      .filter(r => Number.isFinite(r.value))
      .sort((a, b) => (m.lowerIsBetter ? a.value - b.value : b.value - a.value))
    return { rows, metric: m }
  }, [queries, metric])

  const { rows, metric: activeMetric } = ranking
  const maxAbs = rows.length ? Math.max(...rows.map(r => Math.abs(r.value))) : 1

  return (
    <section className="card">
      <div className="card-header flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 text-base font-semibold text-gray-900 dark:text-slate-100">
          <TrophyIcon className="h-4 w-4 text-warning-500" />
          Client leaderboard
          <span className="text-xs font-normal text-gray-500 dark:text-slate-500">
            ({groups.length} test{groups.length === 1 ? '' : 's'}, latest run each)
          </span>
        </h2>
        <div className="flex items-center gap-1 overflow-x-auto" role="tablist" aria-label="Leaderboard metric">
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
      <div className="card-content">
        {isLoading && rows.length === 0 && (
          <p className="text-sm text-gray-500 dark:text-slate-400">Loading per-client metrics…</p>
        )}
        {!isLoading && rows.length === 0 && (
          <p className="text-sm text-gray-500 dark:text-slate-400">
            No per-client metrics available yet.
          </p>
        )}
        {rows.length > 0 && (
          <ol className="space-y-1">
            {rows.map((r, i) => {
              const pct = maxAbs > 0 ? (Math.abs(r.value) / maxAbs) * 100 : 0
              const isFocused = clientFilter === r.client
              const isDimmed = !!clientFilter && !isFocused
              return (
                <li
                  key={r.client}
                  aria-current={isFocused ? 'true' : undefined}
                  className={`flex items-center gap-3 rounded px-1.5 py-1 transition-colors ${
                    isFocused
                      ? 'bg-primary-50 ring-1 ring-primary-200 dark:bg-primary-900/30 dark:ring-primary-700'
                      : ''
                  } ${isDimmed ? 'opacity-50' : ''}`}
                >
                  <span className="w-5 text-right text-xs font-mono text-gray-400 dark:text-slate-500">
                    {i + 1}
                  </span>
                  <span className={`w-28 truncate font-mono text-sm ${
                    isFocused
                      ? 'font-semibold text-primary-700 dark:text-primary-300'
                      : 'text-gray-900 dark:text-slate-100'
                  }`}>
                    {r.client}
                  </span>
                  <div className="relative h-2 flex-1 overflow-hidden rounded bg-gray-100 dark:bg-slate-800">
                    <div
                      className={`absolute inset-y-0 left-0 rounded ${
                        i === 0
                          ? 'bg-success-500'
                          : i === rows.length - 1
                          ? 'bg-danger-400'
                          : 'bg-primary-400'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <span className="w-24 text-right font-mono text-sm text-gray-900 dark:text-slate-100">
                    {activeMetric.format(r.value)}
                  </span>
                  <span className="w-14 text-right text-[11px] text-gray-400 dark:text-slate-500">
                    {r.samples} test{r.samples === 1 ? '' : 's'}
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
