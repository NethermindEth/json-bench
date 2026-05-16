import { ServerIcon } from '@heroicons/react/24/outline'
import type { HistoricRun } from '../types/api'
import { formatAge } from '../utils/run-aggregations'

interface Props {
  /** Recent runs across all tests; expected newest-first. */
  runs: HistoricRun[]
  clientFilter?: string | null
}

interface ClientBuild {
  client: string
  version: string
  seenAt: string
  fromTest: string
}

/**
 * Lists the most-recently-seen web3_clientVersion for each client across the
 * full feed of recent runs. Useful at-a-glance "what builds are we testing
 * against right now?" snapshot without drilling into a specific run.
 */
export default function CurrentBuildsPanel({ runs, clientFilter }: Props) {
  // Walk newest-first; first time we see a client, take its version + run id
  const seen = new Map<string, ClientBuild>()
  for (const r of runs) {
    const versions = r.client_versions
    if (!versions) continue
    for (const [client, version] of Object.entries(versions)) {
      if (seen.has(client)) continue
      seen.set(client, {
        client,
        version,
        seenAt: r.timestamp,
        fromTest: r.test_name,
      })
    }
  }
  const rows = Array.from(seen.values())
    .filter(b => !clientFilter || b.client === clientFilter)
    .sort((a, b) => a.client.localeCompare(b.client))

  if (rows.length === 0) return null

  return (
    <section className="card">
      <div className="card-header flex items-center justify-between">
        <h2 className="flex items-center gap-2 text-base font-semibold text-gray-900 dark:text-slate-100">
          <ServerIcon className="h-4 w-4 text-primary-500" />
          Current client builds
        </h2>
        <p className="text-xs text-gray-500 dark:text-slate-400">
          Latest <code className="font-mono">web3_clientVersion</code> seen per client across recent runs.
        </p>
      </div>
      <div className="card-content">
        <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {rows.map(b => (
            <li
              key={b.client}
              className="rounded border border-gray-200 bg-gray-50 px-3 py-2 dark:border-slate-700 dark:bg-slate-900/60"
            >
              <div className="flex items-center justify-between">
                <span className="font-mono text-sm text-gray-900 dark:text-slate-100">{b.client}</span>
                <span className="text-[10px] text-gray-400 dark:text-slate-500" title={new Date(b.seenAt).toLocaleString()}>
                  {formatAge(b.seenAt)}
                </span>
              </div>
              <div
                className={`mt-0.5 font-mono text-[11px] break-all ${
                  b.version === 'unknown'
                    ? 'italic text-gray-400 dark:text-slate-500'
                    : 'text-gray-700 dark:text-slate-300'
                }`}
                title={b.version}
              >
                {b.version === 'unknown' ? 'version unknown' : b.version}
              </div>
              <div className="mt-0.5 text-[10px] text-gray-400 dark:text-slate-500 truncate" title={b.fromTest}>
                from {b.fromTest}
              </div>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}
