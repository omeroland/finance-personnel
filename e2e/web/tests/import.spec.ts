import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '@playwright/test';

const ici = dirname(fileURLToPath(import.meta.url));
// Classeur de test du dépôt.
const CLASSEUR = join(ici, '../../../testdata/xlsx/simply_investing_template.xlsx');

/** Le client appelle l'origine du web (apiUrl vide) : on relaie les appels /api/ vers Prism. */
async function relaisPrism(page: Page): Promise<void> {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const reponse = await route.fetch({ url: `http://127.0.0.1:4010${url.pathname}${url.search}` });
    await route.fulfill({ response: reponse });
  });
}

async function connecter(page: Page): Promise<void> {
  await relaisPrism(page);
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill('test-user');
  await page.locator('#password').fill('FAUX-mot-de-passe-de-test');
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
  await page.getByTestId('menu-import').click();
  await expect(page.getByRole('heading', { name: 'Importer la feuille' })).toBeVisible();
}

test('import du classeur de test : compte rendu et avertissements affichés', async ({ page }) => {
  await connecter(page);
  await expect(page.getByTestId('annee')).toHaveValue(String(new Date().getFullYear() - 1));
  await page.getByTestId('annee').fill('2006');
  await page.getByTestId('fichier').setInputFiles(CLASSEUR);
  await page.getByTestId('envoyer').click();

  // Prism répond avec l'exemple du contrat (job terminé, un avertissement d'unités).
  await expect(page.getByTestId('rapport')).toBeVisible();
  await expect(page.getByTestId('statut')).toContainText('Import terminé');
  await expect(page.getByTestId('importees')).toContainText('12');
  await expect(page.getByTestId('nb-warning')).toContainText('1');
  const ligne = page.getByTestId('lignes').locator('tbody tr').first();
  await expect(ligne).toContainText('Avertissement');
  await expect(ligne).toContainText('TSX:CNR');
  await expect(ligne).toContainText('Dette sur capitaux propres');
});

test('un fichier de plus de 10 Mo est refusé avant l’envoi', async ({ page }) => {
  await connecter(page);
  let envoi = false;
  page.on('request', (r) => {
    if (r.url().includes('/imports/xlsx')) envoi = true;
  });
  await page.getByTestId('fichier').setInputFiles({
    name: 'gros.xlsx',
    mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: Buffer.alloc(10 * 1024 * 1024 + 1),
  });
  await expect(page.getByTestId('erreur-fichier')).toContainText('10 Mo');
  await expect(page.getByTestId('envoyer')).toBeDisabled();
  expect(envoi).toBe(false);
});

test('suivi du job : interrogation puis compte rendu', async ({ page }) => {
  await connecter(page);
  const id = '0b9c1f0e-6d52-4c1b-9f1d-3c8f5a2e7a10';
  const base = { id, type: 'xlsx', createdAt: '2026-10-30T12:00:00Z' };
  let sondages = 0;
  await page.route('**/api/v1/imports/xlsx', (r) =>
    r.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify({ ...base, status: 'queued' }) }),
  );
  await page.route(`**/api/v1/imports/${id}`, (r) => {
    sondages++;
    const fini = sondages >= 2;
    return r.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...base,
        status: fini ? 'succeeded' : 'running',
        stocksImported: 3,
        entries: fini ? [{ level: 'error', line: 9, message: 'Valeur illisible.' }] : [],
      }),
    });
  });
  await page.getByTestId('fichier').setInputFiles(CLASSEUR);
  await page.getByTestId('envoyer').click();
  await expect(page.getByTestId('suivi')).toBeVisible();
  await expect(page.getByTestId('statut')).toContainText('Import terminé', { timeout: 15_000 });
  await expect(page.getByTestId('nb-error')).toContainText('1');
  await expect(page.getByTestId('lignes')).toContainText('Erreur');
  expect(sondages).toBe(2);
});
