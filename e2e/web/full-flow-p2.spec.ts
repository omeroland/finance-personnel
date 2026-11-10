import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { ACHAT, CELI, MODELE, ORACLE_RE, PROFILS, REEQ, SYN, TAXABLE, VFV, XEQT, XIC, ZAG, csv } from './p2-donnees';
import { CLASSES, Q, planAchat, positionsAttendues, rendementTransactions, replier, exposition, type Tx } from './p2-reference';

// P2-14 : parcours complet de la phase 2 sur la PILE RÉELLE (`make up-test`, realm de test, données synthétiques).
// Les chiffres sont comparés (a) aux valeurs de l'oracle LibreOffice (testdata/oracle/etf_cases.json) quand un cas
// correspond, (b) sinon à un calcul indépendant (p2-reference.ts, règles de docs/decisions.md). Chaque comparaison
// écrit une ligne « PREUVE » (conservée dans docs/gates/phase-2-preuves/).
const ici = dirname(fileURLToPath(import.meta.url));
const ORACLE = JSON.parse(readFileSync(join(ici, '../../testdata/oracle/etf_cases.json'), 'utf-8')) as {
  cas: { id: string; entrees: any; outil: any; attendu_app: any }[];
};
const cas = (id: string) => {
  const c = ORACLE.cas.find((x) => x.id === id);
  if (!c) throw new Error(`cas d'oracle introuvable : ${id}`);
  return c;
};

const UTILISATEUR = 'test-user';
const MOT_DE_PASSE = 'FAUX-mot-de-passe-de-test'; // realm de test, factice
const PROJET = process.env['COMPOSE_PROJECT_NAME'] ?? 'financegate2p2';
const RUN = Date.now().toString(36); // noms uniques : plusieurs exécutions de suite sur la même pile
const NOM = {
  celi: `CELI Gate ${RUN}`,
  taxable: `Non enregistré Gate ${RUN}`,
  reer: `REER Relevés Gate ${RUN}`,
  oracle: `Oracle Gate ${RUN}`,
};
const preuve = (id: string, detail: string) => console.log(`PREUVE ${id} : ${detail}`);

// État partagé des étapes (exécution en série).
const etat = {
  sub: '',
  celi: '',
  taxable: '',
  reer: '',
  oracle: '',
  modeleId: '',
  modeleNom: `Modèle Gate ${RUN}`,
};

// Le test lui-même ne doit pas déclencher la limitation de débit de Traefik (20 req/s, rafale de 40, D34) : les appels
// d'API du test sont espacés et une courte pause précède chaque connexion (le client web fait plusieurs appels par écran).
let dernierAppel = 0;
async function espacer(ms = 120): Promise<void> {
  const attente = dernierAppel + ms - Date.now();
  if (attente > 0) await new Promise((ok) => setTimeout(ok, attente));
  dernierAppel = Date.now();
}

async function connecter(page: Page): Promise<void> {
  await new Promise((ok) => setTimeout(ok, 1500));
  await page.goto('/');
  await page.waitForURL(/\/auth\/realms\/finance\/protocol\/openid-connect\/auth/);
  await page.locator('#username').fill(UTILISATEUR);
  await page.locator('#password').fill(MOT_DE_PASSE);
  await page.locator('#kc-login').click();
  await expect(page).toHaveURL(/\/stocks$/);
}

let jetonCache: { valeur: string; a: number } | null = null;
async function jeton(request: APIRequestContext): Promise<string> {
  if (jetonCache && Date.now() - jetonCache.a < 120_000) return jetonCache.valeur;
  const r = await request.post('/auth/realms/finance/protocol/openid-connect/token', {
    form: { grant_type: 'password', client_id: 'finance-test', username: UTILISATEUR, password: MOT_DE_PASSE },
  });
  expect(r.ok()).toBe(true);
  const valeur = ((await r.json()) as { access_token: string }).access_token;
  jetonCache = { valeur, a: Date.now() };
  return valeur;
}

async function api<T = any>(request: APIRequestContext, methode: 'GET' | 'POST' | 'PUT', chemin: string, corps?: unknown, attendu = 200): Promise<T> {
  const entetes = { Authorization: `Bearer ${await jeton(request)}` };
  // Sur 429 (limitation de débit), on attend et on rejoue ; jamais sur une autre réponse.
  await espacer();
  let r = await request.fetch(`/api/v1${chemin}`, { method: methode, headers: entetes, data: corps });
  for (let essai = 0; r.status() === 429 && essai < 6; essai++) {
    await new Promise((ok) => setTimeout(ok, 1500));
    r = await request.fetch(`/api/v1${chemin}`, { method: methode, headers: entetes, data: corps });
  }
  const texte = await r.text();
  expect(r.status(), `${methode} ${chemin} : ${texte.slice(0, 300)}`).toBe(attendu);
  return (texte ? JSON.parse(texte) : {}) as T;
}

/** psql dans la base de la pile de test (propriétaire des tables), pour poser le catalogue synthétique. */
function psql(sql: string): string {
  return execFileSync('docker', ['exec', '-i', `${PROJET}-db-1`, 'psql', '-U', 'finance', '-d', 'finance', '-v', 'ON_ERROR_STOP=1', '-Atq'], {
    input: sql,
    encoding: 'utf-8',
  }).trim();
}

/** Nombre lu dans un texte français (« 7 581,035 CAD », « 3,72 % »). */
function lire(texte: string | null): number {
  const t = (texte ?? '').replace(/[\s  ]/g, '').replace(',', '.').replace(/[^0-9.\-−]/g, '').replace('−', '-');
  return Number(t);
}
const proche = (a: number, b: number, tol: number, msg: string) => expect(Math.abs(a - b), `${msg} : ${a} vs ${b}`).toBeLessThanOrEqual(tol);

test.describe.configure({ mode: 'serial' });

