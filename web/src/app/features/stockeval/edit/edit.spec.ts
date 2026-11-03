import { TestBed } from '@angular/core/testing';
import { Answer, AnnualFact, Currency, Metric, Qualitative, Snapshot } from '../../../api';
import {
  annees,
  faitsModifies,
  fractionVersPourcentage,
  lignesGrille,
  normaliserDecimal,
  pourcentageVersFraction,
  validerCellule,
} from './edit.data';
import { Instantane } from './instantane';
import { Qualitatif } from './qualitatif';

function fait(fiscalYear: number, metric: Metric, value: string, source = 'xlsx'): AnnualFact {
  return { fiscalYear, metric, value, currency: Currency.CAD, source };
}

describe('conversion pourcentage / fraction', () => {
  it.each([
    ['70', '0.7'],
    ['12,5', '0.125'],
    ['12.5', '0.125'],
    ['0.5', '0.005'],
    ['-8', '-0.08'],
    ['100', '1'],
    ['1000', '10'],
    ['0', '0'],
  ])('saisir %s envoie %s', (saisie, fraction) => {
    const r = validerCellule(Metric.annual_return, saisie);
    expect(r).toEqual({ valeur: fraction });
  });

  it('revient de la fraction à la saisie sans perte', () => {
    expect(fractionVersPourcentage('0.7')).toBe('70');
    expect(fractionVersPourcentage('0.125')).toBe('12.5');
    expect(fractionVersPourcentage(pourcentageVersFraction('3.05'))).toBe('3.05');
  });
});

describe('validation des cellules', () => {
  it('refuse le non numérique et les valeurs hors bornes', () => {
    expect(validerCellule(Metric.diluted_eps, 'abc')).toEqual({ erreur: 'edit.erreur.nombre' });
    expect(validerCellule(Metric.annual_return, '-101')).toEqual({ erreur: 'edit.erreur.bornes' });
    expect(validerCellule(Metric.annual_return, '1001')).toEqual({ erreur: 'edit.erreur.bornes' });
    expect(validerCellule(Metric.dividend_per_share, '-1')).toEqual({ erreur: 'edit.erreur.bornes' });
    expect(validerCellule(Metric.diluted_shares, '0')).toEqual({ erreur: 'edit.erreur.bornes' });
  });

  it('accepte la virgule fr-CA et garde la valeur exacte', () => {
    expect(validerCellule(Metric.diluted_eps, '7,55')).toEqual({ valeur: '7.55' });
    expect(validerCellule(Metric.diluted_eps, '-0,5')).toEqual({ valeur: '-0.5' });
    expect(normaliserDecimal('1 234,50')).toBe('1234.50');
  });
});

describe('grille', () => {
  it('affiche 20 années se terminant à la dernière année connue', () => {
    const a = annees([fait(2025, Metric.diluted_eps, '7.55')], 2026);
    expect(a).toHaveLength(20);
    expect(a[19]).toBe(2025);
    expect(a[0]).toBe(2006);
  });

  it('place les conflits sur la cellule et ne signale que les cellules modifiées', () => {
    const lignes = lignesGrille(
      [fait(2024, Metric.diluted_eps, '7.10'), fait(2024, Metric.annual_return, '0.7')],
      [{ fiscalYear: 2024, metric: Metric.diluted_eps, values: [{ source: 'xlsx', value: '7.10' }, { source: 'manual', value: '7.01' }] }],
    );
    const bpa = lignes.find((l) => l.metrique === Metric.diluted_eps)!;
    const rendement = lignes.find((l) => l.metrique === Metric.annual_return)!;
    expect(bpa.cellules[2024]?.conflit).toBeDefined();
    expect(rendement.cellules[2024]?.texte).toBe('70');
    expect(faitsModifies(lignes)).toEqual([]);
    rendement.cellules[2024]!.modifiee = true;
    expect(faitsModifies(lignes)).toEqual([
      { fiscalYear: 2024, metric: Metric.annual_return, value: '0.7', currency: Currency.CAD, source: 'manual' },
    ]);
  });
});

describe('Instantane', () => {
  it('envoie des montants décimaux exacts et refuse un taux de change nul', () => {
    const fixture = TestBed.createComponent(Instantane);
    fixture.componentRef.setInput('deviseDefaut', Currency.CAD);
    fixture.detectChanges();
    const envoyes: Snapshot[] = [];
    fixture.componentInstance.enregistrerInstantane.subscribe((s) => envoyes.push(s));
    const el = fixture.nativeElement as HTMLElement;
    const saisir = (id: string, valeur: string) => {
      const input = el.querySelector<HTMLInputElement>(`[data-testid="${id}"]`)!;
      input.value = valeur;
      input.dispatchEvent(new Event('input'));
    };
    saisir('inst-price', '152,40');
    saisir('inst-fxRate', '0');
    fixture.detectChanges();
    expect(el.querySelector<HTMLButtonElement>('[data-testid="enregistrer-instantane"]')?.disabled).toBe(true);
    saisir('inst-fxRate', '1,35');
    saisir('inst-equity', '-100');
    fixture.detectChanges();
    el.querySelector<HTMLFormElement>('form')!.dispatchEvent(new Event('submit'));
    expect(envoyes).toHaveLength(1);
    expect(envoyes[0]).toMatchObject({ currency: 'CAD', price: '152.40', equity: '-100', fxRate: 1.35, source: 'manual' });
    expect(envoyes[0].annualDividend).toBeUndefined();
  });
});

describe('Qualitatif', () => {
  it('envoie oui / non / vide, les notes et le rachat', () => {
    const fixture = TestBed.createComponent(Qualitatif);
    const q: Qualitative = {
      rule1: { answer: Answer.yes, note: 'Bonne franchise' },
      rule2: { answer: Answer.no },
      rule3: { answer: Answer.unanswered },
      rule4: { answer: Answer.yes },
      manualBuyback: false,
      reviewedAt: '2026-10-30T12:05:00Z',
    };
    fixture.componentRef.setInput('qualitatif', q);
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="revue"]')?.textContent).toContain('Dernière révision');
    const envoyes: Qualitative[] = [];
    fixture.componentInstance.enregistrerQualitatif.subscribe((x) => envoyes.push(x));
    el.querySelector<HTMLFormElement>('form')!.dispatchEvent(new Event('submit'));
    expect(envoyes[0]).toEqual({
      rule1: { answer: 'yes', note: 'Bonne franchise' },
      rule2: { answer: 'no' },
      rule3: { answer: 'unanswered' },
      rule4: { answer: 'yes' },
      manualBuyback: false,
    });
  });
});
