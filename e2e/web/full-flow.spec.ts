import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// P1-22 : parcours complet sur la pile réelle. Classeur de test et attendus produits par
// `tools/oracle/e2e_workbook.py` (D33, D240) dans `.generated/` ; aucune donnée réelle.
const ici = dirname(fileURLToPath(import.meta.url));
const CLASSEUR = join(ici, '.generated/classeur-5-actions.xlsx');
const ATTENDU = JSON.parse(readFileSync(join(ici, '.generated/attendu-libreoffice.json'), 'utf-8')) as {
  actions: {
    symbole: string;
    moyenne_bpa: number | null;
    hausses_bpa: number | null;
    moyenne_dividende: number | null;
    drapeaux: Record<string, number | null>;
    echecs: number | null;
  }[];
};

const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test'; // realm de test, factice
const MODIFIEE = 'TSX:FAUXD';
const IDS = ['5a', '5b', '6', '7', '8', '9', '10', '11a', '11b', '11c'];

async function connecter(page: Page): Promise<void> {
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
}

async function jeton(request: APIRequestContext): Promise<string> {
  const r = await request.post('/auth/realms/finance/protocol/openid-connect/token', {
    form: { grant_type: 'password', client_id: 'finance-test', username: UTILISATEUR, password: MOT_DE_PASSE },
  });
  expect(r.ok()).toBe(true);
  return ((await r.json()) as { access_token: string }).access_token;
}

interface Critere {
  id: string;
  status: string;
  value?: number;
}
interface Horizon {
  horizon: number;
  strict: number;
  outOf: number;
  failureCount: number;
  criteria: Critere[];
}

const cellule = (page: Page, ligne: string, annee: number) => page.locator(`[row-id="${ligne}"] [col-id="a${annee}"]`);
const compact = (s: string | null) => (s ?? '').replace(/\s+/g, ' ').trim();

// Les étapes partagent l'état de la pile (import, lecture, puis modification) : exécution en série.
test.describe.configure({ mode: 'serial' });

test('1. connexion, import du classeur de 5 actions, la liste montre 5 actions', async ({ page }) => {
  await connecter(page);
  await page.getByTestId('menu-import').click();
  await expect(page.getByRole('heading', { name: 'Importer la feuille' })).toBeVisible();
  await page.getByTestId('annee').fill('2025');
  await page.getByTestId('fichier').setInputFiles(CLASSEUR);
  await page.getByTestId('envoyer').click();
  await expect(page.getByTestId('statut')).toContainText('Import terminé', { timeout: 60_000 });
  await expect(page.getByTestId('importees')).toContainText('5');

  await page.getByTestId('menu-stockeval').click();
  await expect(page.getByTestId('ligne')).toHaveCount(5, { timeout: 30_000 });
  for (const a of ATTENDU.actions) {
    await expect(page.getByTestId(`fiche-${a.symbole}`)).toBeVisible();
  }
});

// Vérification croisée (avant toute modification) : scores compat de l'app = scores de LibreOffice.
for (const a of ATTENDU.actions) {
  test(`2. compat = LibreOffice pour ${a.symbole}`, async ({ page, request }) => {
    // DÉFAUT CONNU, tâche P1-12 (http/evaluate.go, compatInputs) : voir docs/gates/phase-1-integration.md.
    // La moyenne du rendement (11b) est calculée sur la fenêtre complète de 20 ans du mode corrigé ; avec un
    // historique de rendement plus court elle est absente et la règle « réussit » en compat, alors que la
    // feuille moyenne les cellules présentes. Retirer cette ligne quand P1-12 est corrigée.
    test.fail(a.symbole === 'TSX:FAUXE', 'défaut P1-12 : 11b compat avec historique de rendement court');

    const entetes = { Authorization: `Bearer ${await jeton(request)}` };
    const r = await request.get(`/api/v1/stocks/${encodeURIComponent(a.symbole)}/evaluation?mode=compat`, { headers: entetes });
    expect(r.ok(), a.symbole).toBe(true);
    const h = ((await r.json()) as { horizons: Horizon[] }).horizons.find((x) => x.horizon === 20);
    expect(h, `horizon 20 de ${a.symbole}`).toBeDefined();
    const crit = Object.fromEntries(h!.criteria.map((c) => [c.id, c]));
    expect(a.echecs, `LibreOffice ${a.symbole}`).not.toBeNull();
    expect(h!.failureCount, `échecs ${a.symbole}`).toBe(a.echecs);
    expect(h!.strict, `score strict ${a.symbole}`).toBe(10 - a.echecs!);
    for (const id of IDS) {
      const attendu = a.drapeaux[id] === 1 ? 'failed' : 'passed';
      const obtenu = crit[id]?.status === 'passed' ? 'passed' : 'failed';
      expect(obtenu, `critère ${id} de ${a.symbole} (app : ${crit[id]?.status})`).toBe(attendu);
    }
    // L'API publie les ratios arrondis à 7 décimales (écart observé < 5e-9) : tolérance 5e-7 (D240).
    expect(crit['5a'].value!, `5a ${a.symbole}`).toBeCloseTo(a.moyenne_bpa!, 6);
    expect(crit['5b'].value!, `5b ${a.symbole}`).toBe(a.hausses_bpa);
    expect(crit['6'].value!, `6 ${a.symbole}`).toBeCloseTo(a.moyenne_dividende!, 6);

    // Même vérification à l'écran : la liste en mode compat affiche le score strict de la feuille.
    await connecter(page);
    await page.getByTestId('filtre-mode').click();
    await page.getByRole('option', { name: 'Compat' }).click();
    const ligne = page.getByTestId('ligne').filter({ has: page.getByTestId(`fiche-${a.symbole}`) });
    await expect(ligne.locator('[data-col="strict"]')).toContainText(`${10 - a.echecs!}/10`);
  });
}

test("3. fiche, modification d'un BPA, score, archivage, historique", async ({ page }) => {
  await connecter(page);
  await page.getByTestId(`fiche-${MODIFIEE}`).click();
  await expect(page.getByTestId('titre')).toContainText(MODIFIEE);
  await expect(page.getByTestId('meta')).toBeVisible();

  await page.getByTestId('modifier').click();
  await expect(page.getByTestId('grille')).toBeVisible();
  const score = page.getByTestId('score-20');
  await expect(score).toBeVisible();
  const avant = compact(await score.textContent());
  await cellule(page, 'diluted_eps', 2025).dblclick();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type('30');
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('nb-modifs')).toContainText('1');
  await page.getByTestId('enregistrer-faits').click();
  await expect(cellule(page, 'diluted_eps', 2025)).toContainText('manuelle');
  await expect.poll(async () => compact(await score.textContent()), { timeout: 30_000 }).not.toBe(avant);
  const apres = compact(await score.textContent());
  const strict = /Score strict (\d+)\/10/.exec(apres);
  expect(strict, `score affiché après modification : ${apres}`).not.toBeNull();

  await page.getByTestId('retour-fiche').click();
  await expect(page.getByTestId('meta')).toBeVisible();
  await page.getByRole('tab', { name: 'Historique' }).click();
  await page.getByTestId('archiver').click();
  const ligneHisto = page.getByTestId('ligne-historique').first();
  await expect(ligneHisto).toBeVisible({ timeout: 30_000 });
  await expect(ligneHisto).toContainText('Corrigé');
  await expect(ligneHisto).toContainText(`20 ans : ${strict![1]}/10`);
});
