import { expect, test } from '@playwright/test';

// Utilisateur du realm de test (deploy/keycloak/realm-finance-test.json) : mot de passe factice.
const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test';

test('connexion, coquille, aucun jeton dans localStorage, déconnexion', async ({ page }) => {
  await page.goto('/');

  // Redirection vers Keycloak : code d'autorisation avec PKCE S256.
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  const demande = new URL(page.url());
  expect(demande.searchParams.get('response_type')).toBe('code');
  expect(demande.searchParams.get('code_challenge_method')).toBe('S256');
  expect(demande.searchParams.get('code_challenge')).toBeTruthy();

  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();

  // Arrivée sur la coquille.
  await expect(page).toHaveURL(/\/stocks$/);
  await expect(page.getByTestId('menu-stockeval')).toContainText("Évaluation d'actions");
  await expect(page.getByTestId('menu-etf')).toContainText('Portefeuille de FNB');
  // Module 2 actif depuis P2-13 (D450) : l'entrée mène à /portefeuille, sans mention « phase 2 ».
  await expect(page.getByTestId('menu-etf')).not.toContainText('phase 2');
  await expect(page.getByTestId('menu-etf')).not.toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByTestId('menu-etf')).toHaveAttribute('href', '/portefeuille');
  await expect(page.getByTestId('utilisateur')).toContainText('Connecté en tant que');
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr-CA');

  // Aucun jeton dans localStorage.
  const local = await page.evaluate(() => JSON.stringify(Object.entries(localStorage)));
  expect(local).toBe('[]');

  // Déconnexion : retour à la page de connexion de Keycloak.
  await page.getByTestId('deconnexion').click();
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/(auth|logout)/);
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await expect(page.locator('#username')).toBeVisible();
});
