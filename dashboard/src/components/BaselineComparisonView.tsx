import type { BaselineComparison, ComparisonMetric } from '../types/api'

interface Props {
  comparison: BaselineComparison
}

const statusStyles: Record<BaselineComparison['status'], { label: string; cls: string }> = {
  improved: { label: 'Improved', cls: 'bg-success-50 text-success-700 border-success-200' },
  degraded: { label: 'Degraded', cls: 'bg-danger-50 text-danger-700 border-danger-200' },
  stable: { label: 'Stable', cls: 'bg-gray-50 dark:bg-slate-900/60 text-gray-700 dark:text-slate-300 border-gray-200 dark:border-slate-700' },
  mixed: { label: 'Mixed', cls: 'bg-warning-50 text-warning-700 border-warning-200' },
}

const riskStyles: Record<BaselineComparison['risk_level'], { label: string; cls: string }> = {
  low: { label: 'Low risk', cls: 'bg-success-100 text-success-800' },
  medium: { label: 'Medium risk', cls: 'bg-warning-100 text-warning-800' },
  high: { label: 'High risk', cls: 'bg-danger-100 text-danger-800' },
  critical: { label: 'Critical', cls: 'bg-danger-200 text-danger-900' },
}

function fmtMs(v: number): string {
  if (!Number.isFinite(v)) return 'N/A'
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(2)}s`
  return `${v.toFixed(2)}ms`
}

function fmtRate(v: number): string {
  if (!Number.isFinite(v)) return 'N/A'
  return `${v.toFixed(2)}%`
}

function fmtThroughput(v: number): string {
  if (!Number.isFinite(v)) return 'N/A'
  return `${v.toFixed(1)} req/s`
}

function DeltaCell({
  metric,
  format,
}: {
  metric: ComparisonMetric
  format: (v: number) => string
}) {
  // The backend sets is_improvement with the right per-metric polarity:
  // lower-is-better for latency/error_rate, higher-is-better for throughput.
  // We just consume it and color accordingly.
  const sign = metric.percent_change > 0 ? '+' : ''
  const colorCls =
    Math.abs(metric.percent_change) < 0.05
      ? 'text-gray-500 dark:text-slate-400'
      : metric.is_improvement
      ? 'text-success-700'
      : 'text-danger-700'
  return (
    <div className="text-right">
      <div className="font-mono">{format(metric.current_value)}</div>
      <div className={`text-xs ${colorCls}`}>
        {sign}{metric.percent_change.toFixed(1)}% vs {format(metric.baseline_value)}
      </div>
    </div>
  )
}

export default function BaselineComparisonView({ comparison }: Props) {
  const status = statusStyles[comparison.status] ?? statusStyles.stable
  const risk = riskStyles[comparison.risk_level] ?? riskStyles.low
  const clientEntries = Object.entries(comparison.client_changes || {}).sort(
    ([a], [b]) => a.localeCompare(b),
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className={`inline-flex items-center rounded border px-2 py-1 text-sm font-medium ${status.cls}`}>
          {status.label}
        </span>
        <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${risk.cls}`}>
          {risk.label}
        </span>
        <span className="text-xs text-gray-500 dark:text-slate-400">
          vs baseline <span className="font-mono text-gray-700 dark:text-slate-300">{comparison.baseline_name}</span>
        </span>
      </div>

      {comparison.summary && (
        <p className="text-sm text-gray-700 dark:text-slate-300">{comparison.summary}</p>
      )}

      <div className="rounded border border-gray-200 dark:border-slate-700 bg-white">
        <div className="border-b border-gray-200 dark:border-slate-700 px-3 py-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-slate-400">
          Overall avg latency
        </div>
        <div className="flex items-center justify-between px-3 py-3">
          <span className="text-sm text-gray-700 dark:text-slate-300">Current run vs baseline snapshot</span>
          <DeltaCell metric={comparison.overall_change} format={fmtMs} />
        </div>
      </div>

      {clientEntries.length > 0 && (
        <div className="overflow-hidden rounded border border-gray-200 dark:border-slate-700">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 dark:bg-slate-900/60 text-xs uppercase tracking-wide text-gray-500 dark:text-slate-400">
              <tr>
                <th className="px-3 py-2 text-left font-medium">Client</th>
                <th className="px-3 py-2 text-left font-medium">Status</th>
                <th className="px-3 py-2 text-right font-medium">Error rate</th>
                <th className="px-3 py-2 text-right font-medium">Avg latency</th>
                <th className="px-3 py-2 text-right font-medium">p95</th>
                <th className="px-3 py-2 text-right font-medium">p99</th>
                <th className="px-3 py-2 text-right font-medium">Throughput</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-slate-800">
              {clientEntries.map(([clientName, cc]) => {
                const cStatus = statusStyles[cc.status as BaselineComparison['status']] ?? statusStyles.stable
                return (
                  <tr key={clientName}>
                    <td className="px-3 py-2 font-mono text-gray-900 dark:text-slate-100">{clientName}</td>
                    <td className="px-3 py-2">
                      <span className={`inline-block rounded px-2 py-0.5 text-xs ${cStatus.cls}`}>{cStatus.label}</span>
                    </td>
                    <td className="px-3 py-2"><DeltaCell metric={cc.error_rate} format={fmtRate} /></td>
                    <td className="px-3 py-2"><DeltaCell metric={cc.avg_latency} format={fmtMs} /></td>
                    <td className="px-3 py-2"><DeltaCell metric={cc.p95_latency} format={fmtMs} /></td>
                    <td className="px-3 py-2"><DeltaCell metric={cc.p99_latency} format={fmtMs} /></td>
                    <td className="px-3 py-2"><DeltaCell metric={cc.throughput} format={fmtThroughput} /></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {comparison.recommendations && comparison.recommendations.length > 0 && (
        <div className="rounded border border-gray-200 dark:border-slate-700 bg-gray-50 dark:bg-slate-900/60 p-3">
          <div className="mb-1 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-slate-400">
            Recommendations
          </div>
          <ul className="list-disc space-y-1 pl-5 text-sm text-gray-700 dark:text-slate-300">
            {comparison.recommendations.map((r, i) => (
              <li key={i}>{r}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
