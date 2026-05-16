import { test, expect, APIRequestContext } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const API_URL = process.env.API_URL || 'http://localhost:8082'
const POSTGRES_CONTAINER = process.env.POSTGRES_CONTAINER || 'jsonrpc-bench-postgres'

function psql(sql: string): string {
  return execFileSync(
    'docker',
    ['exec', POSTGRES_CONTAINER, 'psql', '-U', 'postgres', '-d', 'jsonrpc_bench', '-tA', '-c', sql],
    { encoding: 'utf8' },
  ).trim()
}

// Helpers ---------------------------------------------------------------------

async function getLatestRunId(api: APIRequestContext): Promise<string> {
  const res = await api.get(`${API_URL}/api/runs?limit=1`)
  expect(res.ok(), `GET /api/runs returned ${res.status()}`).toBeTruthy()
  const body = await res.json()
  expect(body.runs?.length, 'no benchmark runs in DB — run the runner first').toBeGreaterThan(0)
  return body.runs[0].id
}

async function listBaselines(api: APIRequestContext) {
  const res = await api.get(`${API_URL}/api/baselines`)
  expect(res.ok()).toBeTruthy()
  return (await res.json()).baselines ?? []
}

async function deleteBaseline(api: APIRequestContext, name: string) {
  await api.delete(`${API_URL}/api/baselines/${encodeURIComponent(name)}`)
}

// API contract ---------------------------------------------------------------

