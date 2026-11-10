import { HttpErrorResponse } from '@angular/common/http';
import { Problem } from '../api';
import { CleTraduction, traduire } from '../i18n/t.pipe';

/**
 * Message affiché pour une erreur de l'API (RFC 9457) : erreurs par champ, sinon `detail`, sinon texte de repli.
 * Le serveur n'envoie aucune trace interne ; le client n'affiche rien d'autre que ces champs.
 */
export function messageProbleme(e: unknown, repli: CleTraduction): string {
  if (e instanceof HttpErrorResponse) {
    if (e.status === 0) {
      return traduire('erreur.reseau');
    }
    const p = e.error as Partial<Problem> | null;
    const champs = p?.errors?.map((x) => `${x.field} : ${x.message}`).join(' ; ');
    if (champs) {
      return champs;
    }
    if (typeof p?.detail === 'string' && p.detail.length > 0) {
      return p.detail;
    }
  }
  return traduire(repli);
}

/** Affichage d'un décimal exact reçu en chaîne : virgule fr-CA, aucun nombre flottant. */
export function afficherDecimal(valeur: string | undefined): string {
  return valeur === undefined ? '' : valeur.replace('.', ',');
}

const POURCENT = new Intl.NumberFormat('fr-CA', {
  style: 'percent',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/** Fraction reçue de l'API (0.002) affichée en pourcentage (« 0,20 % »). */
export function afficherPourcentage(fraction: number): string {
  return POURCENT.format(fraction);
}