test('0. catalogue synthétique : 4 FNB (instruments et profils) dans le catalogue du propriétaire de test', async ({ request }) => {
  const tok = await jeton(request);
  etat.sub = JSON.parse(Buffer.from(tok.split('.')[1], 'base64url').toString()).sub as string;
  expect(etat.sub).toMatch(/^[0-9a-f-]{36}$/);
  // Le contrat n'a aucune route d'import du catalogue (D401) : posé en SQL sur la pile de test, données inventées.
  let sql = '';
  for (const [symbole, p] of Object.entries({ ...PROFILS, ...SYN })) {
    sql += `INSERT INTO core.instrument (owner_id, symbol, name, exchange, kind, quote_currency, report_currency)
            VALUES ('${etat.sub}', '${symbole}', 'FNB synthétique ${symbole}', 'TSX', 'etf', 'CAD', 'CAD') ON CONFLICT (owner_id, symbol) DO NOTHING;\n`;
    const r = (c: string) => (p.repartition as Record<string, number>)[c] ?? 0;
    sql += `INSERT INTO etf.etf_profile (owner_id, instrument_id, as_of, type, region, equity_ca, equity_us, equity_intl, bond_ca, bond_other, other_assets, mer, esg, source)
            SELECT '${etat.sub}', id, DATE '2025-10-31', '${p.type}', '${p.region}', ${r('equity_ca')}, ${r('equity_us')}, ${r('equity_intl')}, ${r('bond_ca')}, ${r('bond_other')}, ${r('other_assets')}, ${p.rfg}, false, 'manual'
            FROM core.instrument WHERE owner_id = '${etat.sub}' AND symbol = '${symbole}' ON CONFLICT DO NOTHING;\n`;
  }
  psql(sql);
  const liste = await api<{ items: { symbol: string; mer: number }[] }>(request, 'GET', '/portfolio/etfs');
  for (const [symbole, p] of Object.entries(PROFILS)) {
    const f = liste.items.find((e) => e.symbol === symbole);
    expect(f, `profil ${symbole}`).toBeDefined();
    expect(f!.mer).toBeCloseTo(p.rfg, 9);
  }
  preuve('catalogue', `${liste.items.length} FNB lus par GET /portfolio/etfs, dont les 4 profils synthétiques`);
});

test('1. deux comptes créés à l’écran (CELI et non enregistré), plus deux comptes par l’API', async ({ page, request }) => {
  await connecter(page);
  await page.getByTestId('menu-comptes').click();
  for (const [nom, option, cle] of [
    [NOM.celi, 'CELI', 'celi'],
    [NOM.taxable, 'Non enregistré', 'taxable'],
  ] as const) {
    await page.getByTestId('creer-compte').click();
    await page.getByTestId('compte-nom').fill(nom);
    await page.getByTestId('compte-courtier').fill('Courtier synthétique');
    await page.getByTestId('compte-type').click();
    await page.getByRole('option', { name: option, exact: true }).click();
    const reponse = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/api/v1/accounts'));
    await page.getByTestId('compte-confirmer').click();
    const r = await reponse;
    expect(r.status()).toBe(201);
    const compte = (await r.json()) as { id: string; type: string; currency: string };
    expect(compte.type).toBe(cle);
    expect(compte.currency).toBe('CAD');
    etat[cle] = compte.id;
    await expect(page.getByTestId(`compte-${compte.id}`)).toBeVisible();
  }
  // Relevés (REER, sans transaction) et compte de l'oracle : créés par l'API.
  etat.reer = (await api(request, 'POST', '/accounts', { name: NOM.reer, type: 'reer', broker: 'Courtier synthétique', currency: 'CAD' }, 201)).id;
  etat.oracle = (await api(request, 'POST', '/accounts', { name: NOM.oracle, type: 'taxable', broker: 'Courtier synthétique', currency: 'CAD' }, 201)).id;
  preuve('comptes', `celi=${etat.celi} taxable=${etat.taxable} (créés à l'écran), reer=${etat.reer} oracle=${etat.oracle} (API)`);
});

async function importerCsv(page: Page, id: string, nom: string, contenu: string): Promise<{ lues: number; creees: number; doublons: number; refusees: number; invalides: number }> {
  await page.getByTestId('menu-comptes').click();
  await page.getByTestId(`compte-${id}`).click();
  await expect(page.getByTestId('compte-titre')).toHaveText(nom);
  await page.getByRole('tab', { name: 'Import CSV' }).click();
  await page.getByTestId('csv-fichier').setInputFiles({ name: 'transactions.csv', mimeType: 'text/csv', buffer: Buffer.from(contenu, 'utf-8') });
  const reponse = page.waitForResponse((r) => r.url().includes(`/accounts/${id}/imports/csv`));
  await page.getByTestId('csv-envoyer').click();
  const r = await reponse;
  expect(r.status()).toBe(200);
  await expect(page.getByTestId('csv-rapport')).toBeVisible();
  const rapport = (await r.json()) as Record<string, number>;
  // Les compteurs affichés sont ceux du rapport de l'API.
  const afficher = async (t: string) => lire(await page.getByTestId(t).textContent());
  expect(await afficher('csv-lues')).toBe(rapport['rowsRead']);
  expect(await afficher('csv-creees')).toBe(rapport['created']);
  expect(await afficher('csv-doublons')).toBe(rapport['duplicates']);
  expect(await afficher('csv-refusees')).toBe(rapport['rejected']);
  expect(await afficher('csv-invalides')).toBe(rapport['invalid']);
  return { lues: rapport['rowsRead'], creees: rapport['created'], doublons: rapport['duplicates'], refusees: rapport['rejected'], invalides: rapport['invalid'] };
}

test('2. import CSV du CELI (achats, vente, dividende, DRIP, fractionnement, cotisations) : rapport, puis ré-import sans doublon', async ({ page }) => {
  await connecter(page);
  const extra = [
    `2025-13-40;${NOM.celi};buy;XEQT;1;1;CAD;;;;;`, // date impossible
    `2025-07-02;${NOM.celi};achat;XEQT;1;1;CAD;;;;;`, // type inconnu
    `2025-07-03;${NOM.celi};buy;XEQT;-3;1;CAD;;;;;`, // quantité négative
    `2025-07-04;${NOM.taxable};buy;XEQT;1;1;CAD;;;;;`, // autre compte que celui de la route
  ];
  const contenu = csv(CELI, NOM.celi, 'quebec', extra);
  const r1 = await importerCsv(page, etat.celi, NOM.celi, contenu);
  expect(r1).toEqual({ lues: CELI.length + 4, creees: CELI.length, doublons: 0, refusees: 1, invalides: 3 });
  preuve('import.celi', `lues=${r1.lues} créées=${r1.creees} invalides=${r1.invalides} refusées=${r1.refusees} doublons=${r1.doublons}`);
  const r2 = await importerCsv(page, etat.celi, NOM.celi, contenu);
  expect(r2.creees, 'ré-import : rien de créé').toBe(0);
  expect(r2.doublons, 'ré-import : toutes les lignes valides sont des doublons').toBe(CELI.length);
  preuve('import.celi.reimport', `créées=${r2.creees} doublons=${r2.doublons}`);
});

