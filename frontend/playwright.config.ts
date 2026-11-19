import { defineConfig, devices } from '@playwright/test'

// Runs against the Compose environment with the demo stores created.
export default defineConfig({
  testDir: './e2e',
  forbidOnly: !!process.env.CI,
  reporter: 'list',
  use: { ...devices['Desktop Chrome'], trace: 'retain-on-failure' },
})
