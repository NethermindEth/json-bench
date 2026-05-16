import { defineConfig, devices } from '@playwright/test'

/**
 * E2E tests assume the full docker stack is up:
 *   docker compose up -d --build
 * and at least one benchmark run exists. The baseline spec seeds and
 * cleans up its own data via the API, so re-running is idempotent.
 */
export default defineConfig({
  testDir: './tests/e2e',
  timeout: 60_000,
  fullyParallel: false,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: process.env.DASHBOARD_URL || 'http://localhost:8080',
    extraHTTPHeaders: { Accept: 'application/json' },
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
