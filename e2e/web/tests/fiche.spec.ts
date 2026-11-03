import { expect, test, type Page, type Route } from '@playwright/test';

// Utilisateur du realm de test (deploy/keycloak/realm-finance-test.json) : mot de passe factice.
const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test';

const SYMBOLE = 'TSX:CNR';
const IDS = ['5a', '5b', '6', '7', '8', '9', '10', '11a', '11b', '11c'];

async function connecter(page: Page): Promise<void> {
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
}

type Critere = Record<string, unknown>;

/** Dix critères avec les deux seuils ; les statuts et valeurs sont fournis par la simulation, le client n'en calcule aucun. */
function criteres(mode: string): Critere[] {
  return IDS.map((id, i) => {
    const sansAssoupli = id === '9' || id === '10' || id === '11b';
    const statut = id === '7' ? 'borderline' : id === '11c' && mode === 'compat' ? 'passed' : id === '11c' ? 'failed' : 'passed';
    const critere: Critere = {
      id,
      label: `Critère ${id}`,
      status: statut,
      strictThreshold: { description: `strict ${id}` },
    };
    if (!sansAssoupli) {
      critere['relaxedThreshold'] = { description: `assoupli ${id}` };
    }
    if (id === '5a') critere['value'] = 0.0912;
    else if (id === '5b') critere['value'] = 8;
    else if (id === '11a') critere['value'] = 20.2;
    else if (!['9', '10'].includes(id)) critere['value'] = 0.1 + i / 100;
    return critere;
  });
}

function horizon(h: number, mode: string): Record<string, unknown> {
  return {
    horizon: h,
    indicative: h === 5,
    insufficientHistory: false,
    strict: 8,
    relaxed: 10,
    outOf: 10,
    failureCount: 2,
    quality: { strict: 6, relaxed: 7, outOf: 7 },
    price: { strict: 2, relaxed: 3, outOf: 3 },
    ...(h === 5 ? {} : { verdict: 'watch' }),
    criteria: criteres(mode),
  };
}

function evaluation(mode: string): Record<string, unknown> {
  return {
    symbol: SYMBOLE,
    mode,
    rulesVersion: '2026.1',
    computedAt: '2026-10-30T12:10:00Z',
    qualitativeGatePassed: true,
    warnings: ['Un taux ignoré, division par zéro en 2016.'],
    horizons: (mode === 'compat' ? [20] : [5, 10, 15, 20]).map((h) => horizon(h, mode)),
  };
}

async function simulerApi(page: Page): Promise<void> {
  const archives: Record<string, unknown>[] = [];
  await page.route('**/api/v1/stocks/**', async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const chemin = decodeURIComponent(url.pathname);
    if (chemin === `/api/v1/stocks/${SYMBOLE}/evaluation` && req.method() === 'GET') {
      return route.fulfill({ json: evaluation(url.searchParams.get('mode') ?? 'corrected') });
    }
    if (chemin === `/api/v1/stocks/${SYMBOLE}/evaluations`) {
      if (req.method() === 'POST') {
        const enregistrement = {
          id: `00000000-0000-4000-8000-00000000000${archives.length + 1}`,
          createdAt: '2026-10-31T09:00:00Z',
          mode: 'corrected',
          rulesVersion: '2026.1',
          inputsFingerprint: 'f'.repeat(64),
          evaluation: evaluation('corrected'),
        };
        archives.unshift(enregistrement);
        return route.fulfill({ status: 201, json: enregistrement });
      }
      return route.fulfill({ json: { items: archives } });
    }
    return route.continue();
  });
}

test.beforeEach(async ({ page }) => {
  await simulerApi(page);
  await connecter(page);
  await page.goto(`/stocks/${SYMBOLE}`);
  await expect(page.getByRole('heading', { name: `Fiche de ${SYMBOLE}` })).toBeVisible();
  await expect(page.getByTestId('horizon-10')).toBeVisible();
});

