import { expect, test, type Page, type Route } from '@playwright/test';

// Utilisateur du realm de test (deploy/keycloak/realm-finance-test.json) : mot de passe factice.
const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test';
const PRISM = 'http://127.0.0.1:4010';

interface Ligne {
  stock: Record<string, unknown> & { symbol: string; name: string; exchange: string };
  score: Record<string, unknown> & { horizon: number };
}

async function connecter(page: Page): Promise<void> {
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
}

/**
 * Simule l'API à partir de l'exemple du contrat servi par Prism : l'exemple est décliné en quatre actions
 * (verdicts et bourses différents). Les scores viennent de ces réponses, le client n'en calcule aucun.
 * Les ajouts et retraits modifient la liste en mémoire, comme le ferait l'API réelle.
 */
async function simulerApi(page: Page): Promise<void> {
  const exemple = await (await fetch(`${PRISM}/api/v1/stocks`, { headers: { Authorization: 'Bearer x' } })).json();
  const base: Ligne = exemple.items[0];
  const actions = new Map<string, Ligne['stock']>();
  const definir = (symbol: string, name: string, exchange: string): void => {
    actions.set(symbol, { ...base.stock, symbol, name, exchange });
  };
  definir('TSX:CNR', 'Chemin de fer Canadien National', 'TSX');
  definir('NYSE:AAA', 'Alpha', 'NYSE');
  definir('TSX:BBB', 'Bêta', 'TSX');
  definir('NASDAQ:CCC', 'Gamma', 'NASDAQ');

  const score = (symbol: string, horizon: number): Ligne['score'] => {
    const s = { ...base.score, horizon, insufficientHistory: false };
    const sous = (strict: number, relaxed: number, outOf: number) => ({ strict, relaxed, outOf });
    switch (symbol) {
      case 'NYSE:AAA':
        return { ...s, strict: 10, relaxed: 10, failureCount: 0, quality: sous(7, 7, 7), price: sous(3, 3, 3), verdict: horizon >= 10 ? 'candidate' : undefined };
      case 'TSX:BBB':
        return { ...s, strict: 4, relaxed: 6, failureCount: 6, quality: sous(3, 4, 7), price: sous(1, 2, 3), verdict: horizon >= 10 ? 'rejected' : undefined };
      case 'NASDAQ:CCC':
        return { ...s, strict: 0, relaxed: 0, failureCount: 10, quality: sous(0, 0, 7), price: sous(0, 0, 3), verdict: 'excluded' };
      default:
        return { ...s, strict: 8, relaxed: 10, failureCount: 2, quality: sous(6, 7, 7), price: sous(2, 3, 3), verdict: horizon >= 10 ? 'watch' : undefined };
    }
  };

  await page.route('**/api/v1/stocks**', async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'GET' && url.pathname === '/api/v1/stocks') {
      const horizon = Number(url.searchParams.get('horizon') ?? 10);
      const items = [...actions.values()]
        .sort((a, b) => a.symbol.localeCompare(b.symbol))
        .map((stock) => ({ stock, score: score(stock.symbol, horizon) }));
      return route.fulfill({ json: { items } });
    }
    if (req.method() === 'POST') {
      const corps = req.postDataJSON() as { symbol: string; name: string };
      if (actions.has(corps.symbol)) {
        return route.fulfill({
          status: 409,
          contentType: 'application/problem+json',
          json: { type: 'about:blank', title: 'Conflit', status: 409 },
        });
      }
      definir(corps.symbol, corps.name, corps.symbol.split(':')[0]);
      return route.fulfill({ status: 201, json: actions.get(corps.symbol) });
    }
    if (req.method() === 'DELETE') {
      actions.delete(decodeURIComponent(url.pathname.split('/').pop() ?? ''));
      return route.fulfill({ status: 204 });
    }
    return route.continue();
  });
}

async function symboles(page: Page): Promise<string[]> {
  return page.locator('[data-testid="ligne"] td:first-child').allTextContents().then((l) => l.map((s) => s.trim()));
}

test.beforeEach(async ({ page }) => {
  await simulerApi(page);
  await connecter(page);
  await expect(page.getByTestId('tableau')).toBeVisible();
});

test('colonnes, verdict en texte avec icône, plus long horizon à 10/10', async ({ page }) => {
  await expect(page.getByRole('heading', { name: 'Liste de suivi' })).toBeVisible();
  for (const nom of ['Symbole', 'Nom', 'Score strict', 'Score assoupli', 'Qualité / prix', 'Verdict', 'Plus long horizon à 10/10']) {
    await expect(page.getByRole('columnheader', { name: nom })).toBeVisible();
  }
  const ligne = page.getByRole('row').filter({ hasText: 'NYSE:AAA' });
  await expect(ligne.locator('[data-col="strict"]')).toContainText('10/10');
  await expect(ligne.locator('[data-col="verdict"]')).toContainText('Candidate');
  await expect(ligne.locator('[data-col="verdict"] [aria-hidden="true"]')).toHaveText('✔');
  await expect(ligne.locator('[data-col="horizon"]')).toContainText('20 ans');
  const cnr = page.getByRole('row').filter({ hasText: 'TSX:CNR' });
  await expect(cnr.locator('[data-col="verdict"]')).toContainText('À surveiller');
  await expect(cnr.locator('[data-col="horizon"]')).toContainText('Aucun');
});

