import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { BASE_PATH } from '../../api';
import { EtfCatalog } from './etf-catalog';

const fonds = (symbol: string, mer: number) => ({
  symbol,
  name: `Fonds ${symbol}`,
  type: 'equity',
  region: 'global',
  currency: 'CAD',
  allocation: [{ assetClass: 'equity_ca', weight: 0.3 }],
  mer,
  esg: false,
  returns: [{ years: 5, annualized: 0.07 }],
  asOf: '2026-10-31',
});

describe('EtfCatalog', () => {
  function creer() {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), { provide: BASE_PATH, useValue: '' }],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(EtfCatalog);
    fixture.detectChanges();
    http
      .expectOne((r) => r.url === '/api/v1/portfolio/etfs')
      .flush({
        items: [fonds('TSX:AAA', 0.002), fonds('TSX:BBB', 0.0025), fonds('TSX:CCC', 0.003), fonds('TSX:DDD', 0.01)],
      });
    fixture.detectChanges();
    return { fixture, http, el: fixture.nativeElement as HTMLElement };
  }

  const coche = (el: HTMLElement, s: string) => el.querySelector(`[data-testid="choix-${s}"]`) as HTMLInputElement;

  it('envoie le RFG max en fraction (saisie en pourcentage, virgule déplacée sur le texte)', async () => {
    const { fixture, http, el } = creer();
    await fixture.whenStable();
    const rfg = el.querySelector('[data-testid="filtre-rfg"]') as HTMLInputElement;
    rfg.value = '0,5';
    rfg.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    (el.querySelector('[data-testid="filtrer"]') as HTMLButtonElement).click();
    const req = http.expectOne((r) => r.url === '/api/v1/portfolio/etfs');
    expect(req.request.params.get('maxMer')).toBe('0.005');
    req.flush({ items: [] });
    fixture.detectChanges();
    expect(el.querySelector('[data-testid="vide"]')).not.toBeNull();
  });

  it('exige 2 ou 3 FNB et affiche les RFG en pourcentage fr-CA', () => {
    const { fixture, el } = creer();
    const comparer = el.querySelector('[data-testid="comparer"]') as HTMLButtonElement;
    expect(comparer.disabled).toBe(true);
    for (const s of ['TSX:AAA', 'TSX:BBB']) {
      coche(el, s).click();
      fixture.detectChanges();
    }
    expect(comparer.disabled).toBe(false);
    comparer.click();
    fixture.detectChanges();
    const comp = el.querySelector('[data-testid="comparaison"]') as HTMLElement;
    expect(comp.querySelectorAll('thead th')).toHaveLength(3);
    expect(comp.querySelector('[data-row="rfg"]')?.textContent).toMatch(/0,20\s%/);
  });

  it('bloque un quatrième choix', () => {
    const { fixture, el } = creer();
    for (const s of ['TSX:AAA', 'TSX:BBB', 'TSX:CCC']) {
      coche(el, s).click();
      fixture.detectChanges();
    }
    expect(coche(el, 'TSX:DDD').disabled).toBe(true);
  });
});
