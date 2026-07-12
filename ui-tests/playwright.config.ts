import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: '.', testMatch: 'acceptance.spec.ts',
  use: { baseURL: process.env.FORGE_UI_BASE_URL ?? 'http://127.0.0.1:3017', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: process.env.FORGE_UI_BASE_URL ? undefined : { command: 'go run ./cmd/forge server', cwd: '..', url: 'http://127.0.0.1:3017/livez', timeout: 30000, reuseExistingServer: false },
});
