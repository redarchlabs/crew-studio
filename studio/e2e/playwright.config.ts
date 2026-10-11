import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'
import { AUTH_DIR, BASE_URL, OUT_DIR } from './harness/paths.ts'

export default defineConfig({
  testDir: './tests',
  outputDir: path.join(OUT_DIR, 'test-results'),
  globalSetup: './harness/global-setup.ts',
  globalTeardown: './harness/global-teardown.ts',
  // The specs share one server and one database, and several depend on earlier state, so they run in file order
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [
    ['list'],
    ['html', { outputFolder: path.join(OUT_DIR, 'report'), open: 'never' }],
    ['junit', { outputFile: path.join(OUT_DIR, 'junit.xml') }],
  ],
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'off',
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
    locale: 'en-US',
    timezoneId: 'UTC',
  },
  projects: [
    // 1. the one-time /setup flow; leaves the admin signed in (storageState) and records the credentials
    { name: 'first-run', testMatch: /01-first-run\.spec\.ts/, use: { ...devices['Desktop Chrome'] } },
    // 2. creates the role users and their storageState files, via the API and the database
    { name: 'seed', testMatch: /seed\.setup\.ts/, dependencies: ['first-run'], use: { ...devices['Desktop Chrome'] } },
    // 3. everything else, in file order
    {
      name: 'chromium',
      testMatch: /\d\d-.*\.spec\.ts/,
      testIgnore: /01-first-run\.spec\.ts/,
      dependencies: ['seed'],
      use: { ...devices['Desktop Chrome'], storageState: path.join(AUTH_DIR, 'admin.json') },
    },
  ],
})
