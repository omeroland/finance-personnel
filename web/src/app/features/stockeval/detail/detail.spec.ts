import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap } from '@angular/router';
import { BASE_PATH, Criterion, CriterionId, CriterionStatus, Evaluation, Horizon, Mode, Verdict } from '../../../api';
import { Detail } from './detail';
import { comparaisons, formaterValeur } from './detail.data';

function critere(id: CriterionId, status: CriterionStatus, value?: number): Criterion {
  return { id, status, value, label: `Critère ${id}`, strictThreshold: { description: 'seuil strict' } };
}

function evaluation(mode: Mode, horizon: Horizon, statut: CriterionStatus, indicative = false): Evaluation {
  return {
    symbol: 'TSX:CNR',
    mode,
    rulesVersion: '2026.1',
    computedAt: '2026-10-30T12:10:00Z',
    qualitativeGatePassed: true,
    horizons: [
      {
        horizon,
        indicative,
        insufficientHistory: false,
        strict: 8,
        relaxed: 10,
        outOf: 10,
        failureCount: 2,
        quality: { strict: 6, relaxed: 7, outOf: 7 },
        price: { strict: 2, relaxed: 3, outOf: 3 },
        verdict: indicative ? undefined : Verdict.watch,
        criteria: [critere(CriterionId._5a, statut, 0.0912), critere(CriterionId._11a, CriterionStatus.passed, 20.2)],
      },
    ],
  };
}

describe('formaterValeur', () => {
  it('affiche une fraction en pourcentage fr-CA, un multiple en décimal et une absence par null', () => {
    expect(formaterValeur(CriterionId._5a, 0.0912)).toBe(new Intl.NumberFormat('fr-CA', { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 2 }).format(0.0912));
    expect(formaterValeur(CriterionId._5a, 0.0912)).toMatch(/9,12\s%/);
    expect(formaterValeur(CriterionId._11a, 20.2)).toBe('20,20');
    expect(formaterValeur(CriterionId._5b, 8)).toBe('8');
    expect(formaterValeur(CriterionId._9, undefined)).toBeNull();
  });
});

describe('comparaisons', () => {
  it('relève les écarts entre corrigé et compat sans rien recalculer', () => {
    const c = comparaisons(
      evaluation(Mode.corrected, Horizon.NUMBER_20, CriterionStatus.passed),
      evaluation(Mode.compat, Horizon.NUMBER_20, CriterionStatus.failed),
    );
    expect(c).toHaveLength(1);
    expect(c[0].lignes.map((l) => l.ecart)).toEqual(['statut', 'identique']);
    expect(c[0].nbEcarts).toBe(1);
  });

  it('est vide sans évaluation compat ou sans horizon commun', () => {
    expect(comparaisons(evaluation(Mode.corrected, Horizon.NUMBER_10, CriterionStatus.passed), null)).toEqual([]);
    expect(
      comparaisons(
        evaluation(Mode.corrected, Horizon.NUMBER_10, CriterionStatus.passed),
        evaluation(Mode.compat, Horizon.NUMBER_20, CriterionStatus.passed),
      ),
    ).toEqual([]);
  });
});

describe('Detail', () => {
  it('affiche statut en texte avec icône, la mention « indicatif » et archive', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: BASE_PATH, useValue: '' },
        { provide: ActivatedRoute, useValue: { snapshot: { paramMap: convertToParamMap({ symbol: 'TSX:CNR' }) } } },
      ],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(Detail);
    fixture.detectChanges();

    const evals = http.match((r) => r.url.endsWith('/evaluation'));
    expect(evals).toHaveLength(2);
    for (const r of evals) {
      const mode = r.request.params.get('mode') as Mode;
      r.flush(evaluation(mode, Horizon.NUMBER_5, CriterionStatus.borderline, true));
    }
    http.expectOne((r) => r.url.endsWith('/evaluations') && r.method === 'GET').flush({ items: [] });
    fixture.detectChanges();

    const el: HTMLElement = fixture.nativeElement;
    expect(el.querySelector('[data-testid="indicatif"]')?.textContent).toContain('indicatif');
    const statut = el.querySelector('[data-col="statut"]')!;
    expect(statut.textContent).toContain('Limite');
    expect(statut.querySelector('[aria-hidden="true"]')?.textContent).toBe('◐');
    expect(el.querySelectorAll('[data-testid="critere"]')).toHaveLength(2);

    fixture.componentInstance['archiver']();
    const post = http.expectOne((r) => r.url.endsWith('/evaluations') && r.method === 'POST');
    expect(post.request.body).toEqual({ mode: 'corrected' });
    post.flush({
      id: 'a',
      createdAt: '2026-10-30T12:10:00Z',
      mode: Mode.corrected,
      rulesVersion: '2026.1',
      inputsFingerprint: 'x',
      evaluation: evaluation(Mode.corrected, Horizon.NUMBER_5, CriterionStatus.borderline, true),
    });
    fixture.detectChanges();
    expect(fixture.componentInstance['historique']()).toHaveLength(1);
  });
});
