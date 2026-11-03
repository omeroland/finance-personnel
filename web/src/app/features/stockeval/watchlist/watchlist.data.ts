import { Horizon, Mode, ScoreSummary, StockList, StockSummary, StocksService, Verdict } from '../../../api';
import { Observable, forkJoin, map } from 'rxjs';

/** Horizons proposés par l'API (D9). */
export const HORIZONS: readonly Horizon[] = [Horizon.NUMBER_5, Horizon.NUMBER_10, Horizon.NUMBER_15, Horizon.NUMBER_20];

/** Le mode compat ne reproduit la feuille que sur l'horizon de 20 ans (contrat). */
export const HORIZON_COMPAT = Horizon.NUMBER_20;

/** Ordre de tri des verdicts : du plus favorable au moins favorable ; absent (indicatif) à la fin. */
export const ORDRE_VERDICT: Record<Verdict, number> = {
  [Verdict.candidate]: 0,
  [Verdict.watch]: 1,
  [Verdict.rejected]: 2,
  [Verdict.excluded]: 3,
};

/** Une ligne du tableau : le score de l'horizon choisi et l'horizon le plus long à 10/10, tous deux fournis par l'API. */
export interface Ligne extends StockSummary {
  /** Plus long horizon dont le score strict atteint le total, parmi les réponses de l'API ; null si aucun. */
  plusLongHorizon10sur10: Horizon | null;
}

/** Horizons à interroger pour un mode donné. */
export function horizonsAInterroger(mode: Mode): readonly Horizon[] {
  return mode === Mode.compat ? [HORIZON_COMPAT] : HORIZONS;
}

/** Sélection (aucun calcul) : la ligne de l'horizon choisi, enrichie du plus long horizon dont l'API déclare le score parfait. */
export function assembler(reponses: ReadonlyMap<Horizon, StockList>, horizonChoisi: Horizon): Ligne[] {
  const principal = reponses.get(horizonChoisi);
  if (!principal) {
    return [];
  }
  return principal.items.map((ligne) => {
    let plusLong: Horizon | null = null;
    for (const liste of reponses.values()) {
      const score: ScoreSummary | undefined = liste.items.find((i) => i.stock.symbol === ligne.stock.symbol)?.score;
      if (score && !score.insufficientHistory && score.outOf > 0 && score.strict === score.outOf) {
        if (plusLong === null || score.horizon > plusLong) {
          plusLong = score.horizon;
        }
      }
    }
    return { ...ligne, plusLongHorizon10sur10: plusLong };
  });
}

/** Charge la liste de suivi : un appel par horizon (un seul en mode compat). */
export function chargerLignes(api: StocksService, mode: Mode, horizon: Horizon): Observable<Ligne[]> {
  const horizons = horizonsAInterroger(mode);
  const choisi = mode === Mode.compat ? HORIZON_COMPAT : horizon;
  return forkJoin(horizons.map((h) => api.listStocks(mode, h))).pipe(
    map((listes) => assembler(new Map(horizons.map((h, i) => [h, listes[i]])), choisi)),
  );
}