test('3. import CSV du compte non enregistré, puis registre en lecture seule dans l’API', async ({ page, request }) => {
  await connecter(page);
  const r = await importerCsv(page, etat.taxable, NOM.taxable, csv(TAXABLE, NOM.taxable, 'anglo'));
  expect(r).toMatchObject({ creees: TAXABLE.length, invalides: 0, refusees: 0, doublons: 0 });
  // Compte de l'oracle : un appel par compte (D421), par l'API.
  const form = { multipart: { file: { name: 'oracle.csv', mimeType: 'text/csv', buffer: Buffer.from(csv(ORACLE_RE, NOM.oracle, 'anglo')) } } };
  const rr = await request.post(`/api/v1/accounts/${etat.oracle}/imports/csv?broker=generic`, { ...form, headers: { Authorization: `Bearer ${await jeton(request)}` } });
  expect(rr.status()).toBe(200);
  expect(((await rr.json()) as { created: number }).created).toBe(ORACLE_RE.length);
  const reg = await api<{ items: { type: string; tradeDate: string }[] }>(request, 'GET', `/accounts/${etat.celi}/transactions`);
  expect(reg.items).toHaveLength(CELI.length);
  expect(reg.items.map((t) => t.tradeDate)).toEqual([...reg.items.map((t) => t.tradeDate)].sort());
  preuve('import.taxable', `créées=${r.creees}; registre du CELI = ${reg.items.length} écritures, de la plus ancienne à la plus récente`);
});

async function verifierPositions(page: Page, request: APIRequestContext, id: string, nom: string, txs: Tx[]): Promise<void> {
  const attendu = positionsAttendues(txs);
  // API : égalité exacte des chaînes décimales (quantités, coût moyen à 6 décimales, encaisse).
  const p = await api<{ cash: string; positions: { symbol: string; quantity: string; averageCost?: string }[] }>(request, 'GET', `/accounts/${id}/positions?asOf=2025-12-31`);
  expect(p.cash, `encaisse de ${nom}`).toBe(attendu.encaisse);
  expect(p.positions.map((x) => x.symbol)).toEqual(attendu.positions.map((x) => x.symbole));
  for (const a of attendu.positions) {
    const x = p.positions.find((y) => y.symbol === a.symbole)!;
    expect(x.quantity, `quantité ${a.symbole} ${nom}`).toBe(a.qte);
    expect(x.averageCost, `coût moyen ${a.symbole} ${nom}`).toBe(a.coutMoyen);
  }
  // Écran : mêmes valeurs lues dans l'onglet Positions (aujourd'hui = même registre, aucune écriture après 2025).
  await page.getByTestId('menu-comptes').click();
  await page.getByTestId(`compte-${id}`).click();
  await page.getByRole('tab', { name: 'Positions' }).click();
  await expect(page.getByTestId('positions')).toBeVisible();
  proche(lire(await page.getByTestId('pos-encaisse').textContent()), Number(attendu.encaisse), 5e-7, `encaisse affichée ${nom}`);
  for (const a of attendu.positions) {
    const ligne = page.getByTestId('positions').locator('tbody tr').filter({ hasText: a.symbole });
    proche(lire(await ligne.locator('td').nth(1).textContent()), Number(a.qte), 5e-7, `quantité affichée ${a.symbole}`);
    proche(lire(await ligne.locator('td').nth(2).textContent()), Number(a.coutMoyen), 5e-7, `coût moyen affiché ${a.symbole}`);
  }
  preuve(
    `positions.${nom.split(' ')[0].toLowerCase()}`,
    `encaisse ${attendu.encaisse} ; ` + attendu.positions.map((a) => `${a.symbole} ${a.qte} @ ${a.coutMoyen}`).join(' ; ') + ' (API exacte et écran identiques au calcul indépendant)',
  );
}

test('4. positions du CELI reconstituées à l’unité près (API exacte et écran)', async ({ page, request }) => {
  await connecter(page);
  // Calcul à la main, vérifié ici avant de comparer à l'application (garde-fou du calcul indépendant) :
  // XEQT : (100 - 30 + 0,75) x 2 + 10 = 151,5 ; ZAG : 80 - 20 = 60 ; VFV : 10,5 ; encaisse 7 873,035.
  const a = positionsAttendues(CELI);
  expect(a.encaisse).toBe('7873.035');
  expect(a.positions.map((x) => `${x.symbole}=${x.qte}`)).toEqual(['TSX:VFV=10.5', 'TSX:XEQT=151.5', 'TSX:ZAG=60']);
  await verifierPositions(page, request, etat.celi, NOM.celi, CELI);
});

test('5. positions du compte non enregistré (API exacte et écran)', async ({ page, request }) => {
  await connecter(page);
  const a = positionsAttendues(TAXABLE);
  expect(a.encaisse).toBe('1768.3'); // 5000 - 1606,95 - 1740 + 284 + 31,25 - 200
  expect(a.positions.map((x) => `${x.symbole}=${x.qte}`)).toEqual(['TSX:XEQT=100', 'TSX:ZAG=100']);
  await verifierPositions(page, request, etat.taxable, NOM.taxable, TAXABLE);
});

