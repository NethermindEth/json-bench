import type { HistoricRun } from '../types/api'

export interface TestGroup {
  testName: string
  description?: string
  runs: HistoricRun[]      // sorted newest first
  latestRun: HistoricRun
  avgP95: number
  avgSuccess: number
  totalRuns: number
  clients: string[]
}

/**
 * Group runs by test_name, sorted by newest-first within each group.
 * Output array is sorted by most-recent-activity first.
 */
export function groupRunsByTest(runs: HistoricRun[]): TestGroup[] {
  const byName = new Map<string, HistoricRun[]>()
  for (const r of runs) {
    if (!r.test_name) continue
    const list = byName.get(r.test_name) ?? []
    list.push(r)
    byName.set(r.test_name, list)
  }

  const groups: TestGroup[] = []
  for (const [testName, list] of byName) {
    list.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
    const latestRun = list[0]
    const validP95 = list.filter(r => r.p95_latency_ms > 0)
    const avgP95 = validP95.length
      ? validP95.reduce((s, r) => s + r.p95_latency_ms, 0) / validP95.length
      : 0
    const avgSuccess = list.reduce((s, r) => s + (r.success_rate ?? 0), 0) / list.length

    // Collect distinct client names seen across this group's latest run.
    const clients = new Set<string>()
    for (const r of list) {
      ;(r.clients ?? []).forEach(c => clients.add(c))
    }

    groups.push({
      testName,
      description: latestRun.description,
      runs: list,
      latestRun,
      avgP95,
      avgSuccess,
      totalRuns: list.length,
      clients: Array.from(clients).sort(),
    })
  }

  groups.sort(
    (a, b) =>
      new Date(b.latestRun.timestamp).getTime() -
      new Date(a.latestRun.timestamp).getTime(),
  )
  return groups
}

/**
 * Crude age formatter — minutes / hours / days.
 */
export function formatAge(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 0) return 'just now'
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day}d ago`
  const mon = Math.floor(day / 30)
  if (mon < 12) return `${mon}mo ago`
  return `${Math.floor(day / 365)}y ago`
}

/**
 * Heuristic "activity" classification for a test group based on time since
 * latest run. Used to badge inactive tests like hive-ui does.
 */
export function classifyActivity(iso: string): 'fresh' | 'recent' | 'stale' | 'inactive' {
  const ms = Date.now() - new Date(iso).getTime()
  const day = 86_400_000
  if (ms < day) return 'fresh'
  if (ms < 7 * day) return 'recent'
  if (ms < 30 * day) return 'stale'
  return 'inactive'
}
