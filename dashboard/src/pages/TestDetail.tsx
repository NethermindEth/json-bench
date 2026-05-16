import { Link, useParams } from 'react-router-dom'
import { useMemo, useState } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { Helmet } from 'react-helmet-async'
import { ArrowLeftIcon, ClockIcon, BeakerIcon } from '@heroicons/react/24/outline'
import { useAPI } from '../api/hooks'
import { formatAge, classifyActivity } from '../utils/run-aggregations'
import { formatLatency, formatPercentage } from '../utils/metric-formatters'
import PerClientTrendChart, { ClientTrendSeries } from '../components/PerClientTrendChart'
import MethodLeaderboard from '../components/MethodLeaderboard'
import CurrentBuildsPanel from '../components/CurrentBuildsPanel'

// Reasonable cap on how many recent runs we'll detail-fetch per page load.
const DETAIL_LIMIT = 20

/**
 * /tests/:name — drill-down for one test_name showing every run, a per-client
 * p95-over-time trend chart, and a per-method leaderboard.
 */
export default function TestDetail() {
  const { name = '' } = useParams<{ name: string }>()
  const testName = decodeURIComponent(name)
  const api = useAPI()

  const { data: runResp, isLoading: runsLoading } = useQuery({
    queryKey: ['test-runs', testName],
    queryFn: () => api.listRuns({ testName, limit: 100 }),
    enabled: !!testName,
  })
  const runs = useMemo(() => {
    const list = runResp?.runs ?? []
    return [...list].sort(
      (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime(),
    )
  }, [runResp])

  const recent = useMemo(() => runs.slice(0, DETAIL_LIMIT), [runs])

  // Detail fetches (client_metrics) + method-metrics for the recent slice.
  const detailQueries = useQueries({
    queries: recent.map(r => ({
      queryKey: ['run-detail', r.id],
      queryFn: () => api.getRun(r.id),
      staleTime: 5 * 60 * 1000,
    })),
  })
  const methodQueries = useQueries({
    queries: recent.map(r => ({
      queryKey: ['run-methods', r.id],
      queryFn: () => api.getRunMethods(r.id),
      staleTime: 5 * 60 * 1000,
    })),
  })

  // ─── Trend chart configuration ───────────────────────────────────────────
  // Two axes the user can pivot on:
  //   - method: "" → aggregate (across all methods, from client_metrics.latency)
  //             or a specific method name → from methods_by_client[client][method]
  //   - metric: which percentile / aggregate to plot
  const TREND_METRICS = [
    { key: 'p95', label: 'p95', field: 'p95_latency' },
    { key: 'p99', label: 'p99', field: 'p99_latency' },
    { key: 'avg', label: 'avg', field: 'avg_latency' },
  ] as const
  type TrendMetric = (typeof TREND_METRICS)[number]['key']
  const [trendMethod, setTrendMethod] = useState<string>('')
  const [trendMetric, setTrendMetric] = useState<TrendMetric>('p95')

  const allMethods = useMemo(() => {
    const s = new Set<string>()
    for (const q of methodQueries) {
      const mbc = q.data?.methods_by_client ?? {}
      for (const methods of Object.values(mbc)) {
        Object.keys(methods || {}).forEach(m => s.add(m))
      }
    }
    return Array.from(s).sort()
  }, [methodQueries])

  // Build series: one line per client over time, value depends on method+metric.
  // Annotate each point with its client_versions[client] string and flag the
  // first run after a version change so the chart can mark it.
  const clientSeries = useMemo<ClientTrendSeries[]>(() => {
    type RawPoint = { t: string; v: number; version?: string }
    const byClient = new Map<string, RawPoint[]>()

    const pushPoint = (client: string, run: { timestamp: string }, v: number, runIdx: number) => {
      const version = recent[runIdx]?.client_versions?.[client]
      const list = byClient.get(client) ?? []
      list.push({ t: run.timestamp, v, version })
      byClient.set(client, list)
    }

    if (trendMethod === '') {
      detailQueries.forEach((q, i) => {
        const detail = q.data
        const run = recent[i]
        if (!detail?.client_metrics || !run) return
        for (const [client, m] of Object.entries(detail.client_metrics)) {
          const cm = m as Record<string, any>
          const lat = (cm.latency ?? {}) as Record<string, number | undefined>
          const v = trendMetric === 'p95' ? lat.p95
                  : trendMetric === 'p99' ? lat.p99
                  : lat.avg
          if (typeof v !== 'number' || !Number.isFinite(v)) continue
          pushPoint(client, run, v, i)
        }
      })
    } else {
      const field = TREND_METRICS.find(m => m.key === trendMetric)!.field
      methodQueries.forEach((q, i) => {
        const mbc = q.data?.methods_by_client ?? {}
        const run = recent[i]
        if (!run) return
        for (const [client, methods] of Object.entries(mbc)) {
          const mm = (methods ?? {})[trendMethod] as Record<string, number | null> | undefined
          if (!mm) continue
          const raw = mm[field]
          if (typeof raw !== 'number' || !Number.isFinite(raw)) continue
          pushPoint(client, run, raw, i)
        }
      })
    }

    return Array.from(byClient.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([client, pts]) => {
        const sorted = pts.sort((a, b) => new Date(a.t).getTime() - new Date(b.t).getTime())
        // Walk the timeline to flag version transitions. We mark the *new*
        // point's `versionChange`; tooltip there reads "version → <new>".
        let lastVersion: string | undefined
        const annotated = sorted.map(p => {
          const isChange =
            !!p.version && p.version !== 'unknown' && lastVersion !== undefined && p.version !== lastVersion
          const out: ClientTrendSeries['points'][number] = {
            t: p.t,
            v: p.v,
            version: p.version,
            ...(isChange ? { versionChange: p.version } : {}),
          }
          if (p.version && p.version !== 'unknown') lastVersion = p.version
          return out
        })
        return { client, points: annotated }
      })
  }, [trendMethod, trendMetric, detailQueries, methodQueries, recent])

  const summary = useMemo(() => {
    if (runs.length === 0) return null
    const latest = runs[0]
    const sumSuccess = runs.reduce((s, r) => s + (r.success_rate ?? 0), 0)
    const validP95 = runs.filter(r => r.p95_latency_ms > 0)
    return {
      latest,
      total: runs.length,
      avgSuccess: sumSuccess / runs.length,
      avgP95: validP95.length
        ? validP95.reduce((s, r) => s + r.p95_latency_ms, 0) / validP95.length
        : 0,
      firstRun: runs[runs.length - 1],
    }
  }, [runs])

  if (runsLoading) {
    return (
      <div className="text-sm text-gray-500 dark:text-slate-400">Loading test history…</div>
    )
  }
  if (!summary) {
    return (
      <div className="card">
        <div className="card-content">
          <p className="text-sm text-gray-700 dark:text-slate-300">
            No runs found for test <span className="font-mono">{testName}</span>.
          </p>
          <Link to="/" className="mt-3 inline-flex items-center gap-1 text-xs text-primary-600 hover:underline dark:text-primary-300">
            <ArrowLeftIcon className="h-3 w-3" /> Back to overview
          </Link>
        </div>
      </div>
    )
  }

  const activity = classifyActivity(summary.latest.timestamp)

  return (
    <>
      <Helmet><title>{testName}</title></Helmet>

      <div className="mb-4 flex items-center justify-between gap-3">
        <Link
          to="/"
          className="inline-flex items-center gap-1 text-xs text-primary-600 hover:underline dark:text-primary-300"
        >
          <ArrowLeftIcon className="h-3 w-3" /> Overview
        </Link>
        <Link
          to={`/runs/${summary.latest.id}`}
          className="text-xs text-primary-600 hover:underline dark:text-primary-300"
        >
          Open latest run →
        </Link>
      </div>

      <header className="card mb-6">
        <div className="card-content space-y-3">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h1 className="flex items-center gap-2 text-2xl font-bold text-gray-900 dark:text-slate-100">
                <BeakerIcon className="h-5 w-5 text-primary-500" />
                {testName}
              </h1>
              {summary.latest.description && (
                <p className="mt-1 text-sm text-gray-600 dark:text-slate-300">
                  {summary.latest.description}
                </p>
              )}
            </div>
            <span className={`badge ${
              activity === 'fresh' ? 'badge-success'
              : activity === 'recent' ? 'badge-info'
              : activity === 'stale' ? 'badge-warning'
              : 'badge-danger'
            }`}>
              {activity}
            </span>
          </div>
          <dl className="grid grid-cols-2 gap-y-2 gap-x-6 text-sm sm:grid-cols-4">
            <StatCell label="Total runs" value={summary.total.toLocaleString()} />
            <StatCell label="Avg p95" value={formatLatency(summary.avgP95)} />
            <StatCell label="Avg success" value={formatPercentage(summary.avgSuccess)} />
            <StatCell label="Latest" value={`${formatAge(summary.latest.timestamp)}`} icon={<ClockIcon className="h-3 w-3 text-gray-400 dark:text-slate-500" />} />
          </dl>
          {/* When success_rate is low, latency rankings below are timing
              FAILED/REVERTED requests, not successful ones. Surface this
              loudly — silently ranking a 0%-success run is misleading. */}
          {summary.avgSuccess < 95 && (
            <div
              className={`mt-2 rounded-md px-3 py-2 text-sm ${
                summary.avgSuccess < 50
                  ? 'bg-red-50 border border-red-200 text-red-800 dark:bg-red-900/30 dark:border-red-700/50 dark:text-red-200'
                  : 'bg-yellow-50 border border-yellow-200 text-yellow-800 dark:bg-yellow-900/30 dark:border-yellow-700/50 dark:text-yellow-200'
              }`}
              role="alert"
            >
              <strong>Low success rate ({formatPercentage(summary.avgSuccess)}).</strong>
              {' '}
              The latency numbers and rankings below include timing of
              failed/reverted JSON-RPC calls. Inspect the per-method success
              column in <em>Recent runs</em> below to see which calls are failing
              before drawing conclusions from the leaderboard.
            </div>
          )}
        </div>
      </header>

      <div className="mb-6">
        <CurrentBuildsPanel runs={runs} />
      </div>

      <section className="card mb-6">
        <div className="card-header flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-slate-100">
              Latency over time, one line per client
            </h2>
            <p className="text-xs text-gray-500 dark:text-slate-400">
              Last {recent.length} run{recent.length === 1 ? '' : 's'}. Switch to a method to see how each client performs on a specific RPC call.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2 text-xs text-gray-500 dark:text-slate-400">
              Method
              <select
                value={trendMethod}
                onChange={e => setTrendMethod(e.target.value)}
                className="input py-1 text-xs"
              >
                <option value="">All methods (aggregate)</option>
                {allMethods.map(m => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </label>
            <div className="flex items-center gap-1" role="tablist" aria-label="Trend metric">
              {TREND_METRICS.map(m => (
                <button
                  key={m.key}
                  type="button"
                  role="tab"
                  aria-selected={trendMetric === m.key}
                  onClick={() => setTrendMetric(m.key)}
                  className={`whitespace-nowrap rounded px-2 py-1 text-xs font-medium transition-colors ${
                    trendMetric === m.key
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
          <PerClientTrendChart
            series={clientSeries}
            metricLabel={`${trendMetric} latency${trendMethod ? ` — ${trendMethod}` : ''}`}
          />
        </div>
      </section>

      <div className="mb-6">
        <MethodLeaderboard
          runs={methodQueries
            .map(q => q.data)
            .filter((m): m is NonNullable<typeof m> => !!m && !!m.methods_by_client)}
          title={`Per-method leaderboard — ${testName}`}
        />
      </div>

      <section className="card mb-6">
        <div className="card-header">
          <h2 className="text-base font-semibold text-gray-900 dark:text-slate-100">
            Recent runs
          </h2>
        </div>
        <div className="card-content p-0">
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 dark:divide-slate-700">
              <thead className="bg-gray-50 dark:bg-slate-900/60">
                <tr className="text-left text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-400">
                  <th className="px-4 py-2 font-medium">When</th>
                  <th className="px-4 py-2 font-medium">Branch</th>
                  <th className="px-4 py-2 font-medium">Commit</th>
                  <th className="px-4 py-2 font-medium text-right">Requests</th>
                  <th className="px-4 py-2 font-medium text-right">Success</th>
                  <th className="px-4 py-2 font-medium text-right">Avg latency</th>
                  <th className="px-4 py-2 font-medium text-right">p95</th>
                  <th className="px-4 py-2 font-medium" />
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-slate-800">
                {runs.map(r => (
                  <tr key={r.id} className="hover:bg-gray-50 dark:hover:bg-slate-800/60">
                    <td className="px-4 py-2 text-sm text-gray-900 dark:text-slate-200">
                      <div>{new Date(r.timestamp).toLocaleString()}</div>
                      <div className="text-xs text-gray-400 dark:text-slate-500">{formatAge(r.timestamp)}</div>
                    </td>
                    <td className="px-4 py-2 text-xs text-gray-700 dark:text-slate-300">{r.git_branch || '—'}</td>
                    <td className="px-4 py-2 font-mono text-xs text-gray-700 dark:text-slate-300">
                      {(r.git_commit || '').slice(0, 7)}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-sm text-gray-900 dark:text-slate-100">
                      {r.total_requests.toLocaleString()}
                    </td>
                    <td
                      className={`px-4 py-2 text-right font-mono text-sm ${
                        r.success_rate < 50
                          ? 'font-semibold text-red-600 dark:text-red-400'
                          : r.success_rate < 95
                            ? 'font-medium text-yellow-700 dark:text-yellow-400'
                            : 'text-gray-900 dark:text-slate-100'
                      }`}
                    >
                      {formatPercentage(r.success_rate)}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-sm text-gray-900 dark:text-slate-100">
                      {formatLatency(r.avg_latency_ms)}
                    </td>
                    <td className="px-4 py-2 text-right font-mono text-sm text-gray-900 dark:text-slate-100">
                      {formatLatency(r.p95_latency_ms)}
                    </td>
                    <td className="px-4 py-2 text-right">
                      <Link
                        to={`/runs/${r.id}`}
                        className="text-xs text-primary-600 hover:underline dark:text-primary-300"
                      >
                        Open →
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>
    </>
  )
}

function StatCell({ label, value, icon }: { label: string; value: string; icon?: React.ReactNode }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">{label}</dt>
      <dd className="mt-0.5 flex items-center gap-1 font-mono text-base text-gray-900 dark:text-slate-100">
        {icon}
        <span>{value}</span>
      </dd>
    </div>
  )
}