test('6. relevés mensuels saisis à l’écran (cas rendement-fidele-03 de l’oracle) et rendement égal à l’oracle', async ({ page, request }) => {
  const c = cas('rendement-fidele-03');
  const depart: number = c.entrees.depart;
  const ajouts: number[] = c.entrees.ajouts;
  const fins: number[] = c.entrees.fins;
  await connecter(page);
  await page.getByTestId('menu-comptes').click();
  await page.getByTestId(`compte-${etat.reer}`).click();
  await page.getByRole('tab', { name: 'Relevés de valeur' }).click();
  const saisies = [{ mois: '2025-01', valeur: depart, cotisation: 0 }, ...fins.map((v, i) => ({ mois: `2025-${String(i + 2).padStart(2, '0')}`, valeur: v, cotisation: ajouts[i] }))];
  for (const s of saisies) {
    await page.getByTestId('rel-mois').fill(s.mois);
    await page.getByTestId('rel-valeur').fill(String(s.valeur).replace('.', ','));
    await page.getByTestId('rel-cotisation').fill(s.cotisation === 0 ? '' : String(s.cotisation));
    const reponse = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/statements'));
    await page.getByTestId('rel-enregistrer').click();
    expect((await reponse).status(), `relevé ${s.mois}`).toBe(201);
  }
  const perf = await api<any>(request, 'GET', `/portfolio/performance?accounts=${etat.reer}&from=2025-01-01&to=2025-07-31`);
  const s = perf.accounts.find((x: any) => x.accountId === etat.reer);
  expect(s.method).toBe('statements');
  expect(s.irrBasis).toBe('cumulative');
  // Oracle : rendement de la période = produit des rendements mensuels - 1 ; TRI = AACJ du dernier mois (cumulé).
  proche(s.twr, c.attendu_app.rendement_periode, 5e-9, 'TWR de la période vs oracle (rendement_periode)');
  proche(s.irr, c.outil.aacj_mois[5], 5e-9, 'TRI cumulé vs oracle (AACJ mois 6)');
  const mensuel: Record<string, any> = Object.fromEntries(s.monthly.map((m: any) => [m.period, m]));
  c.outil.rendement_mensuel.forEach((attendu: number, i: number) => {
    const m = mensuel[`2025-${String(i + 2).padStart(2, '0')}`];
    proche(m.twr, attendu, 5e-9, `rendement du mois ${i + 2} vs oracle`);
    proche(m.irr, c.outil.aacj_mois[i], 5e-9, `AACJ du mois ${i + 2} vs oracle`);
  });
  preuve('rendement.releves.oracle', `cas ${c.id} : TWR ${s.twr} (oracle ${c.attendu_app.rendement_periode}) ; TRI cumulé ${s.irr} (oracle AACJ mois 6 ${c.outil.aacj_mois[5]}) ; 6 rendements mensuels et 6 AACJ égaux à 5e-9`);
  // À l'écran : TWR, TRI et base du TRI du compte.
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-rendement').click();
  await page.getByTestId(`perf-compte-${etat.reer}`).check();
  await page.getByTestId('perf-debut').fill('2025-01-01');
  await page.getByTestId('perf-fin').fill('2025-07-31');
  await page.getByTestId('perf-afficher').click();
  const serie = page.getByTestId(`serie-${etat.reer}`);
  await expect(serie).toBeVisible();
  proche(lire(await serie.getByTestId('perf-twr').textContent()), c.attendu_app.rendement_periode * 100, 0.006, 'TWR affiché (%)');
  proche(lire(await serie.getByTestId('perf-irr').textContent()), c.outil.aacj_mois[5] * 100, 0.006, 'TRI affiché (%)');
  await expect(serie.getByTestId('perf-base')).toContainText('cumulé');
  await expect(serie.getByTestId('perf-methode')).toContainText(/relev/i);
  preuve('rendement.releves.ecran', `TWR « ${compactTxt(await serie.getByTestId('perf-twr').textContent())} », TRI « ${compactTxt(await serie.getByTestId('perf-irr').textContent())} », base « ${compactTxt(await serie.getByTestId('perf-base').textContent())} »`);
});

const compactTxt = (s: string | null) => (s ?? '').replace(/\s+/g, ' ').trim();

test('7. portefeuille modèle créé à l’écran (cibles en %, versions)', async ({ page, request }) => {
  await connecter(page);
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-modeles').click();
  await page.getByTestId('nouveau-modele').click();
  await page.getByTestId('champ-nom').fill(etat.modeleNom);
  for (const [classe, valeur] of Object.entries(MODELE.cibles_pct)) await page.getByTestId(`cible-${classe}`).fill(valeur);
  await page.getByTestId('cible-encaisse').fill('0');
  for (let i = 1; i < MODELE.holdings.length; i++) await page.getByTestId('ajouter-fonds').click();
  for (const [i, h] of MODELE.holdings.entries()) {
    await page.getByTestId(`fonds-symbole-${i}`).fill(h.symbol);
    await page.getByTestId(`fonds-poids-${i}`).fill(MODELE.poids_pct[i]);
  }
  const reponse = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/api/v1/portfolio/models'));
  await page.getByTestId('enregistrer').click();
  const r = await reponse;
  expect(r.status(), await r.text()).toBe(201);
  const m = (await r.json()) as { id: string; version: number; holdings: { symbol: string; weight: number }[]; targets: { assetClass: string; target: number }[] };
  etat.modeleId = m.id;
  expect(m.version).toBe(1);
  expect(m.holdings).toEqual(MODELE.holdings.map((h) => ({ symbol: h.symbol, weight: h.weight })));
  for (const t of m.targets) expect(t.target).toBeCloseTo(MODELE.cibles[t.assetClass as keyof typeof MODELE.cibles], 9);
  const liste = await api<{ items: { id: string; name: string }[] }>(request, 'GET', '/portfolio/models');
  expect(liste.items.some((x) => x.id === m.id && x.name === etat.modeleNom)).toBe(true);
  // Cibles ne totalisant pas 100 % : refusées, jamais corrigées en silence.
  await api(request, 'POST', '/portfolio/models', { name: `${etat.modeleNom} invalide`, targets: [{ assetClass: 'equity_ca', target: 0.5 }], holdings: [{ symbol: XEQT, weight: 1 }] }, 400);
  preuve('modele', `modèle ${m.id} v${m.version} créé à l'écran ; cibles = fractions exactes ; un modèle à 50 % refusé (400)`);
});

test('8. plan d’achat avec prix saisis : parts entières égales au calcul indépendant (écran et API)', async ({ page, request }) => {
  await connecter(page);
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-achat').click();
  await page.getByTestId('achat-modele').selectOption(etat.modeleId);
  await page.getByTestId('achat-montant').fill(ACHAT.montantSaisi);
  for (const [s, p] of Object.entries(ACHAT.prix)) await page.getByTestId(`prix-${s}`).fill(p.replace('.', ','));
  const reponse = page.waitForResponse((r) => r.url().includes('/purchase-plan'));
  await page.getByTestId('achat-calculer').click();
  const r = await reponse;
  expect(r.status()).toBe(200);
  const plan = (await r.json()) as { invested: string; cashRemainder: string; orders: { side: string; symbol: string; shares: number; price: string; amount: string }[] };
  const attendu = planAchat(ACHAT.montant, MODELE.holdings.map((h) => ({ symbole: h.symbol, poids: String(h.weight), prix: ACHAT.prix[h.symbol] })));
  // Calcul à la main : XEQT 159 + 1, VFV 17, ZAG 202 + 1 ; investi 9 938,35 ; reste 61,65.
  expect(attendu.parts).toEqual({ [XEQT]: 160n, [VFV]: 17n, [ZAG]: 203n });
  expect(attendu.encaisse).toBe('61.65');
  for (const o of plan.orders) {
    expect(o.side).toBe('buy');
    expect(BigInt(o.shares), `parts ${o.symbol}`).toBe(attendu.parts[o.symbol]);
    expect(o.price).toBe(Q.de(ACHAT.prix[o.symbol]).texte(6));
    expect(o.amount).toBe(new Q(attendu.parts[o.symbol]).fois(Q.de(ACHAT.prix[o.symbol])).texte(6));
  }
  expect(plan.orders).toHaveLength(3);
  expect(plan.invested).toBe(attendu.investi);
  expect(plan.cashRemainder).toBe(attendu.encaisse);
  await expect(page.getByTestId('achat-encaisse')).toContainText(/61,65/);
  for (const [i, o] of plan.orders.entries()) await expect(page.getByTestId(`ordre-${i}`)).toContainText(String(o.shares));
  preuve('achat.ecran', `parts ${JSON.stringify(Object.fromEntries(Object.entries(attendu.parts).map(([k, v]) => [k, Number(v)])))}, investi ${plan.invested}, encaisse ${plan.cashRemainder} : égaux au calcul indépendant (D331, D361) et à l'écran`);
});

