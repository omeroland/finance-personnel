import { expect, test } from '@playwright/test';
import { COMPTE_ID, connecter } from './aide-etf';

const MODELE_ID = '5d7a2c64-9b1e-4f3a-8c2d-7e6f1a0b3c45';

test('tableau de bord : positions, exposition vs modèle et rendement consolidé', async ({ page }) => {
  await connecter(page, 'menu-etf');
  await expect(page).toHaveURL(/\/portefeuille$/);
  await expect(page.getByRole('heading', { name: 'Tableau de bord consolidé' })).toBeVisible();
  await expect(page.getByTestId(`tableau-compte-${COMPTE_ID}`)).toBeVisible();
  await expect(page.getByTestId('tableau-exposition').getByTestId('expo-table')).toBeVisible();
  await expect(page.getByTestId('tableau-exposition').getByTestId('expo-resume')).toBeVisible();
  const rendement = page.getByTestId('tableau-rendement');
  await expect(rendement.getByTestId('perf-twr')).toContainText(/8,20\s%/);
  await expect(rendement.getByTestId('perf-base')).toContainText('annualisé');
});

test('portefeuille modèle : liste, création avec cibles en fraction et nouvelle version', async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.getByTestId('pf-nav-modeles').click();
  await expect(page.getByTestId('modeles')).toContainText('Croissance 80/20');

  await page.getByTestId('nouveau-modele').click();
  await page.getByTestId('champ-nom').fill('Mon modèle');
  const cibles: Record<string, string> = {
    equity_ca: '27,5',
    equity_us: '43,5',
    equity_intl: '29',
    bond_ca: '0',
    bond_other: '0',
    other_assets: '0',
  };
  for (const [classe, valeur] of Object.entries(cibles)) {
    await page.getByTestId(`cible-${classe}`).fill(valeur);
  }
  await page.getByTestId('fonds-symbole-0').fill('tsx:xeqt');
  await page.getByTestId('fonds-poids-0').fill('100');
  const requete = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/v1/portfolio/models'));
  await page.getByTestId('enregistrer').click();
  const corps = (await requete).postDataJSON();
  expect(corps.targets[0]).toEqual({ assetClass: 'equity_ca', target: 0.275 });
  expect(corps.holdings).toEqual([{ symbol: 'TSX:XEQT', weight: 1 }]);
  await expect(page.getByTestId('modele-confirmation')).toContainText('version');

  // Nouvelle version : le nom est fixe, la requête est un PUT.
  await page.getByTestId(`nouvelle-version-${MODELE_ID}`).click();
  await expect(page.getByTestId('champ-nom')).not.toBeEditable();
  const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith(`/api/v1/portfolio/models/${MODELE_ID}`));
  await page.getByTestId('enregistrer').click();
  expect((await put).postDataJSON().holdings[0]).toEqual({ symbol: 'TSX:XEQT', weight: 0.8 });
  // Attend la réponse avant de finir le test : une requête en vol ferait échouer le test suivant.
  await expect(page.getByTestId('modele-confirmation')).toContainText('enregistré');
});

test("plan d'achat : prix saisis, ordres en parts entières et encaisse restante", async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.getByTestId('pf-nav-achat').click();
  await page.getByTestId('achat-montant').fill('10 000,00');
  await page.getByTestId('prix-TSX:XEQT').fill('37,50');
  const requete = page.waitForRequest((r) => r.url().endsWith('/purchase-plan'));
  await page.getByTestId('achat-calculer').click();
  expect((await requete).postDataJSON()).toEqual({ amount: '10000.00', currency: 'CAD', prices: { 'TSX:XEQT': '37.50' } });
  await expect(page.getByTestId('achat-encaisse')).toContainText('9,85 CAD');
  await expect(page.getByTestId('ordre-0')).toContainText('213');
  await expect(page.getByTestId('ordre-0')).toContainText('Achat');
});

test("plan d'achat : un 422 liste les fonds sans prix", async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.route('**/purchase-plan', (route) =>
    route.fulfill({
      status: 422,
      contentType: 'application/problem+json',
      body: JSON.stringify({ type: 'about:blank', title: 'Prix manquant', status: 422, detail: 'prix manquant pour TSX:XEQT, TSX:ZAG' }),
    }),
  );
  await page.getByTestId('pf-nav-achat').click();
  await page.getByTestId('achat-montant').fill('500');
  await page.getByTestId('achat-calculer').click();
  const alerte = page.getByTestId('achat-sans-prix');
  await expect(alerte).toContainText('Fonds sans prix');
  await expect(page.getByTestId('achat-fonds-sans-prix')).toContainText('TSX:ZAG');
  await expect(page.getByTestId('achat-fonds-sans-prix')).toContainText('TSX:XEQT');
});

test('plan de rééquilibrage : cotisation par compte, classes hors bande et ordres', async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.getByTestId('pf-nav-reequilibrage').click();
  await page.getByTestId(`reeq-cotisation-${COMPTE_ID}`).fill('1 000');
  const requete = page.waitForRequest((r) => r.url().endsWith('/portfolio/rebalance-plans'));
  await page.getByTestId('reeq-calculer').click();
  const corps = (await requete).postDataJSON();
  expect(corps.modelId).toBe(MODELE_ID);
  expect(corps.contributions).toEqual([{ accountId: COMPTE_ID, amount: '1000' }]);
  await expect(page.getByTestId('plan-reeq')).toBeVisible();
  await expect(page.getByTestId('reeq-classes')).toBeVisible();
  await expect(page.getByTestId('reeq-encaisse')).toBeVisible();
  await expect(page.getByTestId('ordres').or(page.getByTestId('ordres-vide'))).toBeVisible();
});

test('exposition par transparence : écarts au modèle et RFG pondéré', async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.getByTestId('pf-nav-exposition').click();
  await expect(page.getByTestId('expo-table')).toBeVisible();
  await expect(page.getByTestId('expo-rfg')).toBeVisible();
  const requete = page.waitForRequest((r) => r.url().includes('/portfolio/exposure?') && r.url().includes('modelId='));
  await page.getByTestId('expo-modele').selectOption(MODELE_ID);
  await page.getByTestId('expo-afficher').click();
  expect(new URL((await requete).url()).searchParams.get('modelId')).toBe(MODELE_ID);
});

test('rendement : base du TRI expliquée, mensuel et annuel de la même série', async ({ page }) => {
  await connecter(page, 'menu-etf');
  await page.getByTestId('pf-nav-rendement').click();
  const consolide = page.getByTestId('perf-consolide');
  await expect(consolide.getByTestId('perf-twr')).toContainText(/8,20\s%/);
  await expect(consolide.getByTestId('perf-irr')).toContainText(/7,90\s%/);
  await expect(consolide.getByTestId('perf-base')).toContainText('annualisé');
  await expect(consolide.getByTestId('perf-table')).toContainText('2025-01');
  await expect(page.getByTestId(`serie-${COMPTE_ID}`)).toBeVisible();
  await page.getByTestId('perf-granularite').selectOption('annual');
  await expect(consolide.getByTestId('perf-table')).not.toContainText('2025-01');
  await expect(consolide.getByTestId('perf-table')).toContainText('2025');
});
