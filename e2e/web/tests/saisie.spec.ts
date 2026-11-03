import { expect, test, type Page, type Request, type Route } from '@playwright/test';

// Utilisateur du realm de test (deploy/keycloak/realm-finance-test.json) : mot de passe factice.
const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test';
const SYMBOLE = 'TSX:CNR';

async function connecter(page: Page): Promise<void> {
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
}

interface Fait {
  fiscalYear: number;
  metric: string;
  value: string;
  currency: string;
  source: string;
}

interface Ecritures {
  facts: Fait[][];
  snapshots: Record<string, unknown>[];
  qualitatifs: Record<string, unknown>[];
}

/**
 * Simule l'API en mémoire. Le « serveur » recalcule le score : 9/10 dès que le BPA de 2025 est saisi à la main,
 * 8/10 sinon. Le client n'affiche que ce qui lui est renvoyé. Un conflit disparaît quand une valeur manuelle
 * est écrite pour la même mesure et la même année.
 */
async function simulerApi(page: Page): Promise<Ecritures> {
  const faits: Fait[] = [
    { fiscalYear: 2024, metric: 'diluted_eps', value: '7.10', currency: 'CAD', source: 'xlsx' },
    { fiscalYear: 2025, metric: 'diluted_eps', value: '7.55', currency: 'CAD', source: 'xlsx' },
    { fiscalYear: 2025, metric: 'annual_return', value: '0.5', currency: 'CAD', source: 'xlsx' },
  ];
  let conflits = [
    { fiscalYear: 2024, metric: 'diluted_eps', values: [{ source: 'xlsx', value: '7.10' }, { source: 'manual', value: '7.01' }] },
  ];
  let qualitatif: Record<string, unknown> = {
    rule1: { answer: 'yes', note: 'Franchise' },
    rule2: { answer: 'yes' },
    rule3: { answer: 'unanswered' },
    rule4: { answer: 'yes' },
    reviewedAt: '2026-10-30T12:05:00Z',
  };
  const ecritures: Ecritures = { facts: [], snapshots: [], qualitatifs: [] };

  const fiche = () => ({
    stock: {
      symbol: SYMBOLE,
      name: 'Chemin de fer Canadien National',
      exchange: 'TSX',
      quoteCurrency: 'CAD',
      reportingCurrency: 'CAD',
      isFinancial: false,
      createdAt: '2026-03-02T14:05:00Z',
    },
    facts: faits,
    snapshot: { currency: 'CAD', price: '152.40', fxRate: 1 },
    qualitative: qualitatif,
    sources: [{ source: 'xlsx', factCount: 3 }],
    conflicts: conflits,
  });

  const evaluation = () => {
    const strict = faits.some((f) => f.metric === 'diluted_eps' && f.fiscalYear === 2025 && f.source === 'manual') ? 9 : 8;
    return {
      symbol: SYMBOLE,
      mode: 'corrected',
      rulesVersion: '2026.1',
      computedAt: '2026-10-30T12:10:00Z',
      qualitativeGatePassed: true,
      horizons: [10].map((horizon) => ({
        horizon,
        indicative: false,
        insufficientHistory: false,
        strict,
        relaxed: 10,
        outOf: 10,
        failureCount: 10 - strict,
        quality: { strict: 6, relaxed: 7, outOf: 7 },
        price: { strict: 2, relaxed: 3, outOf: 3 },
        verdict: 'watch',
        criteria: [],
      })),
    };
  };

  await page.route('**/api/v1/stocks/**', async (route: Route) => {
    const req: Request = route.request();
    const chemin = decodeURIComponent(new URL(req.url()).pathname);
    const base = `/api/v1/stocks/${SYMBOLE}`;
    if (chemin === `${base}/evaluation` && req.method() === 'GET') {
      return route.fulfill({ json: evaluation() });
    }
    if (chemin === `${base}/facts` && req.method() === 'PUT') {
      const corps = req.postDataJSON() as { facts: Fait[] };
      ecritures.facts.push(corps.facts);
      for (const f of corps.facts) {
        const i = faits.findIndex((x) => x.fiscalYear === f.fiscalYear && x.metric === f.metric);
        if (i >= 0) faits[i] = f;
        else faits.push(f);
        conflits = conflits.filter((c) => !(c.fiscalYear === f.fiscalYear && c.metric === f.metric));
      }
      return route.fulfill({ json: { facts: faits } });
    }
    if (chemin === `${base}/qualitative` && req.method() === 'PUT') {
      qualitatif = { ...(req.postDataJSON() as object), reviewedAt: '2026-11-01T09:00:00Z' };
      ecritures.qualitatifs.push(qualitatif);
      return route.fulfill({ json: qualitatif });
    }
    if (chemin === base && req.method() === 'PATCH') {
      ecritures.snapshots.push((req.postDataJSON() as { snapshot: Record<string, unknown> }).snapshot);
      return route.fulfill({ json: fiche() });
    }
    if (chemin === base && req.method() === 'GET') {
      return route.fulfill({ json: fiche() });
    }
    return route.continue();
  });
  return ecritures;
}

const cellule = (page: Page, ligne: string, annee: number) => page.locator(`[row-id="${ligne}"] [col-id="a${annee}"]`);

async function saisir(page: Page, ligne: string, annee: number, texte: string): Promise<void> {
  await cellule(page, ligne, annee).dblclick();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type(texte);
  await page.keyboard.press('Enter');
}

