import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.ts',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  retries: process.env['CI'] ? 1 : 0,
  reporter: process.env['CI'] ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: 'http://localhost:4200',
    locale: 'fr-CA',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      // Serveur simulé à partir du contrat (outils épinglés dans api/package.json).
      // Prérequis : `npm ci` dans api/ et web/ (voir e2e/web/README.md).
      command: 'npx prism mock --host 127.0.0.1 --port 4010 openapi.yaml',
      cwd: '../../api',
      url: 'http://127.0.0.1:4010/api/v1/stocks',
      timeout: 120_000,
      reuseExistingServer: !process.env['CI'],
    },
    {
      // Client web ; config.json par défaut (public/config.json) vise Keycloak sur 8180.
      command: 'npx ng serve --port 4200 --host localhost',
      cwd: '../../web',
      url: 'http://localhost:4200',
      timeout: 240_000,
      reuseExistingServer: !process.env['CI'],
    },
  ],
});