test('9. plans d’achat de l’oracle (cas ACHATS) : égal à l’oracle ou écart expliqué', async ({ request }) => {
  const symboles = [XEQT, VFV, XIC, ZAG];
  // Valeurs épinglées par backend/internal/modules/etf/engine/oracle_etf_test.go (D415) pour les écarts voulus.
  const epingles: Record<string, { parts: number[]; encaisse: string }> = {
    'achats-fidele-04': { parts: [158, 134, 187, 0], encaisse: '22.36' },
    'achats-ecart-parts-02': { parts: [155, 141, 170, 202], encaisse: '23.96' },
    'achats-ecart-parts-03': { parts: [1, 1], encaisse: '50' },
  };
  const lignes: string[] = [];
  for (const id of ['achats-fidele-02', 'achats-fidele-04', 'achats-ecart-parts-01', 'achats-ecart-parts-02', 'achats-ecart-parts-03']) {
    const c = cas(id);
    const fonds: { pct: number; prix: number }[] = c.entrees.fonds;
    const syms = symboles.slice(0, fonds.length);
    const classes = ['equity_ca', 'equity_us', 'equity_intl', 'bond_ca'];
    const m = await api<{ id: string }>(request, 'POST', '/portfolio/models', {
      name: `Oracle ${id} ${RUN}`,
      targets: fonds.map((_, i) => ({ assetClass: classes[i], target: fonds[i].pct })).filter((t) => t.target > 0),
      holdings: fonds.map((f, i) => ({ symbol: syms[i], weight: f.pct })),
    }, 201);
    const prices = Object.fromEntries(fonds.map((f, i) => [syms[i], String(f.prix)]));
    const plan = await api<{ invested: string; cashRemainder: string; orders: { symbol: string; shares: number }[] }>(request, 'POST', `/portfolio/models/${m.id}/purchase-plan`, { amount: String(c.entrees.montant), currency: 'CAD', prices }, 200);
    const parts = syms.map((s) => plan.orders.find((o) => o.symbol === s)?.shares ?? 0);
    // Référence indépendante (D331 + D361).
    const ref = planAchat(String(c.entrees.montant), fonds.map((f, i) => ({ symbole: syms[i], poids: String(f.pct), prix: String(f.prix) })));
    expect(parts, `${id} : parts vs calcul indépendant`).toEqual(syms.map((s) => Number(ref.parts[s])));
    expect(plan.cashRemainder, `${id} : encaisse vs calcul indépendant`).toBe(ref.encaisse);
    const attenduApp: number[] = c.attendu_app.parts;
    const memes = parts.every((p, i) => p === attenduApp[i]);
    let verdict: string;
    if (epingles[id]) {
      // Écart voulu D415 : le moteur redistribue le reliquat en parts entières (D361), l'oracle laisse le reste en encaisse.
      expect(parts, `${id} : valeurs épinglées par le test du moteur`).toEqual(epingles[id].parts);
      expect(plan.cashRemainder).toBe(epingles[id].encaisse);
      parts.forEach((p, i) => expect(p, `${id} : au moins les parts de l'oracle`).toBeGreaterThanOrEqual(attenduApp[i]));
      verdict = `écart expliqué (D415, redistribution du reliquat D361) : app ${JSON.stringify(parts)} encaisse ${plan.cashRemainder} contre oracle ${JSON.stringify(attenduApp)}`;
    } else {
      expect(memes, `${id} : égal à attendu_app`).toBe(true);
      expect(Number(plan.cashRemainder)).toBeCloseTo(c.attendu_app.encaisse_restante, 6);
      verdict = `égal à attendu_app ${JSON.stringify(attenduApp)} (encaisse ${plan.cashRemainder})`;
    }
    // Lien avec les parts non entières de l'outil LibreOffice : le plancher des parts de l'outil est un minimum.
    c.outil.parts.forEach((p: number, i: number) => expect(parts[i]).toBeGreaterThanOrEqual(Math.floor(p + 1e-9)));
    lignes.push(`${id} : ${verdict} ; outil LibreOffice ${JSON.stringify(c.outil.parts.map((x: number) => Math.round(x * 1000) / 1000))}`);
  }
  for (const l of lignes) preuve('achat.oracle', l);
});

test('10. rééquilibrage de l’oracle (reequilibrage-fidele-02) sur le registre : statuts égaux, ventes puis achats', async ({ request }) => {
  const c = cas('reequilibrage-fidele-02');
  const m = await api<{ id: string }>(request, 'POST', '/portfolio/models', {
    name: `Oracle RE02 ${RUN}`,
    targets: [{ assetClass: 'equity_us', target: 0.4 }, { assetClass: 'equity_ca', target: 0.2 }, { assetClass: 'bond_ca', target: 0.4 }],
    holdings: [{ symbol: VFV, weight: 0.4 }, { symbol: XIC, weight: 0.2 }, { symbol: ZAG, weight: 0.4 }],
  }, 201);
  // Sans prix saisi, le prix d'un fonds est le dernier prix d'écriture tous comptes confondus (D410) : ici VFV 100,25 $ (CELI),
  // ZAG 14,60 $ (CELI) et XIC 50 $. Poids de VFV attendu : 100 x 100,25 / (10 025 + 1 000 + 1 168).
  const sansPrix = await api<any>(request, 'POST', '/portfolio/rebalance-plans', { modelId: m.id, accountIds: [etat.oracle], currency: 'CAD' }, 200);
  proche(sansPrix.funds.find((x: any) => x.symbol === VFV).before, 10025 / 12193, 1e-9, 'poids de VFV au prix du registre');
  proche(Number(sansPrix.totalValue), 12193, 1e-6, 'valeur totale au prix du registre');
  // Pour rejouer le cas de l'oracle (tous les fonds à 50 $), les prix sont saisis dans la demande (D440).
  const prices = { [VFV]: '50', [XIC]: '50', [ZAG]: '50' };
  const plan = await api<any>(request, 'POST', '/portfolio/rebalance-plans', { modelId: m.id, accountIds: [etat.oracle], currency: 'CAD', prices }, 200);
  // SYN-US -> VFV, SYN-CA -> XIC, SYN-OBL -> ZAG (même ordre que `entrees.fonds` de l'oracle).
  const correspondance: [string, number][] = [[VFV, 0], [XIC, 1], [ZAG, 2]];
  for (const [sym, i] of correspondance) {
    const f = plan.funds.find((x: any) => x.symbol === sym);
    const o = c.outil.fonds[i];
    proche(f.before, o.pct, 1e-9, `${sym} poids avant vs oracle`);
    proche(f.target, o.cible, 1e-9, `${sym} cible vs oracle`);
    expect(f.outOfBandBefore, `${sym} statut vs oracle « ${o.statut} »`).toBe(o.statut === 'À équilibrer');
    proche(f.after, o.cible, 1e-9, `${sym} poids après = cible`);
    expect(f.outOfBandAfter).toBe(false);
  }
  proche(Number(plan.totalValue), c.outil.total, 1e-6, 'valeur totale vs oracle');
  // Ordres : ventes d'abord ; vendre 1 000 $ de VFV (20 parts à 50 $) finance l'achat de 20 parts de XIC.
  expect(plan.orders.map((o: any) => `${o.side} ${o.symbol} ${o.shares} @ ${o.price}`)).toEqual([`sell ${VFV} 20 @ 50`, `buy ${XIC} 20 @ 50`]);
  expect(plan.cashAfter).toBe('0');
  // Classes : l'outil évalue 3 classes à ±5 points, l'app 6 classes à ±20 % relatifs (D416) : actions « OK » dans l'outil, « hors bande » ici.
  const us = plan.classes.find((x: any) => x.assetClass === 'equity_us');
  const ca = plan.classes.find((x: any) => x.assetClass === 'equity_ca');
  expect(us.outOfBand && ca.outOfBand).toBe(true);
  expect(plan.classes.every((x: any) => !x.outOfBandAfter)).toBe(true);
  preuve('reeq.oracle', `cas ${c.id} : poids avant ${plan.funds.map((f: any) => f.symbol + '=' + f.before).join(' ')} = oracle ; statuts des fonds égaux ; ordres ${plan.orders.map((o: any) => o.side + ' ' + o.symbol + ' ' + o.shares).join(', ')} ; classes : écart expliqué D416 (±20 % relatifs contre ±5 points de l'outil)`);
});

