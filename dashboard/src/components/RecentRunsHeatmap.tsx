import { Link } from 'react-router-dom'
import type { HistoricRun } from '../types/api'
import { formatAge } from '../utils/run-aggregations'

interface Props {
  runs: HistoricRun[]    // expected newest-first
  max?: number
}

/**
 * Color cells by success_rate. The "dead" / placeholder cells are slate to
 * give a clear visual cue when a test has fewer runs than max.
 */
function cellColor(success: number): string {
  if (success >= 99.5) return 'bg-success-500 dark:bg-success-500'
  if (success >= 95) return 'bg-success-400 dark:bg-success-500/80'
  if (success >= 80) return 'bg-warning-400 dark:bg-warning-400/80'
  if (success > 0) return 'bg-danger-500 dark:bg-danger-500'
  return 'bg-gray-200 dark:bg-slate-800'
}

export default function RecentRunsHeatmap({ runs, max = 14 }: Props) {
  const window = runs.slice(0, max)
  // Pad with nulls at the *left* (older) side so the newest sits on the right
  const padded: (HistoricRun | null)[] = [
    ...Array.from({ length: max - window.length }, () => null),
    ...[...window].reverse(),
  ]

  return (
    <div className="flex items-center gap-1" aria-label="Recent runs heatmap">
      {padded.map((r, i) => {
        if (!r) {
          return (
            <span
              key={`pad-${i}`}
              className="h-3 w-3 rounded-sm bg-gray-100 dark:bg-slate-800"
              aria-hidden
            />
          )
        }
        const cls = cellColor(r.success_rate ?? 0)
        const title = `${r.test_name} • ${(r.success_rate ?? 0).toFixed(1)}% success • ${(r.p95_latency_ms ?? 0).toFixed(1)}ms p95 • ${formatAge(r.timestamp)}`
        return (
          <Link
            key={r.id}
            to={`/runs/${r.id}`}
            title={title}
            className={`h-3 w-3 rounded-sm transition-transform hover:scale-125 ${cls}`}
          />
        )
      })}
    </div>
  )
}
