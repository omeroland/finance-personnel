import { provideHttpClient } from '@angular/common/http';
import { HttpErrorResponse } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { BASE_PATH } from '../../api';
import { Dashboard } from './dashboard';
import { Exposition } from './exposition';
import { Modeles } from './modeles';
import { PlanAchat } from './plan-achat';
import { PlanReequilibrage } from './plan-reequilibrage';
import { estSansPrix, fractionVersSaisie, saisieVersFraction, symbolesCites, validerMontant } from './portfolio.data';
import { Rendement } from './rendement';

function config(): HttpTestingController {
  TestBed.configureTestingModule({
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      provideRouter([]),
      { provide: BASE_PATH, useValue: '' },
    ],
  });
  return TestBed.inject(HttpTestingController);
}

const modele = {
  id: 'm1',
  name: 'Croissance 80/20',
  version: 2,
  targets: [
    { assetClass: 'equity_ca', target: 0.3 },
    { assetClass: 'equity_us', target: 0.5 },
    { assetClass: 'bond_ca', target: 0.2 },
  ],
  cashTarget: 0,
  holdings: [
    { symbol: 'TSX:XEQT', weight: 0.8 },
    { symbol: 'TSX:ZAG', weight: 0.2 },
  ],
  createdAt: '2026-01-01T00:00:00Z',
};
const compte = {
  id: 'a1',
  name: 'CELI',
  type: 'celi',
  broker: 'B',
  currency: 'CAD',
  createdAt: '2026-01-01T00:00:00Z',
};

function el(f: { nativeElement: unknown }): HTMLElement {
  return f.nativeElement as HTMLElement;
}
function saisir(racine: HTMLElement, testid: string, valeur: string): void {
  const i = racine.querySelector(`[data-testid="${testid}"]`) as HTMLInputElement;
  i.value = valeur;
  i.dispatchEvent(new Event('input'));
}
function cliquer(racine: HTMLElement, testid: string): void {
  (racine.querySelector(`[data-testid="${testid}"]`) as HTMLElement).click();
}
async function stable(f: { detectChanges(): void; whenStable(): Promise<unknown> }): Promise<void> {
  f.detectChanges();
  await f.whenStable();
  f.detectChanges();
}

describe('portfolio.data', () => {
  it('convertit la saisie en pourcentage en fraction et inversement, sans arithmétique flottante', () => {
    expect(saisieVersFraction('27,5')).toEqual({ valeur: 0.275 });
    expect(saisieVersFraction('100')).toEqual({ valeur: 1 });
    expect('erreur' in saisieVersFraction('101')).toBe(true);
    expect('erreur' in saisieVersFraction('abc')).toBe(true);
    expect(fractionVersSaisie(0.275)).toBe('27,5');
    expect(fractionVersSaisie(undefined)).toBe('');
  });

  it('valide les montants : strictement positif sauf la cotisation', () => {
    expect('erreur' in validerMontant('0', false)).toBe(true);
    expect(validerMontant('1 000,50', false)).toEqual({ valeur: '1000.50' });
    expect(validerMontant('0', true)).toEqual({ valeur: '0' });
  });

  it('repère un 422 et les symboles cités', () => {
    const e = new HttpErrorResponse({
      status: 422,
      error: { detail: 'prix manquant pour TSX:XEQT, NYSE:BRK.B' },
    });
    expect(estSansPrix(e)).toBe(true);
    expect(symbolesCites(e)).toEqual(['TSX:XEQT', 'NYSE:BRK.B']);
    expect(estSansPrix(new HttpErrorResponse({ status: 500 }))).toBe(false);
  });
});