test('11. rééquilibrage à l’écran avec cotisation par compte et prix saisis : cohérence indépendante', async ({ page, request }) => {
  await connecter(page);
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-reequilibrage').click();
  await page.getByTestId('reeq-modele').selectOption(etat.modeleId);
  await page.getByTestId(`reeq-inclure-${etat.celi}`).check();
  await page.getByTestId(`reeq-inclure-${etat.taxable}`).check();
  await page.getByTestId(`reeq-cotisation-${etat.celi}`).fill(REEQ.cotisationCeli);
  await page.getByTestId(`reeq-cotisation-${etat.taxable}`).fill(REEQ.cotisationTaxable);
  for (const [s, p] of Object.entries(REEQ.prix)) await page.getByTestId(`reeq-prix-${s}`).fill(p.replace('.', ','));
  const reponse = page.waitForResponse((r) => r.url().endsWith('/portfolio/rebalance-plans'));
  await page.getByTestId('reeq-calculer').click();
  const r = await reponse;
  expect(r.status(), await r.text()).toBe(200);
  const demande = JSON.parse(r.request().postData() ?? '{}');
  expect(demande.contributions).toEqual([
    { accountId: etat.celi, amount: REEQ.cotisationCeli },
    { accountId: etat.taxable, amount: REEQ.cotisationTaxable },
  ]);
  const plan = (await r.json()) as any;
  await expect(page.getByTestId('plan-reeq')).toBeVisible();

  // Référence indépendante : positions et encaisses issues du calcul indépendant du registre.
  const prix = Object.fromEntries(Object.entries(REEQ.prix).map(([s, p]) => [s, Q.de(p)]));
  const comptes = [
    { id: etat.celi, e: replier(CELI), cot: Q.de(REEQ.cotisationCeli) },
    { id: etat.taxable, e: replier(TAXABLE), cot: Q.de(REEQ.cotisationTaxable) },
  ];
  const qte: Record<string, Q> = {};
  let total = Q.ZERO;
  for (const c of comptes) {
    total = total.plus(c.e.encaisse).plus(c.cot);
    for (const [s, l] of c.e.lots) {
      qte[s] = (qte[s] ?? Q.ZERO).plus(l.qte);
      total = total.plus(l.qte.fois(prix[s]));
    }
  }
  // `totalValue` = positions + encaisses + cotisations (D371), la base des poids avant et après.
  proche(Number(plan.totalValue), total.num(), 1e-6, 'valeur totale du plan (positions, encaisses et cotisations)');
  // Poids avant (fraction du total encaisses et cotisations comprises) et statut de bande ±20 % relatif.
  for (const h of MODELE.holdings) {
    const f = plan.funds.find((x: any) => x.symbol === h.symbol);
    const avant = (qte[h.symbol] ?? Q.ZERO).fois(prix[h.symbol]).sur(total).num();
    proche(f.before, avant, 1e-9, `${h.symbol} poids avant`);
    proche(f.target, h.weight, 1e-9, `${h.symbol} cible`);
    expect(f.outOfBandBefore, `${h.symbol} statut avant`).toBe(Math.abs(avant - h.weight) > 0.2 * h.weight);
  }
  // Ordres : parts entières, ventes avant achats, encaisse de chaque compte jamais négative, poids après = ordres appliqués.
  const sellsFirst = plan.orders.map((o: any) => o.side);
  expect([...sellsFirst].sort((a, b) => (a === 'sell' ? -1 : 1) - (b === 'sell' ? -1 : 1))).toEqual(sellsFirst);
  const apres: Record<string, Q> = { ...qte };
  const cash: Record<string, Q> = Object.fromEntries(comptes.map((c) => [c.id, c.e.encaisse.plus(c.cot)]));
  for (const o of plan.orders) {
    expect(Number.isInteger(o.shares) && o.shares >= 1, `parts entières de ${o.symbol}`).toBe(true);
    expect(o.price).toBe(Q.de(REEQ.prix[o.symbol]).texte(6));
    const montant = new Q(BigInt(o.shares)).fois(Q.de(o.price));
    expect(o.amount).toBe(montant.texte(6));
    const signe = o.side === 'buy' ? 1n : -1n;
    apres[o.symbol] = (apres[o.symbol] ?? Q.ZERO).plus(new Q(signe * BigInt(o.shares)));
    cash[o.accountId] = o.side === 'buy' ? cash[o.accountId].moins(montant) : cash[o.accountId].plus(montant);
  }
  for (const c of comptes) {
    expect(cash[c.id].cmp(Q.ZERO), `encaisse finale du compte ${c.id} non négative`).toBeGreaterThanOrEqual(0);
    const rep = plan.accountCash.find((x: any) => x.accountId === c.id);
    expect(rep.cash, `encaisse après du compte ${c.id}`).toBe(cash[c.id].texte(6));
  }
  const cashTotal = Object.values(cash).reduce((a, b) => a.plus(b), Q.ZERO);
  expect(plan.cashAfter).toBe(cashTotal.texte(6));
  for (const h of MODELE.holdings) {
    const f = plan.funds.find((x: any) => x.symbol === h.symbol);
    proche(f.after, (apres[h.symbol] ?? Q.ZERO).fois(prix[h.symbol]).sur(total).num(), 1e-9, `${h.symbol} poids après = ordres appliqués`);
    // Le client n'affiche que ce que l'API calcule.
    await expect(page.getByTestId(`reeq-fonds-${h.symbol}`)).toBeVisible();
  }
  expect(plan.orders.length).toBeGreaterThan(0);
  await expect(page.getByTestId('ordres')).toBeVisible();
  preuve('reeq.ecran', `valeur de marché ${plan.totalValue}, ${plan.orders.length} ordre(s) : ${plan.orders.map((o: any) => `${o.side} ${o.symbol} ${o.shares} @ ${o.price} (compte ...${o.accountId.slice(-4)})`).join(' ; ')} ; encaisse après ${plan.cashAfter} ; poids avant, poids après, statuts et encaisses par compte égaux au calcul indépendant`);
});