test('tri par score strict, croissant puis décroissant', async ({ page }) => {
  await expect.poll(() => symboles(page)).toEqual(['NASDAQ:CCC', 'NYSE:AAA', 'TSX:BBB', 'TSX:CNR']);
  const entete = page.getByRole('columnheader', { name: 'Score strict' });
  await entete.click();
  await expect(entete).toHaveAttribute('aria-sort', 'ascending');
  await expect.poll(() => symboles(page)).toEqual(['NASDAQ:CCC', 'TSX:BBB', 'TSX:CNR', 'NYSE:AAA']);
  await entete.click();
  await expect(entete).toHaveAttribute('aria-sort', 'descending');
  await expect.poll(() => symboles(page)).toEqual(['NYSE:AAA', 'TSX:CNR', 'TSX:BBB', 'NASDAQ:CCC']);
});

test('filtres : verdict, bourse, mode compat', async ({ page }) => {
  await page.getByTestId('filtre-verdict').click();
  await page.getByRole('option', { name: 'Candidate' }).click();
  await expect.poll(() => symboles(page)).toEqual(['NYSE:AAA']);

  await page.getByTestId('filtre-verdict').click();
  await page.getByRole('option', { name: 'Tous' }).click();
  await page.getByTestId('filtre-bourse').click();
  await page.getByRole('option', { name: 'TSX', exact: true }).click();
  await expect.poll(() => symboles(page)).toEqual(['TSX:BBB', 'TSX:CNR']);

  await page.getByTestId('filtre-verdict').click();
  await page.getByRole('option', { name: 'Candidate' }).click();
  await expect(page.getByTestId('vide')).toBeVisible();

  await page.getByTestId('filtre-mode').click();
  await page.getByRole('option', { name: 'Compat' }).click();
  await expect(page.getByTestId('filtre-horizon')).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByText("Le mode compat ne s'applique qu'à l'horizon de 20 ans.")).toBeVisible();
});

test('filtre horizon : le verdict est indicatif à 5 ans', async ({ page }) => {
  await page.getByTestId('filtre-horizon').click();
  await page.getByRole('option', { name: '5 ans', exact: true }).click();
  const cnr = page.getByRole('row').filter({ hasText: 'TSX:CNR' });
  await expect(cnr.locator('[data-col="verdict"]')).toContainText('Indicatif seulement');
});

test('ajout puis retrait d’une action', async ({ page }) => {
  await page.getByTestId('ajouter').click();
  const dialogue = page.getByRole('dialog');
  await dialogue.getByTestId('ajout-symbole').fill('tsx:cnr');
  await dialogue.getByTestId('ajout-nom').fill('Nouvelle');
  await dialogue.getByTestId('ajout-confirmer').click();
  await expect(dialogue.getByText('Symbole invalide')).toBeVisible();

  await dialogue.getByTestId('ajout-symbole').fill('TSX:CNR');
  await dialogue.getByTestId('ajout-confirmer').click();
  await expect(dialogue.getByTestId('ajout-erreur')).toContainText('déjà dans la liste');

  await dialogue.getByTestId('ajout-symbole').fill('TSX:NEW');
  await dialogue.getByTestId('ajout-confirmer').click();
  await expect(dialogue).toBeHidden();
  await expect(page.getByText('Action ajoutée : TSX:NEW')).toBeVisible();
  await expect.poll(() => symboles(page)).toContain('TSX:NEW');

  await page.getByTestId('retirer-TSX:NEW').click();
  await page.getByTestId('retrait-annuler').click();
  await expect(page.getByRole('dialog')).toBeHidden();
  await expect.poll(() => symboles(page)).toContain('TSX:NEW');

  await page.getByTestId('retirer-TSX:NEW').click();
  await expect(page.getByRole('dialog')).toContainText('Retirer TSX:NEW de la liste de suivi');
  await page.getByTestId('retrait-confirmer').click();
  await expect(page.getByText('Action retirée : TSX:NEW')).toBeVisible();
  await expect.poll(() => symboles(page)).not.toContain('TSX:NEW');
});

test('navigation clavier : filtres, en-têtes de tri, boutons de retrait et dialogue', async ({ page }) => {
  await page.getByTestId('ajouter').focus();
  // Le tri et les filtres sont atteignables au clavier : Tab jusqu'à l'en-tête, Entrée trie.
  const entete = page.getByRole('columnheader', { name: 'Score strict' });
  await entete.locator('[role="button"], button').first().focus();
  await page.keyboard.press('Enter');
  await expect(entete).toHaveAttribute('aria-sort', 'ascending');

  // Parcours complet de la page : chaque contrôle interactif reçoit le focus par Tab.
  await page.getByTestId('ajouter').focus();
  const attendus = ['ajouter', 'filtre-bourse', 'filtre-verdict', 'filtre-mode', 'filtre-horizon'];
  for (const id of attendus.slice(1)) {
    await page.keyboard.press('Tab');
    await expect(page.getByTestId(id)).toBeFocused();
  }
  // Puis les sept en-têtes triables, enfin les boutons de retrait.
  const focales: string[] = [];
  for (let i = 0; i < 7; i++) {
    await page.keyboard.press('Tab');
    focales.push(await page.evaluate(() => document.activeElement?.closest('th')?.textContent?.trim() ?? ''));
  }
  expect(focales).toEqual([
    'Symbole',
    'Nom',
    'Score strict',
    'Score assoupli',
    'Qualité / prix',
    'Verdict',
    'Plus long horizon à 10/10',
  ]);
  // Chaque ligne : le lien vers la fiche, puis le bouton de retrait.
  await page.keyboard.press('Tab');
  await expect(page.getByTestId('fiche-NASDAQ:CCC')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.getByTestId('retirer-NASDAQ:CCC')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toBeHidden();
  await expect(page.getByTestId('retirer-NASDAQ:CCC')).toBeFocused();
});