test.describe('Baseline API contract', () => {
  test('accepts snake_case run_id', async ({ request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-snake-${Date.now()}`
    const res = await request.post(`${API_URL}/api/baselines`, {
      data: { run_id: runId, name },
      headers: { 'Content-Type': 'application/json' },
    })
    expect(res.status(), 'POST should succeed with snake_case').toBe(201)
    const body = await res.json()
    expect(body.name).toBe(name)
    expect(body.run_id).toBe(runId)
    await deleteBaseline(request, name)
  })

  test('accepts camelCase runId', async ({ request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-camel-${Date.now()}`
    const res = await request.post(`${API_URL}/api/baselines`, {
      data: { runId, name },
      headers: { 'Content-Type': 'application/json' },
    })
    expect(res.status(), 'POST should succeed with camelCase').toBe(201)
    const body = await res.json()
    expect(body.run_id, 'response always uses snake_case').toBe(runId)
    await deleteBaseline(request, name)
  })

  test('rejects missing run identifier', async ({ request }) => {
    const res = await request.post(`${API_URL}/api/baselines`, {
      data: { name: 'no-run-id' },
      headers: { 'Content-Type': 'application/json' },
    })
    expect(res.status()).toBe(400)
  })

  test('listBaselines returns Baseline objects (snake_case)', async ({ request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-shape-${Date.now()}`
    await request.post(`${API_URL}/api/baselines`, {
      data: { runId, name },
      headers: { 'Content-Type': 'application/json' },
    })
    try {
      const baselines = await listBaselines(request)
      const created = baselines.find((b: any) => b.name === name)
      expect(created, 'created baseline appears in list').toBeDefined()
      // Required Baseline fields per dashboard/src/types/api.ts
      for (const key of ['id', 'name', 'test_name', 'run_id', 'created_at', 'updated_at', 'baseline_metrics', 'is_active']) {
        expect(created, `field ${key} present`).toHaveProperty(key)
      }
      // baseline_metrics snapshot fields
      for (const key of ['overall_error_rate', 'avg_latency_ms', 'p95_latency_ms', 'total_requests', 'total_errors']) {
        expect(created.baseline_metrics, `baseline_metrics.${key} present`).toHaveProperty(key)
      }
    } finally {
      await deleteBaseline(request, name)
    }
  })

  test('delete uses baseline name in URL path', async ({ request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-delete-${Date.now()}`
    await request.post(`${API_URL}/api/baselines`, {
      data: { runId, name },
      headers: { 'Content-Type': 'application/json' },
    })
    const delRes = await request.delete(`${API_URL}/api/baselines/${encodeURIComponent(name)}`)
    expect(delRes.ok()).toBeTruthy()
    const baselines = await listBaselines(request)
    expect(baselines.find((b: any) => b.name === name), 'baseline gone from list').toBeUndefined()
  })

  test('snapshot is preserved when source run is deleted (ON DELETE SET NULL)', async ({ request }) => {
    // The FK lives on baselines.run_id → benchmark_runs(id). To exercise the
    // ON DELETE SET NULL path we need to delete a row from benchmark_runs.
    // The HTTP DELETE /api/runs handler delegates to a not-implemented storage
    // method so we drop straight to psql for this one. We seed a throwaway run
    // (populating columns the Go scanner expects to be non-NULL) so we don't
    // disturb the latest real run.
    const sourceRun = await getLatestRunId(request)
    const throwawayRunId = `e2e-fk-run-${Date.now()}`
    psql(
      `INSERT INTO benchmark_runs (id, timestamp, git_commit, git_branch, test_name, description, ` +
        `config_hash, result_path, duration, total_requests, success_rate, avg_latency, p95_latency, ` +
        `clients, methods, tags, is_baseline, baseline_name) ` +
        `SELECT '${throwawayRunId}', NOW(), git_commit, git_branch, test_name, description, ` +
        `config_hash, result_path, duration, total_requests, success_rate, avg_latency, p95_latency, ` +
        `clients, methods, tags, false, '' FROM benchmark_runs WHERE id = '${sourceRun}'`,
    )

    const name = `e2e-fk-${Date.now()}`
    try {
      const create = await request.post(`${API_URL}/api/baselines`, {
        data: { runId: throwawayRunId, name },
        headers: { 'Content-Type': 'application/json' },
      })
      expect(create.ok(), `baseline create failed: ${create.status()} ${await create.text()}`).toBeTruthy()
      const baseline = await create.json()
      const baselineId = baseline.id
      expect(baseline.run_id).toBe(throwawayRunId)

      // Drop the source run; the FK should null out run_id, not cascade-delete.
      psql(`DELETE FROM benchmark_runs WHERE id = '${throwawayRunId}'`)

      const orphanedRunId = psql(
        `SELECT COALESCE(run_id, '<NULL>') FROM baselines WHERE id = '${baselineId}'`,
      )
      expect(orphanedRunId, 'baseline row survives with NULL run_id').toBe('<NULL>')

      // Snapshot in baseline_metrics is preserved (JSONB column, not FK-tied).
      const stillThere = (await listBaselines(request)).find((b: any) => b.id === baselineId)
      expect(stillThere, 'baseline still listed via API').toBeDefined()
      expect(stillThere.baseline_metrics).toBeDefined()
    } finally {
      psql(`DELETE FROM baselines WHERE name = '${name}'`)
      psql(`DELETE FROM benchmark_runs WHERE id = '${throwawayRunId}'`)
    }
  })
})

// UI flow --------------------------------------------------------------------

test.describe('Baseline UI flow on RunDetails page', () => {
  test('BaselineManager renders the list with snake_case fields', async ({ page, request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-ui-render-${Date.now()}`
    await request.post(`${API_URL}/api/baselines`, {
      data: { runId, name },
      headers: { 'Content-Type': 'application/json' },
    })
    try {
      const errors: string[] = []
      page.on('pageerror', err => errors.push(`pageerror: ${err.message}`))
      page.on('console', msg => {
        if (msg.type() === 'error') errors.push(`[console.error] ${msg.text()}`)
      })

      await page.goto(`/runs/${runId}`, { waitUntil: 'networkidle' })

      // BaselineManager table row should show the baseline name — scope to
      // the row to avoid the comparison <select> matching the same text.
      const row = page.locator('tr', { hasText: name }).first()
      await expect(row).toBeVisible({ timeout: 10_000 })

      // No JS errors during render — guards against the ASI hazard regression.
      expect(errors, `unexpected JS errors:\n${errors.join('\n')}`).toEqual([])
    } finally {
      await deleteBaseline(request, name)
    }
  })

  test('Per-client percentile chart renders and clicking a bar sets the client filter', async ({ page, request }) => {
    const runId = await getLatestRunId(request)
    await page.goto(`/runs/${runId}`, { waitUntil: 'networkidle' })

    const card = page.locator('.card', { has: page.getByText('Latency Percentiles by Client') }).first()
    await card.scrollIntoViewIfNeeded()

    // Description text confirms what the chart is showing — guards against a
    // regression to the synthetic-bucket histogram we replaced.
    await expect(card.getByText(/Real per-client p50 \/ p95 \/ p99 \/ max/)).toBeVisible()

    const canvas = card.locator('canvas').first()
    const box = await canvas.boundingBox()
    expect(box, 'chart canvas rendered').toBeTruthy()
    expect(box!.width, 'canvas has non-zero width').toBeGreaterThan(100)
    expect(box!.height, 'canvas has non-zero height').toBeGreaterThan(100)

    // Clicking a bar inside the canvas should set the client filter banner.
    // chart.js dispatches the onClick only when an element is actually under the
    // pointer, so we scan a small grid until we land on one.
    let filterShown = false
    for (const fx of [0.18, 0.30, 0.50, 0.70, 0.85]) {
      for (const fy of [0.30, 0.50, 0.70]) {
        await page.mouse.click(box!.x + box!.width * fx, box!.y + box!.height * fy)
        if (await page.locator('text=/Filtering subsequent tables to client/').isVisible().catch(() => false)) {
          filterShown = true
          break
        }
      }
      if (filterShown) break
    }
    expect(filterShown, 'clicking a bar somewhere in the canvas sets the client filter').toBe(true)
  })

  test('Overview groups runs by test_name with leaderboard + per-test cards', async ({ page, request }) => {
    // Need at least one run for the dashboard to have anything to render.
    await getLatestRunId(request)

    await page.goto('/', { waitUntil: 'networkidle' })

    // Hero stat strip — confirm summary tiles render.
    await expect(page.getByText(/Active tests/i)).toBeVisible()
    await expect(page.getByText(/Total runs/i)).toBeVisible()

    // Leaderboard panel.
    await expect(page.getByRole('heading', { name: /Client leaderboard/i })).toBeVisible()

    // Switch leaderboard metric — should keep the panel populated.
    await page.getByRole('tab', { name: /^p99 latency$/i }).click()
    await expect(page.getByRole('tab', { name: /^p99 latency$/i })).toHaveAttribute(
      'aria-selected',
      'true',
    )

    // At least one test group card and its Latest link.
    const cards = page.locator('article.card')
    await expect(cards.first()).toBeVisible()
    const firstLatest = cards.first().getByRole('link', { name: /Latest/i }).first()
    await firstLatest.click()
    await page.waitForURL(/\/runs\/[^/]+$/, { timeout: 5_000 })
    expect(page.url()).toMatch(/\/runs\/[\w-]+$/)
  })

  test('Test card title links to /tests/:name and renders history + per-client trend', async ({ page, request }) => {
    const latestId = await getLatestRunId(request)
    const runResp = await request.get(`${API_URL}/api/runs/${latestId}`).then(r => r.json())
    const testName: string = runResp.run.test_name
    expect(testName).toBeTruthy()

    await page.goto('/', { waitUntil: 'networkidle' })

    // Find the card whose heading matches the test name and click the link.
    const card = page.locator('article.card', { hasText: testName }).first()
    await card.getByRole('link', { name: testName }).first().click()
    await page.waitForURL(/\/tests\/[^/]+$/, { timeout: 5_000 })

    // The page <h1> contains the test name. There may be other elements that
    // mention it (table rows, breadcrumbs) so we scope to h1 explicitly.
    await expect(page.locator('h1', { hasText: testName })).toBeVisible()

    // Stats row tiles present.
    await expect(page.getByText(/^Total runs$/i)).toBeVisible()
    await expect(page.getByText(/^Avg p95$/i)).toBeVisible()

    // Per-client trend chart canvas renders.
    const trendCard = page.locator('.card', { has: page.getByText(/Latency over time/i) }).first()
    await expect(trendCard.locator('canvas').first()).toBeVisible()

    // Method picker should have at least one specific method besides the
    // aggregate "All methods" entry. Picking it should keep the chart up.
    const methodOptions = await trendCard.locator('select').first().locator('option').allTextContents()
    expect(methodOptions.length, 'trend method picker populated').toBeGreaterThan(1)
    const concreteMethod = methodOptions.find(o => !o.startsWith('All methods'))
    if (concreteMethod) {
      await trendCard.locator('select').first().selectOption({ label: concreteMethod })
      // Switch metric to p99; tab should be active and chart still rendered.
      await trendCard.getByRole('tab', { name: /^p99$/ }).click()
      await expect(trendCard.getByRole('tab', { name: /^p99$/ })).toHaveAttribute('aria-selected', 'true')
      await expect(trendCard.locator('canvas').first()).toBeVisible()
    }

    // Per-method leaderboard has at least one client row.
    const methodCard = page.locator('.card', { has: page.getByText(/Per-method leaderboard/i) }).first()
    await expect(methodCard.locator('select').first()).toBeVisible()

    // Recent runs table has at least one Open link back to a run.
    await expect(page.getByRole('heading', { name: /Recent runs/i })).toBeVisible()
    const openLink = page.getByRole('link', { name: /^Open →$/ }).first()
    await openLink.click()
    await page.waitForURL(/\/runs\/[^/]+$/, { timeout: 5_000 })
  })

  test('Client chip filter highlights the leaderboard row without collapsing the ranking', async ({ page, request }) => {
    await getLatestRunId(request)
    await page.goto('/', { waitUntil: 'networkidle' })

    // Read the unfiltered leaderboard's full client set so we can verify
    // none disappear when a filter is applied.
    const leaderboard = page.locator('.card', { has: page.getByRole('heading', { name: /Client leaderboard/i }) }).first()
    await expect(leaderboard).toBeVisible()
    const initialRows = await leaderboard.locator('li').count()
    expect(initialRows, 'leaderboard has at least 2 clients to rank').toBeGreaterThan(1)

    // Click the geth chip (any client that exists in the data set).
    const gethChip = page.getByRole('button', { name: /^geth$/ }).first()
    if (await gethChip.isVisible().catch(() => false)) {
      await gethChip.click()
      await page.waitForTimeout(200)

      // Same number of rows — no collapse.
      const afterRows = await leaderboard.locator('li').count()
      expect(afterRows, 'all clients still ranked after filter').toBe(initialRows)

      // Exactly one row is marked as the currently-filtered client.
      const focusedRows = leaderboard.locator('li[aria-current="true"]')
      await expect(focusedRows).toHaveCount(1)
      await expect(focusedRows.first()).toContainText('geth')
    }
  })

  test('client_versions captured at run start appear in the Per-Client Metrics table', async ({ page, request }) => {
    const runId = await getLatestRunId(request)
    const runResp = await request.get(`${API_URL}/api/runs/${runId}`).then(r => r.json())
    const versions: Record<string, string> | undefined = runResp.run?.client_versions
    // Skip silently if this run pre-dates the feature (no versions captured)
    test.skip(!versions || Object.keys(versions).length === 0, 'run has no client_versions data')

    await page.goto(`/runs/${runId}`, { waitUntil: 'networkidle' })

    const header = page.getByRole('button', { name: /(Expand|Collapse) Client Performance Analysis/ })
    const label = await header.getAttribute('aria-label')
    if (label?.startsWith('Expand')) {
      await header.click()
    }
    const section = page.locator(
      'xpath=//*[normalize-space()="Client Performance Analysis"]/ancestor::div[contains(@class,"card")][1]',
    )
    await expect.poll(
      async () => section.locator('tr.table-row').count(),
      { timeout: 10_000 },
    ).toBeGreaterThan(0)

    // For each client name shown in the table, the row should also surface
    // its build string (or "version unknown" if the call failed).
    for (const [client, version] of Object.entries(versions!)) {
      const row = section.locator('tr.table-row', { hasText: client }).first()
      if (await row.count() === 0) continue // client absent from this run's metrics — skip
      if (version === 'unknown') {
        await expect(row).toContainText('version unknown')
      } else {
        // Display may truncate — match on a stable prefix (the build name).
        const prefix = version.split(/[/-]/)[0]
        await expect(row).toContainText(prefix)
      }
    }
  })

  test('Client Performance Analysis dropdown actually filters the table', async ({ page, request }) => {
    const runId = await getLatestRunId(request)
    await page.goto(`/runs/${runId}`, { waitUntil: 'networkidle' })

    // Expand the Client Performance Analysis section if collapsed.
    const header = page.getByRole('button', { name: /(Expand|Collapse) Client Performance Analysis/ })
    const label = await header.getAttribute('aria-label')
    if (label?.startsWith('Expand')) {
      await header.click()
    }
    // Scope to the Client Performance Analysis section card. The "Per-Client
    // Metrics" sub-heading is text-only — text=… as a Playwright locator
    // resolves to a span here, not the card wrapper.
    const section = page.locator(
      'xpath=//*[normalize-space()="Client Performance Analysis"]/ancestor::div[contains(@class,"card")][1]',
    )
    await expect(section).toBeVisible()
    await expect.poll(
      async () => section.locator('tr.table-row').count(),
      { timeout: 10_000, message: 'detailed metrics did not load multiple client rows' },
    ).toBeGreaterThan(1)

    const rowCountAll = await section.locator('tr.table-row').count()
    expect(rowCountAll).toBeGreaterThan(1)

    const select = page.locator('select').filter({ hasText: 'All Clients' }).first()
    const options = await select.locator('option').allTextContents()
    const specific = options.find(o => o && o !== 'All Clients')
    expect(specific, 'has at least one concrete client option').toBeTruthy()
    await select.selectOption(specific!)
    await page.waitForTimeout(200)

    // The header should stay expanded (regression from the ExpandableSection fix).
    await expect(header).toHaveAttribute('aria-expanded', 'true')

    // The table now shows exactly one row.
    const rowCountFiltered = await section.locator('tr.table-row').count()
    expect(rowCountFiltered, 'one client row after filter').toBe(1)
    await expect(section.locator('tr.table-row').first()).toContainText(specific!)
  })

  test('Overview shows a Current client builds panel with captured versions', async ({ page, request }) => {
    // Make sure the dataset actually has a run with client_versions
    const runId = await getLatestRunId(request)
    const detail = await request.get(`${API_URL}/api/runs/${runId}`).then(r => r.json())
    const versions: Record<string, string> | undefined = detail.run?.client_versions
    test.skip(!versions || Object.keys(versions).length === 0, 'no client_versions in latest run')

    await page.goto('/', { waitUntil: 'networkidle' })

    const panel = page.locator('.card', { has: page.getByRole('heading', { name: /Current client builds/i }) }).first()
    await expect(panel).toBeVisible()

    // Every captured client should have a tile in the panel.
    for (const [client, version] of Object.entries(versions!)) {
      const tile = panel.locator('li', { hasText: client }).first()
      await expect(tile).toBeVisible()
      if (version === 'unknown') {
        await expect(tile).toContainText('version unknown')
      } else {
        const prefix = version.split(/[/-]/)[0]
        await expect(tile).toContainText(prefix)
      }
    }
  })

  test('Theme toggle switches body class and persists across reload', async ({ page }) => {
    await page.goto('/', { waitUntil: 'networkidle' })

    // Force light first so the test isn't affected by a prior run.
    await page.evaluate(() => {
      localStorage.setItem('jsonbench-theme', 'light')
      document.documentElement.classList.remove('dark')
    })
    await page.reload({ waitUntil: 'networkidle' })

    const html = page.locator('html')
    await expect(html).not.toHaveClass(/(^|\s)dark(\s|$)/)

    await page.getByRole('radio', { name: /Dark theme/i }).click()
    await expect(html).toHaveClass(/(^|\s)dark(\s|$)/)

    // Persists across reload.
    await page.reload({ waitUntil: 'networkidle' })
    await expect(html).toHaveClass(/(^|\s)dark(\s|$)/)

    // Restore light so we don't leak state into the next test.
    await page.getByRole('radio', { name: /Light theme/i }).click()
  })

  test('Compare against baseline selector loads a real comparison view', async ({ page, request }) => {
    // Need two real runs *of the same test* — the baseline picker filters to
    // baselines whose test_name matches the current run.
    const listRes = await request.get(`${API_URL}/api/runs?limit=50`)
    const runs = (await listRes.json()).runs as Array<{ id: string; test_name: string }>
    const byTest = new Map<string, typeof runs>()
    for (const r of runs) {
      const list = byTest.get(r.test_name) ?? []
      list.push(r)
      byTest.set(r.test_name, list)
    }
    const pair = Array.from(byTest.values()).find(list => list.length >= 2)
    test.skip(!pair, 'need at least 2 runs of one test to exercise compare')
    if (!pair) return

    const currentRunId = pair[0].id
    const baselineSourceRunId = pair[1].id
    const baselineName = `e2e-compare-${Date.now()}`

    const create = await request.post(`${API_URL}/api/baselines`, {
      data: { runId: baselineSourceRunId, name: baselineName },
      headers: { 'Content-Type': 'application/json' },
    })
    expect(create.ok()).toBeTruthy()

    try {
      await page.goto(`/runs/${currentRunId}`, { waitUntil: 'networkidle' })
      const card = page.locator('[data-testid="baseline-comparison-card"]')
      await card.scrollIntoViewIfNeeded()

      // Selector is present with our baseline as an option.
      const select = card.locator('#baseline-compare-select')
      await expect(select).toBeVisible()
      await expect(select.locator('option', { hasText: baselineName })).toHaveCount(1)

      await select.selectOption(baselineName)

      // Comparison renders: overall status badge + the per-client table header
      await expect(card.getByText(/vs baseline/).first()).toBeVisible({ timeout: 10_000 })
      await expect(card.getByText('Overall avg latency', { exact: false })).toBeVisible()
      await expect(card.locator('th', { hasText: 'Client' })).toBeVisible()

      // A delta cell uses the "% vs" pattern — proves the wiring isn't showing static text
      const deltaText = await card.locator('text=/% vs /').first().innerText()
      expect(deltaText).toMatch(/[+\-]?\d+\.\d+% vs/)
    } finally {
      await request.delete(`${API_URL}/api/baselines/${encodeURIComponent(baselineName)}`)
    }
  })

  test('clicking delete in the UI removes a baseline', async ({ page, request }) => {
    const runId = await getLatestRunId(request)
    const name = `e2e-ui-delete-${Date.now()}`
    await request.post(`${API_URL}/api/baselines`, {
      data: { runId, name },
      headers: { 'Content-Type': 'application/json' },
    })

    page.on('dialog', async d => { await d.accept() }) // confirm() prompt

    await page.goto(`/runs/${runId}`, { waitUntil: 'networkidle' })
    const row = page.locator('tr', { hasText: name })
    await expect(row).toBeVisible({ timeout: 10_000 })

    await row.getByRole('button', { name: /delete/i }).click()

    // Either the row vanishes, or its container empties — try both
    await expect(row).toHaveCount(0, { timeout: 10_000 })

    const remaining = await listBaselines(request)
    expect(remaining.find((b: any) => b.name === name)).toBeUndefined()
  })
})