test('12. exposition par transparence et écarts au modèle, RFG pondéré (API et écran)', async ({ page, request }) => {
  // Positions des deux comptes, au dernier prix d'écriture (aucun prix de marché en phase 2, D410) : XEQT 17,00, ZAG 14,60, VFV 100,25.
  const cumul: Record<string, Q> = {};
  for (const txs of [CELI, TAXABLE]) for (const [s, l] of replier(txs).lots) cumul[s] = (cumul[s] ?? Q.ZERO).plus(l.qte);
  const dernier = { [XEQT]: Q.de('17.00'), [ZAG]: Q.de('14.60'), [VFV]: Q.de('100.25') };
  const ref = exposition(
    Object.entries(cumul).map(([symbole, qte]) => ({ symbole, qte, prix: dernier[symbole as keyof typeof dernier] })),
    PROFILS,
  );
  const url = `/portfolio/exposure?accounts=${etat.celi},${etat.taxable}&modelId=${etat.modeleId}`;
  const e = await api<any>(request, 'GET', url);
  proche(Number(e.totalValue), ref.valeur.num(), 1e-6, 'valeur des positions');
  proche(e.weightedMer, ref.rfg, 1e-9, 'RFG pondéré');
  const lignes: string[] = [];
  for (const c of CLASSES) {
    const x = e.classes.find((y: any) => y.assetClass === c);
    proche(x?.actual ?? 0, ref.classes[c], 1e-9, `exposition ${c}`);
    if (x) {
      proche(x.target, MODELE.cibles[c], 1e-12, `cible ${c}`);
      proche(x.deviation, ref.classes[c] - MODELE.cibles[c], 1e-9, `écart ${c}`);
    }
    lignes.push(`${c} ${((x?.actual ?? 0) * 100).toFixed(3)} % (cible ${(MODELE.cibles[c] * 100).toFixed(1)} %)`);
  }
  // Écran.
  await connecter(page);
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-exposition').click();
  await page.getByTestId('expo-modele').selectOption(etat.modeleId);
  await page.getByTestId(`expo-compte-${etat.celi}`).check();
  await page.getByTestId(`expo-compte-${etat.taxable}`).check();
  const reponse = page.waitForResponse((r) => r.url().includes('/portfolio/exposure?'));
  await page.getByTestId('expo-afficher').click();
  expect((await reponse).status()).toBe(200);
  await expect(page.getByTestId('expo-table')).toBeVisible();
  const rfg = lire(await page.getByTestId('expo-rfg').textContent());
  proche(rfg, ref.rfg * 100, 0.006, 'RFG affiché (%)');
  const ligne = page.getByTestId('expo-equity_us');
  proche(lire(await ligne.locator('td').nth(0).textContent()), ref.classes.equity_us * 100, 0.006, 'exposition affichée equity_us (%)');
  proche(lire(await ligne.locator('td').nth(1).textContent()), MODELE.cibles.equity_us * 100, 0.006, 'cible affichée equity_us (%)');
  proche(lire(await ligne.locator('td').nth(2).textContent()), (ref.classes.equity_us - MODELE.cibles.equity_us) * 100, 0.006, 'écart affiché equity_us (points)');
  preuve('exposition', `valeur ${e.totalValue}, RFG pondéré ${(e.weightedMer * 100).toFixed(4)} % ; ${lignes.join(' ; ')} ; API et écran égaux au calcul indépendant`);
});

test('12b. diversification de l’oracle (diversification-fidele-01 et -02) par la route d’exposition', async ({ request }) => {
  for (const id of ['diversification-fidele-01', 'diversification-fidele-02']) {
    const c = cas(id);
    const positions: { pct: number; symbole: string }[] = c.entrees.positions.map((p: { symbole: string; pct: number }) => ({
      pct: p.pct,
      symbole: Object.entries(SYN).find(([, v]) => v.oracle === p.symbole)![0],
    }));
    const compte = await api<{ id: string }>(request, 'POST', '/accounts', { name: `Diversification ${id} ${RUN}`, type: 'taxable', broker: 'Synthétique', currency: 'CAD' }, 201);
    await api(request, 'POST', `/accounts/${compte.id}/transactions`, { type: 'contribution', tradeDate: '2025-01-05', amount: '10000', currency: 'CAD' }, 201);
    for (const p of positions) {
      // 10 000 $ x pct à 10 $ la part : valeur de chaque position = pct x 10 000.
      await api(request, 'POST', `/accounts/${compte.id}/transactions`, { type: 'buy', tradeDate: '2025-01-06', symbol: p.symbole, quantity: String(Math.round(p.pct * 1000)), price: '10', currency: 'CAD' }, 201);
    }
    const e = await api<any>(request, 'GET', `/portfolio/exposure?accounts=${compte.id}`);
    const part = (k: string) => e.classes.find((x: any) => x.assetClass === k)?.actual ?? 0;
    const actions = part('equity_ca') + part('equity_us') + part('equity_intl');
    const obl = part('bond_ca') + part('bond_other');
    proche(actions, c.outil.portion_actions, 1e-9, `${id} : portion d'actions`);
    proche(obl, c.outil.portion_obligations, 1e-9, `${id} : portion d'obligations`);
    ['equity_ca', 'equity_us', 'equity_intl'].forEach((k, i) => proche(part(k) / actions, c.outil.actions_pct[i], 1e-9, `${id} : ${k} / actions`));
    ['bond_ca', 'bond_other'].forEach((k, i) => proche(part(k) / obl, c.outil.obligations_pct[i], 1e-9, `${id} : ${k} / obligations`));
    preuve('diversification.oracle', `${id} : actions ${(actions * 100).toFixed(2)} % (CA/US/intl ${['equity_ca', 'equity_us', 'equity_intl'].map((k) => ((part(k) / actions) * 100).toFixed(4)).join(' / ')} %), obligations ${(obl * 100).toFixed(2)} % (CAD/autres ${['bond_ca', 'bond_other'].map((k) => ((part(k) / obl) * 100).toFixed(2)).join(' / ')} %) : égaux à l'oracle (1e-9)`);
  }
});

