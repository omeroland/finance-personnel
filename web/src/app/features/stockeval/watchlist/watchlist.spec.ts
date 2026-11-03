import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { BASE_PATH, Currency, Exchange, Horizon, Mode, StockList, Verdict } from '../../../api';
import { Watchlist } from './watchlist';

function liste(symbole: string, horizon: Horizon, strict: number, verdict?: Verdict, perfectHorizon?: Horizon, bourse = Exchange.TSX): StockList {
  return {
    items: [
      {
        stock: {
          symbol: symbole,
          name: `Compagnie ${symbole}`,
          exchange: bourse,
          quoteCurrency: Currency.CAD,
          reportingCurrency: Currency.CAD,
          isFinancial: false,
          createdAt: '2026-03-02T14:05:00Z',
        },
        score: {
          horizon,
          mode: Mode.corrected,
          strict,
          relaxed: 10,
          outOf: 10,
          failureCount: 10 - strict,
          quality: { strict: 6, relaxed: 7, outOf: 7 },
          price: { strict: 2, relaxed: 3, outOf: 3 },
          verdict,
          perfectHorizon,
        },
      },
    ],
  };
}

describe('Watchlist', () => {
  it('affiche le verdict en texte avec son icône, et seulement les lignes du filtre', async () => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideRouter([]), provideHttpClientTesting(), { provide: BASE_PATH, useValue: '' }],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(Watchlist);
    fixture.detectChanges();

    const reqs = http.match((r) => r.url.endsWith('/api/v1/stocks'));
    expect(reqs).toHaveLength(1);
    expect(reqs[0].request.params.get('horizon')).toBe('10');
    reqs[0].flush(liste('TSX:AAA', Horizon.NUMBER_10, 10, Verdict.candidate, Horizon.NUMBER_20));
    await fixture.whenStable();
    fixture.detectChanges();

    const el = fixture.nativeElement as HTMLElement;
    const cellule = el.querySelector('[data-col="verdict"]');
    expect(cellule?.textContent).toContain('Candidate');
    expect(cellule?.querySelector('[aria-hidden="true"]')?.textContent).toBe('✔');
    expect(el.querySelector('[data-col="horizon"]')?.textContent).toContain('20 ans');
    expect(el.querySelector('[data-col="strict"]')?.textContent).toContain('10/10');
    http.verify();
  });
});

describe('Watchlist sans horizon parfait', () => {
  it('affiche « Aucun » quand perfectHorizon est absent', async () => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideRouter([]), provideHttpClientTesting(), { provide: BASE_PATH, useValue: '' }],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(Watchlist);
    fixture.detectChanges();
    http.expectOne((r) => r.url.endsWith('/api/v1/stocks')).flush(liste('TSX:AAA', Horizon.NUMBER_10, 8, Verdict.watch));
    await fixture.whenStable();
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).querySelector('[data-col="horizon"]')?.textContent).toContain('Aucun');
    http.verify();
  });
});
