import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Account, AccountType, BASE_PATH, Currency } from '../../api';
import { ImportCsv, TAILLE_MAX_OCTETS } from './import-csv';
import { Positions } from './positions';
import { Registre } from './registre';
import { AccountsList } from './accounts-list';

const compte: Account = {
  id: 'a1',
  name: 'CELI',
  type: AccountType.celi,
  broker: 'Courtier',
  currency: Currency.CAD,
  createdAt: '2026-01-01T00:00:00Z',
};

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

describe('AccountsList', () => {
  it('affiche les comptes avec le type en texte, puis un état vide', () => {
    const http = config();
    const fixture = TestBed.createComponent(AccountsList);
    fixture.detectChanges();
    http.expectOne('/api/v1/accounts').flush({ items: [compte] });
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="comptes"]')?.textContent).toContain('CELI');
    expect(el.querySelector('[data-testid="comptes"]')?.textContent).toContain('Courtier');
  });

  it("affiche l'erreur problem+json sans trace", () => {
    const http = config();
    const fixture = TestBed.createComponent(AccountsList);
    fixture.detectChanges();
    http
      .expectOne('/api/v1/accounts')
      .flush({ type: 'x', title: 't', status: 500, detail: 'Service indisponible' }, { status: 500, statusText: 'x' });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[data-testid="erreur"]').textContent).toContain('Service indisponible');
  });
});

describe('Registre', () => {
  it("n'offre l'écriture inverse que pour une original non annulée, et jamais de modification", () => {
    const http = config();
    const fixture = TestBed.createComponent(Registre);
    fixture.componentRef.setInput('compte', compte);
    fixture.detectChanges();
    const base = {
      accountId: 'a1',
      tradeDate: '2026-01-10',
      currency: 'CAD',
      createdAt: '2026-01-10T00:00:00Z',
    };
    http.expectOne('/api/v1/accounts/a1/transactions').flush({
      items: [
        { ...base, id: 't1', type: 'contribution', amount: '5000' },
        {
          ...base,
          id: 't2',
          type: 'contribution',
          amount: '5000',
          reversesId: 't1',
        },
        { ...base, id: 't3', type: 'fee', amount: '10' },
      ],
    });
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="inverse-t1"]')).toBeNull();
    expect(el.querySelector('[data-testid="inverse-t2"]')).toBeNull();
    expect(el.querySelector('[data-testid="inverse-t3"]')).not.toBeNull();
    expect(el.querySelector('[data-testid="tx-t1"]')?.textContent).toContain('Annulée');
    expect(el.querySelector('[data-testid="tx-t2"]')?.textContent).toContain('Écriture inverse');
  });

  it('refuse une saisie invalide sans requête', () => {
    const http = config();
    const fixture = TestBed.createComponent(Registre);
    fixture.componentRef.setInput('compte', compte);
    fixture.detectChanges();
    http.expectOne('/api/v1/accounts/a1/transactions').flush({ items: [] });
    fixture.detectChanges();
    (fixture.nativeElement.querySelector('[data-testid="tx-ajouter"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    http.expectNone((r) => r.method === 'POST');
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Ce champ est requis');
  });
});

describe('Positions', () => {
  it('affiche « non disponible » quand le prix et la valeur sont absents', () => {
    const http = config();
    const fixture = TestBed.createComponent(Positions);
    fixture.componentRef.setInput('compte', compte);
    fixture.detectChanges();
    http.expectOne('/api/v1/accounts/a1/positions').flush({
      accountId: 'a1',
      asOf: '2026-11-10',
      currency: 'CAD',
      cash: '1200.5',
      positions: [{ symbol: 'TSX:XEQT', quantity: '100', currency: 'CAD' }],
    });
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-col="prix"]')?.textContent).toContain('non disponible');
    expect(el.querySelector('[data-col="valeur"]')?.textContent).toContain('non disponible');
    expect(el.querySelector('[data-testid="pos-total"]')?.textContent).toContain('non disponible');
    expect(el.querySelector('[data-testid="pos-encaisse"]')?.textContent).toContain('1200,5');
  });
});

describe('ImportCsv', () => {
  function choisir(fixture: { nativeElement: HTMLElement; detectChanges(): void }, f: File): void {
    const input = fixture.nativeElement.querySelector('[data-testid="csv-fichier"]') as HTMLInputElement;
    Object.defineProperty(input, 'files', {
      value: { 0: f, length: 1, item: () => f },
      configurable: true,
    });
    input.dispatchEvent(new Event('change'));
    fixture.detectChanges();
  }

  it('refuse plus de 10 Mo sans requête', () => {
    const http = config();
    const fixture = TestBed.createComponent(ImportCsv);
    fixture.componentRef.setInput('compte', compte);
    fixture.detectChanges();
    choisir(fixture, new File([new Uint8Array(TAILLE_MAX_OCTETS + 1)], 'gros.csv'));
    expect(fixture.nativeElement.querySelector('[data-testid="csv-erreur-fichier"]').textContent).toContain('10 Mo');
    expect((fixture.nativeElement.querySelector('[data-testid="csv-envoyer"]') as HTMLButtonElement).disabled).toBe(
      true,
    );
    http.expectNone(() => true);
  });

  it('envoie au compte de la route et affiche le rapport ligne par ligne', () => {
    const http = config();
    const fixture = TestBed.createComponent(ImportCsv);
    fixture.componentRef.setInput('compte', compte);
    fixture.detectChanges();
    choisir(fixture, new File(['date'], 'tx.csv'));
    (fixture.nativeElement.querySelector('[data-testid="csv-envoyer"]') as HTMLButtonElement).click();
    const req = http.expectOne((r) => r.url === '/api/v1/accounts/a1/imports/csv');
    expect(req.request.params.get('broker')).toBe('generic');
    req.flush({
      rowsRead: 3,
      valid: 1,
      invalid: 1,
      created: 1,
      duplicates: 0,
      rejected: 1,
      warnings: 0,
      entries: [
        {
          line: 3,
          level: 'error',
          kind: 'rejected',
          column: 'compte',
          message: "Le compte de la ligne n'est pas celui de la route.",
        },
      ],
    });
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="csv-refusees"]')?.textContent).toContain('1');
    const ligne = el.querySelector('[data-testid="csv-lignes"] tbody tr') as HTMLElement;
    expect(ligne.textContent).toContain('3');
    expect(ligne.textContent).toContain('Erreur');
    expect(ligne.textContent).toContain('compte');
  });
});
