import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Helmet } from 'react-helmet-async'
import { MagnifyingGlassIcon, ServerIcon, AdjustmentsHorizontalIcon } from '@heroicons/react/24/outline'

import { useRuns } from '../api/hooks'
import { groupRunsByTest } from '../utils/run-aggregations'
import TestGroupCard from '../components/TestGroupCard'
import Leaderboard from '../components/Leaderboard'
import CurrentBuildsPanel from '../components/CurrentBuildsPanel'

/**
 * Overview page. Groups runs by test_name and shows:
 *   - global per-client leaderboard at the top
 *   - per-test cards with recent-run heatmap and key stats
 *   - search/filter chips for client and a freeform query
 */
export default function Dashboard() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState(searchParams.get('q') ?? '')
  const [clientFilter, setClientFilter] = useState<string | null>(
    searchParams.get('client') || null,
  )

  // Pull a generous slice of recent runs and group client-side. The runs
  // table is small enough that this is cheaper than another endpoint.
  const { data: runs, isLoading } = useRuns({ limit: 200 })

  const groups = useMemo(() => groupRunsByTest(runs ?? []), [runs])

  // Collect all clients seen across all groups for the filter chips.
  const allClients = useMemo(() => {
    const s = new Set<string>()
    groups.forEach(g => g.clients.forEach(c => s.add(c)))
    return Array.from(s).sort()
  }, [groups])

  // Apply the client filter to the displayed groups (keep groups whose latest
  // run touched that client). Filtering is purely visual — the leaderboard
  // continues to honor `clientFilter` separately.
  const visibleGroups = useMemo(() => {
    const trimmed = query.trim().toLowerCase()
    return groups.filter(g => {
      if (clientFilter && !g.clients.includes(clientFilter)) return false
      if (!trimmed) return true
      return (
        g.testName.toLowerCase().includes(trimmed) ||
        (g.description ?? '').toLowerCase().includes(trimmed)
      )
    })
  }, [groups, query, clientFilter])

  const updateParams = (next: Record<string, string | null>) => {
    const p = new URLSearchParams(searchParams)
    for (const [k, v] of Object.entries(next)) {
      if (v) p.set(k, v); else p.delete(k)
    }
    setSearchParams(p, { replace: true })
  }

  const onQueryChange = (v: string) => {
    setQuery(v)
    updateParams({ q: v || null })
  }
  const onClientFilter = (client: string | null) => {
    setClientFilter(client)
    updateParams({ client })
  }

  // Summary stats across all (unfiltered) groups
  const summary = useMemo(() => {
    const totalRuns = groups.reduce((s, g) => s + g.totalRuns, 0)
    const activeTests = groups.length
    const successOverall = groups.length
      ? groups.reduce((s, g) => s + g.avgSuccess, 0) / groups.length
      : 0
    const latest = groups[0]?.latestRun.timestamp
    return { totalRuns, activeTests, successOverall, latest }
  }, [groups])

  return (
    <>
      <Helmet>
        <title>Overview</title>
      </Helmet>

      {/* Hero strip with summary numbers */}
      <section className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <SummaryStat label="Active tests" value={summary.activeTests.toString()} />
        <SummaryStat label="Total runs" value={summary.totalRuns.toLocaleString()} />
        <SummaryStat
          label="Avg success"
          value={summary.activeTests ? `${summary.successOverall.toFixed(1)}%` : 'n/a'}
        />
        <SummaryStat
          label="Latest run"
          value={summary.latest ? new Date(summary.latest).toLocaleString() : 'n/a'}
        />
      </section>

      {/* Leaderboard */}
      <div className="mb-6">
        <Leaderboard groups={groups} clientFilter={clientFilter} />
      </div>

      {/* Current client builds — pulled from the latest run that recorded
          web3_clientVersion for each client. */}
      <div className="mb-6">
        <CurrentBuildsPanel runs={runs ?? []} clientFilter={clientFilter} />
      </div>

      {/* Filter row */}
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[220px]">
          <MagnifyingGlassIcon className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400 dark:text-slate-500" />
          <input
            type="text"
            placeholder="Filter tests…"
            value={query}
            onChange={e => onQueryChange(e.target.value)}
            className="input pl-9"
          />
        </div>

        {allClients.length > 0 && (
          <div className="flex flex-wrap items-center gap-1">
            <span className="flex items-center gap-1 text-xs text-gray-500 dark:text-slate-400 mr-1">
              <AdjustmentsHorizontalIcon className="h-3.5 w-3.5" /> Client:
            </span>
            <ClientChip
              label="All"
              active={!clientFilter}
              onClick={() => onClientFilter(null)}
            />
            {allClients.map(c => (
              <ClientChip
                key={c}
                label={c}
                active={clientFilter === c}
                onClick={() => onClientFilter(clientFilter === c ? null : c)}
              />
            ))}
          </div>
        )}
      </div>

      {/* Test grid */}
      {isLoading && groups.length === 0 ? (
        <div className="card-content text-sm text-gray-500 dark:text-slate-400">
          Loading runs…
        </div>
      ) : visibleGroups.length === 0 ? (
        <div className="card text-center">
          <div className="card-content py-10">
            <p className="text-sm text-gray-700 dark:text-slate-300">No tests match your filter.</p>
            {(query || clientFilter) && (
              <button
                type="button"
                className="mt-3 text-xs text-primary-600 hover:underline dark:text-primary-300"
                onClick={() => {
                  setQuery('')
                  onClientFilter(null)
                  setSearchParams(new URLSearchParams(), { replace: true })
                }}
              >
                Clear filters
              </button>
            )}
          </div>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visibleGroups.map(g => (
            <TestGroupCard key={g.testName} group={g} />
          ))}
        </div>
      )}
    </>
  )
}

function SummaryStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="card">
      <div className="card-content py-3">
        <div className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">
          {label}
        </div>
        <div className="mt-1 text-lg font-semibold text-gray-900 dark:text-slate-100">
          {value}
        </div>
      </div>
    </div>
  )
}

function ClientChip({
  label,
  active,
  onClick,
}: {
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors ${
        active
          ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300 dark:border-primary-400'
          : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800'
      }`}
    >
      <ServerIcon className="h-3 w-3" />
      {label}
    </button>
  )
}
