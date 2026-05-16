import { Link } from 'react-router-dom'
import { ArrowRightIcon, BeakerIcon, ClockIcon, ServerIcon } from '@heroicons/react/24/outline'
import type { TestGroup } from '../utils/run-aggregations'
import { classifyActivity, formatAge } from '../utils/run-aggregations'
import RecentRunsHeatmap from './RecentRunsHeatmap'
import { formatLatency, formatPercentage } from '../utils/metric-formatters'

interface Props {
  group: TestGroup
}

const ACTIVITY_BADGE: Record<ReturnType<typeof classifyActivity>, { label: string; cls: string }> = {
  fresh: { label: 'Active', cls: 'bg-success-100 text-success-800 dark:bg-success-900/40 dark:text-success-300' },
  recent: { label: 'Recent', cls: 'bg-primary-100 text-primary-800 dark:bg-primary-900/40 dark:text-primary-300' },
  stale: { label: 'Stale', cls: 'bg-warning-100 text-warning-800 dark:bg-warning-900/40 dark:text-warning-300' },
  inactive: { label: 'Inactive', cls: 'bg-gray-200 text-gray-700 dark:bg-slate-800 dark:text-slate-400' },
}

export default function TestGroupCard({ group }: Props) {
  const activity = classifyActivity(group.latestRun.timestamp)
  const badge = ACTIVITY_BADGE[activity]
  const latest = group.latestRun

  return (
    <article className="card flex flex-col">
      <div className="card-header flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-base font-semibold text-gray-900 dark:text-slate-100">
            <BeakerIcon className="h-4 w-4 text-primary-500 flex-shrink-0" />
            <Link
              to={`/tests/${encodeURIComponent(group.testName)}`}
              className="truncate hover:underline"
              title="Open test history"
            >
              {group.testName}
            </Link>
            <span className={`badge ${badge.cls}`}>{badge.label}</span>
          </h3>
          {group.description && (
            <p className="mt-1 line-clamp-2 text-xs text-gray-500 dark:text-slate-400">
              {group.description}
            </p>
          )}
        </div>
        <Link
          to={`/runs/${latest.id}`}
          className="flex items-center gap-1 text-xs text-primary-600 hover:underline dark:text-primary-300"
          aria-label="Open latest run"
        >
          Latest <ArrowRightIcon className="h-3 w-3" />
        </Link>
      </div>

      <div className="card-content flex-1 space-y-4">
        <div>
          <div className="mb-1 flex items-center justify-between text-xs">
            <span className="text-gray-500 dark:text-slate-400">Recent runs (newest right)</span>
            <span className="text-gray-400 dark:text-slate-500">{group.totalRuns} total</span>
          </div>
          <RecentRunsHeatmap runs={group.runs} />
        </div>

        <dl className="grid grid-cols-2 gap-y-2 gap-x-4 text-sm">
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">Avg p95</dt>
            <dd className="font-mono text-gray-900 dark:text-slate-100">{formatLatency(group.avgP95)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">Avg success</dt>
            <dd className="font-mono text-gray-900 dark:text-slate-100">{formatPercentage(group.avgSuccess)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">Latest</dt>
            <dd className="flex items-center gap-1 text-gray-900 dark:text-slate-100">
              <ClockIcon className="h-3 w-3 text-gray-400 dark:text-slate-500" />
              <span>{formatAge(latest.timestamp)}</span>
            </dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-gray-500 dark:text-slate-500">Clients</dt>
            <dd className="flex flex-wrap items-center gap-1">
              {group.clients.length === 0 && (
                <span className="text-xs text-gray-400 dark:text-slate-500">n/a</span>
              )}
              {group.clients.slice(0, 5).map(c => {
                const version = latest.client_versions?.[c]
                const tooltip = version
                  ? (version === 'unknown' ? `${c}: version unknown` : `${c}: ${version}`)
                  : c
                return (
                  <span
                    key={c}
                    title={tooltip}
                    className="inline-flex items-center gap-1 rounded bg-gray-100 px-1.5 py-0.5 text-[11px] font-mono text-gray-700 dark:bg-slate-800 dark:text-slate-300"
                  >
                    <ServerIcon className="h-2.5 w-2.5" />
                    {c}
                  </span>
                )
              })}
              {group.clients.length > 5 && (
                <span className="text-[11px] text-gray-400 dark:text-slate-500">
                  +{group.clients.length - 5}
                </span>
              )}
            </dd>
          </div>
        </dl>
      </div>
    </article>
  )
}
