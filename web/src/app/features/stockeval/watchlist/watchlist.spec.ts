import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { BASE_PATH, Currency, Exchange, Horizon, Mode, StockList, Verdict } from '../../../api';
import { Watchlist } from './watchlist';
import { assembler } from './watchlist.data';

function liste(symbole: string, horizon: Horizon, strict: number, verdict?: Verdict, bourse = Exchange.TSX): StockList {
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
        },
      },
    ],
  };
}

describe('assembler', () => {
  it("retient le plus long horizon dont l'API déclare le score parfait", () => {
    const reponses = new Map<Horizon, StockList>([
      [Horizon.NUMBER_5, liste('TSX:AAA', Horizon.NUMBER_5, 10)],
      [Horizon.NUMBER_10, liste('TSX:AAA', Horizon.NUMBER_10, 10, Verdict.candidate)],
      [Horizon.NUMBER_15, liste('TSX:AAA', Horizon.NUMBER_15, 9, Verdict.watch)],
      [Horizon.NUMBER_20, liste('TSX:AAA', Horizon.NUMBER_20, 8, Verdict.rejected)],
    ]);
    const lignes = assembler(reponses, Horizon.NUMBER_15);
    expect(lignes).toHaveLength(1);
    expect(lignes[0].score.horizon).toBe(Horizon.NUMBER_15);
    expect(lignes[0].plusLongHorizon10sur10).toBe(Horizon.NUMBER_10);
  });

  it('donne null quand aucun horizon n’est parfait', () => {
    const reponses = new Map<Horizon, StockList>([[Horizon.NUMBER_20, liste('TSX:AAA', Horizon.NUMBER_20, 8, Verdict.rejected)]]);
    expect(assembler(reponses, Horizon.NUMBER_20)[0].plusLongHorizon10sur10).toBeNull();
  });
});

describe('Watchlist', () => {
  it('affiche le verdict en texte avec son icône, et seulement les lignes du filtre', async () => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), { provide: BASE_PATH, useValue: '' }],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(Watchlist);
    fixture.detectChanges();

    const reqs = http.match((r) => r.url.endsWith('/api/v1/stocks'));
    expect(reqs).toHaveLength(4);
    for (const r of reqs) {
      const h = Number(r.request.params.get('horizon')) as Horizon;
      r.flush(liste('TSX:AAA', h, 10, Verdict.candidate));
    }
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
