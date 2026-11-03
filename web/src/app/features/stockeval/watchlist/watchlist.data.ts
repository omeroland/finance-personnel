import { Horizon, Mode, StockSummary, StocksService, Verdict } from '../../../api';
import { Observable, map } from 'rxjs';

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

/** Une ligne du tableau : celle de l'API (le plus long horizon à 10/10 est `score.perfectHorizon`, D160). */
export type Ligne = StockSummary;

/** Charge la liste de suivi : un seul appel, quel que soit le mode (D210). */
export function chargerLignes(api: StocksService, mode: Mode, horizon: Horizon): Observable<Ligne[]> {
  const choisi = mode === Mode.compat ? HORIZON_COMPAT : horizon;
  return api.listStocks(mode, choisi).pipe(map((liste) => liste.items));
}
