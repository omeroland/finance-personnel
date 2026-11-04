import { defineConfig, devices } from '@playwright/test';

// Parcours complet sur la PILE RÉELLE (`make up-test`, https://localhost:9443) : ni Prism, ni ng serve,
// ni Keycloak de test séparé. Lancé par `make e2e-full` (P1-22). Certificat auto-signé accepté.
export default defineConfig({
  testDir: '.',
  testMatch: 'full-flow.spec.ts',
  timeout: 180_000,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env['E2E_BASE_URL'] ?? 'https://localhost:9443',
    ignoreHTTPSErrors: true,
    locale: 'fr-CA',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
