import { HttpErrorResponse } from '@angular/common/http';
import { AssetClass, OrderSide, SkippedOrderReasonEnum } from '../../api';
import { CleTraduction, traduire } from '../../i18n/t.pipe';
import { fractionVersPourcentage, pourcentageVersFraction, validerDecimal } from '../stockeval/edit/edit.data';

/** Les six classes d'actifs, dans l'ordre du contrat. */
export const CLASSES: readonly AssetClass[] = Object.values(AssetClass);

/** Nombre maximal de fonds d'un modèle (D313). */
export const MAX_FONDS = 20;

const FORMAT_ECART = new Intl.NumberFormat('fr-CA', {
  style: 'percent',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
  signDisplay: 'exceptZero',
});

/** Écart reçu de l'API (fraction) affiché en points de pourcentage, avec son signe écrit (« +1,50 % »). */
export function afficherEcart(fraction: number): string {
  return FORMAT_ECART.format(fraction);
}

export function libelleClasse(c: AssetClass): string {
  return traduire(`pf.classe.${c}`);
}

export function libelleSens(s: OrderSide): string {
  return traduire(`pf.sens.${s}`);
}

export function libelleMotif(m: SkippedOrderReasonEnum): string {
  return traduire(`pf.motif.${m}`);
}

/** Pictogramme du sens d'un ordre (le libellé texte accompagne toujours le pictogramme). */
export function iconeSens(s: OrderSide): string {
  return s === OrderSide.sell ? '▼' : '▲';
}

/** Saisie en pourcentage (« 27,5 ») vers la fraction envoyée (0.275), par déplacement de la virgule sur le texte (D214). */
export function saisieVersFraction(
  saisie: string,
  bornes = { min: 0, max: 100 },
): { valeur: number } | { erreur: CleTraduction } {
  const r = validerDecimal(saisie, bornes);
  if ('erreur' in r) {
    return r;
  }
  return { valeur: Number(pourcentageVersFraction(r.valeur)) };
}

/** Fraction reçue (0.275) vers le texte de saisie en pourcentage (« 27,5 »), sans arithmétique flottante. */
export function fractionVersSaisie(fraction: number | undefined): string {
  if (fraction === undefined) {
    return '';
  }
  const texte = fraction.toString();
  if (!/^-?[0-9]+(\.[0-9]+)?$/.test(texte)) {
    return '';
  }
  return fractionVersPourcentage(texte).replace('.', ',');
}

/** Prix ou montant saisi : décimal strictement positif (prix, montant à investir) ou nul (cotisation) ; chaîne exacte envoyée. */
export function validerMontant(saisie: string, accepteZero: boolean): { valeur: string } | { erreur: CleTraduction } {
  return validerDecimal(saisie, accepteZero ? { min: 0 } : { min: 0, minExclu: true });
}

/** Vrai pour la réponse 422 « prix manquant » des plans (D410, D440). */
export function estSansPrix(e: unknown): boolean {
  return e instanceof HttpErrorResponse && e.status === 422;
}

const SYMBOLE = /[A-Z0-9]+:[A-Z0-9][A-Z0-9.-]*/g;

/** Symboles canoniques cités par le message d'un 422 : sert seulement à repérer les champs de prix à remplir. */
export function symbolesCites(e: unknown): string[] {
  if (!(e instanceof HttpErrorResponse)) {
    return [];
  }
  const detail = (e.error as { detail?: unknown } | null)?.detail;
  return typeof detail === 'string' ? [...new Set(detail.match(SYMBOLE) ?? [])] : [];
}