describe('Modeles', () => {
  it('liste le modèle, puis crée la version 1 avec des fractions envoyées par déplacement de la virgule', async () => {
    const http = config();
    const f = TestBed.createComponent(Modeles);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/portfolio/etfs').flush({ items: [] });
    await stable(f);
    expect(el(f).querySelector('[data-testid="modeles"]')?.textContent).toContain('Croissance 80/20');
    expect(el(f).querySelector('[data-testid="modeles"]')?.textContent).toContain('80,00');

    cliquer(el(f), 'nouveau-modele');
    await stable(f);
    saisir(el(f), 'champ-nom', 'Mon modèle');
    for (const [c, v] of [
      ['equity_ca', '27,5'],
      ['equity_us', '43,5'],
      ['equity_intl', '29'],
      ['bond_ca', '0'],
      ['bond_other', '0'],
      ['other_assets', '0'],
    ]) {
      saisir(el(f), `cible-${c}`, v);
    }
    saisir(el(f), 'fonds-symbole-0', 'tsx:xeqt');
    saisir(el(f), 'fonds-poids-0', '100');
    await stable(f);
    cliquer(el(f), 'enregistrer');
    const r = http.expectOne((x) => x.method === 'POST' && x.url === '/api/v1/portfolio/models');
    expect(r.request.body.targets[0]).toEqual({
      assetClass: 'equity_ca',
      target: 0.275,
    });
    expect(r.request.body.holdings).toEqual([{ symbol: 'TSX:XEQT', weight: 1 }]);
    expect(r.request.body.cashTarget).toBeUndefined();
    r.flush({ ...modele, version: 1 }, { status: 201, statusText: 'Created' });
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    await stable(f);
    expect(el(f).querySelector('[data-testid="modele-confirmation"]')?.textContent).toContain('version 1');
  });

  it('refuse un total que le serveur rejette en affichant son message, et prépare une nouvelle version par PUT', async () => {
    const http = config();
    const f = TestBed.createComponent(Modeles);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/portfolio/etfs').flush({ items: [] });
    await stable(f);
    cliquer(el(f), 'nouvelle-version-m1');
    await stable(f);
    expect((el(f).querySelector('[data-testid="champ-nom"]') as HTMLInputElement).readOnly).toBe(true);
    expect((el(f).querySelector('[data-testid="cible-equity_ca"]') as HTMLInputElement).value).toBe('30');
    cliquer(el(f), 'enregistrer');
    const r = http.expectOne((x) => x.method === 'PUT' && x.url === '/api/v1/portfolio/models/m1');
    r.flush(
      {
        type: 'x',
        title: 't',
        status: 400,
        detail: 'Les cibles doivent totaliser 100 %.',
      },
      { status: 400, statusText: 'x' },
    );
    await stable(f);
    expect(el(f).querySelector('[data-testid="form-erreur"]')?.textContent).toContain('totaliser 100 %');
  });

  it('affiche un message clair quand il n’y a aucun modèle', async () => {
    const http = config();
    const f = TestBed.createComponent(Modeles);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [] });
    http.expectOne('/api/v1/portfolio/etfs').flush({ items: [] });
    await stable(f);
    expect(el(f).querySelector('[data-testid="modeles-vide"]')).not.toBeNull();
  });
});

describe('PlanAchat', () => {
  it('envoie le montant et les prix saisis, puis affiche les ordres tels que reçus', async () => {
    const http = config();
    const f = TestBed.createComponent(PlanAchat);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    await stable(f);
    saisir(el(f), 'achat-montant', '10 000,00');
    saisir(el(f), 'prix-TSX:XEQT', '35,10');
    await stable(f);
    cliquer(el(f), 'achat-calculer');
    const r = http.expectOne('/api/v1/portfolio/models/m1/purchase-plan');
    expect(r.request.body).toEqual({
      amount: '10000.00',
      currency: 'CAD',
      prices: { 'TSX:XEQT': '35.10' },
    });
    r.flush({
      modelId: 'm1',
      modelVersion: 2,
      currency: 'CAD',
      amount: '10000.00',
      invested: '9990.50',
      cashRemainder: '9.50',
      orders: [
        {
          side: 'buy',
          symbol: 'TSX:XEQT',
          shares: 227,
          price: '35.10',
          amount: '7967.70',
        },
      ],
    });
    await stable(f);
    expect(el(f).querySelector('[data-testid="achat-encaisse"]')?.textContent).toContain('9,50 CAD');
    expect(el(f).querySelector('[data-testid="ordre-0"]')?.textContent).toContain('227');
    expect(el(f).querySelector('[data-testid="ordre-0"]')?.textContent).toContain('Achat');
  });

  it('affiche clairement les fonds sans prix après un 422', async () => {
    const http = config();
    const f = TestBed.createComponent(PlanAchat);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    await stable(f);
    saisir(el(f), 'achat-montant', '500');
    await stable(f);
    cliquer(el(f), 'achat-calculer');
    http.expectOne('/api/v1/portfolio/models/m1/purchase-plan').flush(
      {
        type: 'x',
        title: 't',
        status: 422,
        detail: 'prix manquant pour TSX:XEQT, TSX:ZAG',
      },
      { status: 422, statusText: 'x' },
    );
    await stable(f);
    const alerte = el(f).querySelector('[data-testid="achat-sans-prix"]');
    expect(alerte?.textContent).toContain('Fonds sans prix');
    expect(el(f).querySelector('[data-testid="achat-fonds-sans-prix"]')?.textContent).toContain('TSX:ZAG');
    expect(el(f).textContent).toContain('prix requis');
  });

  it('refuse un montant invalide sans requête', async () => {
    const http = config();
    const f = TestBed.createComponent(PlanAchat);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    await stable(f);
    saisir(el(f), 'achat-montant', '-5');
    await stable(f);
    cliquer(el(f), 'achat-calculer');
    http.expectNone((r) => r.method === 'POST');
    await stable(f);
    expect(el(f).querySelector('[data-testid="achat-erreur-saisie"]')).not.toBeNull();
  });
});

