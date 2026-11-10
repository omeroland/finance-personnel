import { expect, type Page } from '@playwright/test';

/** Identifiant du premier compte de l'exemple du contrat (Prism). */
export const COMPTE_ID = '3f2a9c1e-7b4d-4e68-a1c5-2d9e8f7b6a40';

/** Le client appelle l'origine du web (apiUrl vide) : on relaie les appels /api/ vers Prism. */
async function relaisPrism(page: Page): Promise<void> {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const reponse = await route.fetch({ url: `http://127.0.0.1:4010${url.pathname}${url.search}` });
    await route.fulfill({ response: reponse });
  });
}

/** Connexion Keycloak (compte de test factice), puis ouverture de l'entrée de menu demandée. */
export async function connecter(page: Page, menu: 'menu-comptes' | 'menu-fnb'): Promise<void> {
  await relaisPrism(page);
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill('test-user');
  await page.locator('#password').fill('FAUX-mot-de-passe-de-test');
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
  await page.getByTestId(menu).click();
}

/** Ouvre la page du premier compte de l'exemple. */
export async function ouvrirCompte(page: Page): Promise<void> {
  await connecter(page, 'menu-comptes');
  await page.getByTestId(`compte-${COMPTE_ID}`).click();
  await expect(page.getByTestId('compte-titre')).toHaveText('CELI Questrade');
}