let ecritures: Ecritures;

test.beforeEach(async ({ page }) => {
  ecritures = await simulerApi(page);
  await connecter(page);
  await page.goto(`/stocks/${SYMBOLE}`);
  await page.getByTestId('modifier').click();
  await expect(page).toHaveURL(/\/stocks\/TSX:CNR\/edit$/);
  await expect(page.getByTestId('grille')).toBeVisible();
  await expect(cellule(page, 'diluted_eps', 2025)).toContainText('7,55');
});

test('modifier un BPA, enregistrer et voir le nouveau score de l’API', async ({ page }) => {
  await expect(page.getByTestId('score-10')).toContainText('8/10');
  await saisir(page, 'diluted_eps', 2025, '8,00');
  await expect(cellule(page, 'diluted_eps', 2025)).toContainText('non enregistrée');
  await expect(page.getByTestId('nb-modifs')).toContainText('1');
  await page.getByTestId('enregistrer-faits').click();
  await expect(page.getByTestId('score-10')).toContainText('9/10');
  await expect(cellule(page, 'diluted_eps', 2025)).toContainText('manuelle');
  expect(ecritures.facts[0]).toEqual([{ fiscalYear: 2025, metric: 'diluted_eps', value: '8.00', currency: 'CAD', source: 'manual' }]);
});

test('saisir « 70 » dans le rendement envoie 0.7', async ({ page }) => {
  await expect(cellule(page, 'annual_return', 2025)).toContainText('50 %');
  const requete = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/facts'));
  await saisir(page, 'annual_return', 2025, '70');
  await page.getByTestId('enregistrer-faits').click();
  const corps = (await requete).postDataJSON() as { facts: Fait[] };
  expect(corps.facts).toEqual([{ fiscalYear: 2025, metric: 'annual_return', value: '0.7', currency: 'CAD', source: 'manual' }]);
  await expect(cellule(page, 'annual_return', 2025)).toContainText('70 %');
});

test('une valeur hors bornes est refusée et annoncée', async ({ page }) => {
  await saisir(page, 'annual_return', 2025, '1500');
  await expect(page.getByTestId('grille-erreur')).toContainText('bornes');
  await expect(cellule(page, 'annual_return', 2025)).toContainText('50 %');
  await saisir(page, 'diluted_eps', 2025, 'abc');
  await expect(page.getByTestId('grille-erreur')).toContainText('nombre');
  await expect(page.getByTestId('enregistrer-faits')).toBeDisabled();
});

test('résoudre un conflit retire le badge', async ({ page }) => {
  await expect(cellule(page, 'diluted_eps', 2024).getByTestId('badge-conflit')).toContainText('Conflit');
  await expect(page.getByTestId('conflits')).toBeVisible();
  await page.getByTestId('choisir-diluted_eps-2024-manual').click();
  await expect(page.getByTestId('badge-conflit')).toHaveCount(0);
  await expect(page.getByTestId('conflits')).toHaveCount(0);
  expect(ecritures.facts[0]).toEqual([{ fiscalYear: 2024, metric: 'diluted_eps', value: '7.01', currency: 'CAD', source: 'manual' }]);
});

test('la grille se parcourt et se modifie au clavier', async ({ page }) => {
  await cellule(page, 'diluted_eps', 2025).click();
  await page.keyboard.press('ArrowLeft');
  await expect(cellule(page, 'diluted_eps', 2024)).toHaveClass(/ag-cell-focus/);
  await page.keyboard.press('ArrowDown');
  await expect(cellule(page, 'dividend_per_share', 2024)).toHaveClass(/ag-cell-focus/);
  await page.keyboard.press('Enter');
  await page.keyboard.type('3,38');
  await page.keyboard.press('Enter');
  await expect(cellule(page, 'dividend_per_share', 2024)).toContainText('3,38');
  await expect(page.getByTestId('nb-modifs')).toContainText('1');
  // Échap annule une modification en cours.
  await page.keyboard.press('F2');
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type('9');
  await page.keyboard.press('Escape');
  await expect(cellule(page, 'dividend_per_share', 2024)).toContainText('3,38');
});

test("l'instantané et les règles 1 à 4 s'enregistrent", async ({ page }) => {
  await page.getByTestId('inst-price').fill('160,25');
  await page.getByTestId('inst-fxRate').fill('0');
  await expect(page.getByTestId('enregistrer-instantane')).toBeDisabled();
  await page.getByTestId('inst-fxRate').fill('1,35');
  await page.getByTestId('enregistrer-instantane').click();
  await expect.poll(() => ecritures.snapshots.length).toBe(1);
  expect(ecritures.snapshots[0]).toMatchObject({ currency: 'CAD', price: '160.25', fxRate: 1.35, source: 'manual' });

  await expect(page.getByTestId('revue')).toContainText('Dernière révision');
  await page.getByTestId('rule3-yes').click();
  await page.getByTestId('rule2-note').fill('À revoir');
  await page.getByTestId('enregistrer-qualitatif').click();
  await expect.poll(() => ecritures.qualitatifs.length).toBe(1);
  expect(ecritures.qualitatifs[0]).toMatchObject({
    rule2: { answer: 'yes', note: 'À revoir' },
    rule3: { answer: 'yes' },
  });
});
