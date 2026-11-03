import { Criterion, CriterionId, CriterionStatus, Evaluation, HorizonResult, Verdict } from '../../../api';

/** Pictogrammes textuels des statuts : toujours accompagnés du libellé, jamais la couleur seule (D143, D151). */
export const ICONES_STATUT: Record<CriterionStatus, string> = {
  [CriterionStatus.passed]: '✔',
  [CriterionStatus.borderline]: '◐',
  [CriterionStatus.failed]: '✖',
  [CriterionStatus.missing]: '?',
  [CriterionStatus.insufficient]: '…',
  [CriterionStatus.exempt]: '–',
};

export const ICONES_VERDICT: Record<Verdict, string> = {
  [Verdict.candidate]: '✔',
  [Verdict.watch]: '◐',
  [Verdict.rejected]: '✖',
  [Verdict.excluded]: '⊘',
};

/** Critères dont la valeur est une fraction (ratio ou taux), affichée en pourcentage. Les autres sont des comptes ou des multiples. */
const CRITERES_FRACTION: ReadonlySet<CriterionId> = new Set([
  CriterionId._5a,
  CriterionId._6,
  CriterionId._7,
  CriterionId._8,
  CriterionId._11b,
]);
const CRITERES_COMPTE: ReadonlySet<CriterionId> = new Set([CriterionId._5b]);

const POURCENTAGE = new Intl.NumberFormat('fr-CA', { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 2 });
const MULTIPLE = new Intl.NumberFormat('fr-CA', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const COMPTE = new Intl.NumberFormat('fr-CA', { maximumFractionDigits: 0 });

/** Mise en forme seule (aucun calcul) de la valeur reçue de l'API ; null si elle est absente. */
export function formaterValeur(id: CriterionId, valeur: number | undefined): string | null {
  if (valeur === undefined || valeur === null) {
    return null;
  }
  if (CRITERES_FRACTION.has(id)) {
    return POURCENTAGE.format(valeur);
  }
  return (CRITERES_COMPTE.has(id) ? COMPTE : MULTIPLE).format(valeur);
}

export type Ecart = 'identique' | 'statut' | 'valeur';

/** Compare deux critères reçus de l'API (égalité des champs, aucun recalcul). */
export function comparer(corrige: Criterion, compat: Criterion | undefined): Ecart | null {
  if (!compat) {
    return null;
  }
  if (corrige.status !== compat.status) {
    return 'statut';
  }
  if (corrige.value !== compat.value) {
    return 'valeur';
  }
  return 'identique';
}

/** Ligne de la comparaison côte à côte, pour un critère d'un horizon présent dans les deux modes. */
export interface LigneComparaison {
  corrige: Criterion;
  compat: Criterion | undefined;
  ecart: Ecart | null;
}

export interface Comparaison {
  corrige: HorizonResult;
  compat: HorizonResult;
  lignes: LigneComparaison[];
  nbEcarts: number;
}

/** Une comparaison par horizon que les deux évaluations ont en commun (le mode compat n'en a qu'un : 20 ans). */
export function comparaisons(corrige: Evaluation, compat: Evaluation | null): Comparaison[] {
  if (!compat) {
    return [];
  }
  const resultat: Comparaison[] = [];
  for (const h of compat.horizons) {
    const c = corrige.horizons.find((x) => x.horizon === h.horizon);
    if (!c) {
      continue;
    }
    const lignes = c.criteria.map((critere) => {
      const autre = h.criteria.find((x) => x.id === critere.id);
      return { corrige: critere, compat: autre, ecart: comparer(critere, autre) };
    });
    resultat.push({ corrige: c, compat: h, lignes, nbEcarts: lignes.filter((l) => l.ecart && l.ecart !== 'identique').length });
  }
  return resultat;
}