describe('PlanReequilibrage', () => {
  it('envoie une cotisation par compte et affiche classes hors bande, ordres et ordres écartés', async () => {
    const http = config();
    const f = TestBed.createComponent(PlanReequilibrage);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    await stable(f);
    saisir(el(f), 'reeq-cotisation-a1', '1 500');
    await stable(f);
    cliquer(el(f), 'reeq-calculer');
    const r = http.expectOne('/api/v1/portfolio/rebalance-plans');
    expect(r.request.body).toEqual({
      modelId: 'm1',
      currency: 'CAD',
      contributions: [{ accountId: 'a1', amount: '1500' }],
    });
    r.flush({
      modelId: 'm1',
      modelVersion: 2,
      currency: 'CAD',
      totalValue: '50000.00',
      classes: [
        { assetClass: 'equity_ca', actual: 0.4, target: 0.3, outOfBand: true },
        { assetClass: 'bond_ca', actual: 0.2, target: 0.2, outOfBand: false },
      ],
      orders: [
        {
          side: 'sell',
          symbol: 'TSX:XIC',
          accountId: 'a1',
          shares: 10,
          price: '40.00',
          amount: '400.00',
        },
        {
          side: 'buy',
          symbol: 'TSX:ZAG',
          accountId: 'a1',
          shares: 20,
          price: '15.00',
          amount: '300.00',
        },
      ],
      skipped: [
        {
          side: 'buy',
          symbol: 'TSX:XEQT',
          amount: '5.00',
          reason: 'below_minimum',
        },
      ],
      cashAfter: '100.00',
    });
    await stable(f);
    const classes = el(f).querySelector('[data-testid="reeq-classes"]')?.textContent ?? '';
    expect(classes).toContain('Hors bande');
    expect(classes).toContain('Dans la bande');
    expect(el(f).querySelector('[data-testid="ordre-0"]')?.textContent).toContain('Vente');
    expect(el(f).querySelector('[data-testid="ordre-1"]')?.textContent).toContain('CELI');
    expect(el(f).querySelector('[data-testid="ecartes"]')?.textContent).toContain('sous le montant minimum');
    expect(el(f).querySelector('[data-testid="reeq-encaisse"]')?.textContent).toContain('100,00');
  });

  it('dit que tout est dans les bandes quand il n’y a aucun ordre', async () => {
    const http = config();
    const f = TestBed.createComponent(PlanReequilibrage);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    await stable(f);
    cliquer(el(f), 'reeq-calculer');
    http.expectOne('/api/v1/portfolio/rebalance-plans').flush({
      modelId: 'm1',
      modelVersion: 2,
      currency: 'CAD',
      classes: [],
      orders: [],
      skipped: [],
      cashAfter: '0',
    });
    await stable(f);
    expect(el(f).querySelector('[data-testid="ordres-vide"]')).not.toBeNull();
    expect(el(f).querySelector('[data-testid="reeq-valeur"]')?.textContent).toContain('non disponible');
  });
});

describe('Exposition', () => {
  it('affiche écarts signés et RFG pondéré, et un message clair sans position', async () => {
    const http = config();
    const f = TestBed.createComponent(Exposition);
    f.detectChanges();
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/exposure')
      .flush({
        asOf: '2026-01-31',
        currency: 'CAD',
        totalValue: '1000.00',
        weightedMer: 0.002,
        classes: [
          {
            assetClass: 'equity_ca',
            actual: 0.31,
            target: 0.3,
            deviation: 0.01,
          },
        ],
      });
    await stable(f);
    expect(el(f).querySelector('[data-testid="expo-rfg"]')?.textContent).toContain('0,20');
    expect(el(f).querySelector('[data-testid="expo-equity_ca"]')?.textContent).toContain('+1,00');

    cliquer(el(f), 'expo-afficher');
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/exposure')
      .flush({ asOf: '2026-01-31', currency: 'CAD', classes: [] });
    await stable(f);
    expect(el(f).querySelector('[data-testid="expo-vide"]')).not.toBeNull();
  });
});