test('13. rendement des comptes à transactions : TWR et TRI (annualisé) égaux au calcul indépendant, consolidé et par compte', async ({ page, request }) => {
  const FIN = '2025-12-31';
  const refCeli = rendementTransactions([CELI], FIN);
  const refTax = rendementTransactions([TAXABLE], FIN);
  const refCons = rendementTransactions([CELI, TAXABLE], FIN);
  // Calcul à la main du CELI : valeur avant la cotisation du 1er juillet 10 349,16 $ (base 10 000 $), puis 12 377,16 $ au 31 décembre
  // pour 12 349,16 $ (10 349,16 + 2 000) : TWR = 1,034916 x 12 377,16 / 12 349,16 - 1.
  proche(refCeli.twr, 1.034916 * (12377.16 / 12349.16) - 1, 1e-12, 'garde-fou : TWR du CELI à la main');
  const p = await api<any>(request, 'GET', `/portfolio/performance?accounts=${etat.celi},${etat.taxable}&to=${FIN}`);
  const serie = (id?: string) => (id ? p.accounts.find((x: any) => x.accountId === id) : p.consolidated);
  const verifs: [string, any, typeof refCeli][] = [
    ['CELI', serie(etat.celi), refCeli],
    ['non enregistré', serie(etat.taxable), refTax],
    ['consolidé', serie(), refCons],
  ];
  for (const [nom, s, ref] of verifs) {
    expect(s.method, `méthode ${nom}`).toBe('transactions');
    expect(s.irrBasis, `base du TRI ${nom}`).toBe('annualized');
    proche(s.twr, ref.twr, 1e-8, `TWR ${nom}`);
    proche(s.irr, ref.tri, 1e-6, `TRI annualisé ${nom}`);
    // Une seule série : le produit des rendements mensuels donne le TWR de la période, et l'année aussi (D381).
    const produit = s.monthly.reduce((a: number, m: any) => a * (1 + (m.twr ?? 0)), 1) - 1;
    proche(produit, s.twr, 1e-9, `produit des mois ${nom}`);
    proche(s.annual.find((a: any) => a.period === '2025').twr, s.twr, 1e-9, `année 2025 ${nom}`);
    preuve(`rendement.${nom}`, `TWR ${s.twr.toFixed(8)} (référence ${ref.twr.toFixed(8)}), TRI annualisé ${s.irr.toFixed(8)} (référence ${ref.tri.toFixed(8)}, flux ${JSON.stringify(ref.flux)}), valeur finale ${ref.valeurFin}`);
  }
  // Écran : TWR, TRI et base du consolidé.
  await connecter(page);
  await page.getByTestId('menu-etf').click();
  await page.getByTestId('pf-nav-rendement').click();
  await page.getByTestId(`perf-compte-${etat.celi}`).check();
  await page.getByTestId(`perf-compte-${etat.taxable}`).check();
  await page.getByTestId('perf-fin').fill(FIN);
  await page.getByTestId('perf-afficher').click();
  const cons = page.getByTestId('perf-consolide');
  await expect(cons).toBeVisible();
  await expect(page.getByTestId(`serie-${etat.celi}`)).toBeVisible();
  proche(lire(await cons.getByTestId('perf-twr').textContent()), refCons.twr * 100, 0.006, 'TWR consolidé affiché (%)');
  proche(lire(await cons.getByTestId('perf-irr').textContent()), refCons.tri * 100, 0.006, 'TRI consolidé affiché (%)');
  await expect(cons.getByTestId('perf-base')).toContainText('annualisé');
  preuve('rendement.ecran', `consolidé : TWR « ${compactTxt(await cons.getByTestId('perf-twr').textContent())} », TRI « ${compactTxt(await cons.getByTestId('perf-irr').textContent())} », base « ${compactTxt(await cons.getByTestId('perf-base').textContent())} »`);
});

test('14. registre en insertion seule : écriture inverse, aucune modification ni suppression par l’API, positions inchangées', async ({ request }) => {
  const avant = await api<any>(request, 'GET', `/accounts/${etat.taxable}/positions?asOf=2025-12-31`);
  const frais = { type: 'fee', tradeDate: '2025-11-25', amount: '12.00', currency: 'CAD', note: 'Frais de test (annulés)' };
  const t = await api<{ id: string }>(request, 'POST', `/accounts/${etat.taxable}/transactions`, frais, 201);
  const milieu = await api<any>(request, 'GET', `/accounts/${etat.taxable}/positions?asOf=2025-12-31`);
  expect(milieu.cash, 'frais en vigueur').toBe(Q.de(avant.cash).moins(Q.de('12')).texte(6));
  await api(request, 'POST', `/accounts/${etat.taxable}/transactions`, { ...frais, reversesId: t.id }, 201);
  const fin = await api<any>(request, 'GET', `/accounts/${etat.taxable}/positions?asOf=2025-12-31`);
  expect(fin.cash, 'écriture inverse : retour à l’encaisse initiale').toBe(avant.cash);
  expect(fin.positions).toEqual(avant.positions);
  await api(request, 'POST', `/accounts/${etat.taxable}/transactions`, { ...frais, reversesId: t.id }, 409); // déjà inversée
  const entetes = { Authorization: `Bearer ${await jeton(request)}` };
  for (const m of ['PUT', 'PATCH', 'DELETE'] as const) {
    const r = await request.fetch(`/api/v1/accounts/${etat.taxable}/transactions/${t.id}`, { method: m, headers: entetes, data: m === 'DELETE' ? undefined : {} });
    expect([404, 405], `${m} sur une transaction`).toContain(r.status());
  }
  preuve('registre', `frais de 12 $ puis écriture inverse : encaisse ${avant.cash} -> ${milieu.cash} -> ${fin.cash} ; seconde inverse 409 ; PUT, PATCH et DELETE d'une transaction absents de l'API`);
});