test('chaque horizon affiche les 10 critères avec valeur, deux seuils et statut en texte', async ({ page }) => {
  for (const h of [5, 10, 15, 20]) {
    const section = page.getByTestId(`horizon-${h}`);
    await expect(section.getByTestId('critere')).toHaveCount(10);
    await expect(section.getByTestId('scores')).toContainText('8/10');
    await expect(section.getByTestId('scores')).toContainText('10/10');
  }
  const ligne = page.getByTestId('horizon-10').getByTestId('critere').first();
  await expect(ligne).toContainText('5a — Critère 5a');
  // Fraction 0.0912 reçue de l'API, affichée en pourcentage fr-CA.
  await expect(ligne.locator('[data-col="valeur"]')).toContainText(/9,12\s%/);
  await expect(ligne.locator('[data-col="strict"]')).toContainText('strict 5a');
  await expect(ligne.locator('[data-col="assoupli"]')).toContainText('assoupli 5a');
  await expect(ligne.locator('[data-col="statut"]')).toContainText('Réussi');
  await expect(ligne.locator('[data-col="statut"] [aria-hidden="true"]')).toHaveText('✔');
  const limite = page.getByTestId('horizon-10').getByTestId('critere').filter({ hasText: '7 — Critère 7' });
  await expect(limite.locator('[data-col="statut"]')).toContainText('Limite');
  await expect(limite.locator('[data-col="statut"] [aria-hidden="true"]')).toHaveText('◐');
  await expect(page.getByTestId('horizon-10').getByTestId('verdict')).toContainText('À surveiller');
  await expect(page.getByTestId('avertissements')).toContainText('division par zéro en 2016');
});

test("l'horizon de 5 ans porte la mention « indicatif » et aucun autre", async ({ page }) => {
  await expect(page.getByTestId('horizon-5').getByTestId('indicatif')).toHaveText('indicatif');
  await expect(page.getByTestId('horizon-5').getByTestId('verdict')).toContainText('Indicatif seulement');
  for (const h of [10, 15, 20]) {
    await expect(page.getByTestId(`horizon-${h}`).getByTestId('indicatif')).toHaveCount(0);
  }
});

test('explication du statut au survol et au focus', async ({ page }) => {
  const statut = page.getByTestId('horizon-10').getByTestId('critere').first().locator('.statut');
  await statut.hover();
  await expect(page.locator('.mdc-tooltip__surface')).toContainText('respecte le seuil strict');
  await page.mouse.move(0, 0);
  await expect(page.locator('.mdc-tooltip__surface')).toBeHidden();
  // Au clavier : Tab depuis le premier statut atteint le deuxième, dont l'explication s'affiche.
  await statut.focus();
  await page.keyboard.press('Tab');
  await expect(page.getByTestId('horizon-10').getByTestId('critere').nth(1).locator('.statut')).toBeFocused();
  await expect(page.locator('.mdc-tooltip__surface')).toContainText('respecte le seuil strict');
});

test('comparaison corrigé / compat côte à côte, écarts en texte', async ({ page }) => {
  const cmp = page.getByTestId('comparaison');
  await expect(cmp).toContainText('horizon de 20 ans');
  await expect(cmp.getByTestId('ligne-comparaison')).toHaveCount(10);
  await expect(cmp.getByTestId('nb-ecarts')).toContainText('1');
  const p = cmp.getByTestId('ligne-comparaison').filter({ hasText: '11c — Critère 11c' });
  await expect(p.locator('[data-col="corrige"]')).toContainText('Échoué');
  await expect(p.locator('[data-col="compat"]')).toContainText('Réussi');
  await expect(p.locator('[data-col="ecart"]')).toContainText('Écart de statut');
  const identique = cmp.getByTestId('ligne-comparaison').filter({ hasText: '5a — Critère 5a' });
  await expect(identique.locator('[data-col="ecart"]')).toContainText('Identique');
});

test("archiver ajoute une ligne à l'historique", async ({ page }) => {
  await page.getByRole('tab', { name: 'Historique' }).click();
  await expect(page.getByTestId('historique-vide')).toBeVisible();
  await page.getByTestId('archiver').click();
  await expect(page.getByTestId('ligne-historique')).toHaveCount(1);
  await expect(page.getByTestId('ligne-historique')).toContainText('2026.1');
  await expect(page.getByTestId('ligne-historique')).toContainText('Corrigé');
  await page.getByTestId('archiver').click();
  await expect(page.getByTestId('ligne-historique')).toHaveCount(2);
});

test('le lien de retour ramène à la liste de suivi', async ({ page }) => {
  await page.getByTestId('retour').click();
  await expect(page).toHaveURL(/\/stocks$/);
});