describe('Rendement', () => {
  it('explique la base du TRI et affiche les points mensuels puis annuels de la même série', async () => {
    const http = config();
    const f = TestBed.createComponent(Rendement);
    f.detectChanges();
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/performance')
      .flush({
        from: '2025-01-01',
        to: '2025-12-31',
        consolidated: {
          method: 'transactions',
          twr: 0.08,
          irr: 0.075,
          irrBasis: 'annualized',
          monthly: [{ period: '2025-01', twr: 0.01 }],
          annual: [{ period: '2025', twr: 0.08, irr: 0.075 }],
        },
        accounts: [
          {
            accountId: 'a1',
            method: 'statements',
            twr: 0.05,
            irr: 0.04,
            irrBasis: 'cumulative',
            monthly: [],
            annual: [],
          },
        ],
      });
    await stable(f);
    const consolide = el(f).querySelector('[data-testid="perf-consolide"]') as HTMLElement;
    expect(consolide.querySelector('[data-testid="perf-base"]')?.textContent).toContain('annualisé');
    expect(consolide.querySelector('[data-testid="perf-table"]')?.textContent).toContain('2025-01');
    const compteSerie = el(f).querySelector('[data-testid="serie-a1"]') as HTMLElement;
    expect(compteSerie.querySelector('[data-testid="perf-base"]')?.textContent).toContain('cumulé');
    expect(compteSerie.querySelector('[data-testid="perf-vide"]')).not.toBeNull();
    expect(compteSerie.textContent).toContain('CELI');

    const select = el(f).querySelector('[data-testid="perf-granularite"]') as HTMLSelectElement;
    select.value = 'annual';
    select.dispatchEvent(new Event('change'));
    await stable(f);
    expect(
      (el(f).querySelector('[data-testid="perf-consolide"] [data-testid="perf-table"]') as HTMLElement).textContent,
    ).toContain('2025');
    expect(
      (el(f).querySelector('[data-testid="perf-consolide"] [data-testid="perf-table"]') as HTMLElement).textContent,
    ).not.toContain('2025-01');
  });
});

describe('Dashboard', () => {
  it('regroupe positions, exposition vs modèle et rendement ; une section en erreur n’efface pas les autres', async () => {
    const http = config();
    const f = TestBed.createComponent(Dashboard);
    f.detectChanges();
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    http.expectOne('/api/v1/portfolio/models').flush({ items: [modele] });
    http.expectOne('/api/v1/accounts/a1/positions').flush({
      accountId: 'a1',
      asOf: '2026-01-31',
      currency: 'CAD',
      cash: '100.00',
      totalValue: '1100.00',
      positions: [
        {
          symbol: 'TSX:XEQT',
          quantity: '10',
          marketValue: '1000.00',
          currency: 'CAD',
        },
      ],
    });
    const expo = http.expectOne((r) => r.url === '/api/v1/portfolio/exposure');
    expect(expo.request.params.get('modelId')).toBe('m1');
    expo.flush({
      asOf: '2026-01-31',
      currency: 'CAD',
      modelId: 'm1',
      classes: [{ assetClass: 'equity_ca', actual: 0.31, target: 0.3, deviation: 0.01 }],
    });
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/performance')
      .flush({ type: 'x', title: 't', status: 500, detail: 'Service indisponible' }, { status: 500, statusText: 'x' });
    await stable(f);
    expect(el(f).querySelector('[data-testid="tableau-compte-a1"]')?.textContent).toContain('1100,00');
    expect(el(f).querySelector('[data-testid="expo-equity_ca"]')?.textContent).toContain('+1,00');
    expect(el(f).querySelector('[data-testid="tableau-rendement-erreur"]')?.textContent).toContain(
      'Service indisponible',
    );
  });

  it('affiche un message clair sans compte', async () => {
    const http = config();
    const f = TestBed.createComponent(Dashboard);
    f.detectChanges();
    http.expectOne('/api/v1/accounts').flush({ items: [] });
    http.expectOne('/api/v1/portfolio/models').flush({ items: [] });
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/exposure')
      .flush({ asOf: '2026-01-31', currency: 'CAD', classes: [] });
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/performance')
      .flush({
        from: '2026-01-01',
        to: '2026-01-31',
        consolidated: { monthly: [], annual: [] },
        accounts: [],
      });
    await stable(f);
    expect(el(f).querySelector('[data-testid="tableau-vide"]')).not.toBeNull();
    expect(el(f).querySelector('[data-testid="expo-vide"]')).not.toBeNull();
  });
});
