import { AnnualFact, Currency, Metric, SourceConflict } from '../../../api';
import { CleTraduction } from '../../../i18n/t.pipe';

/** Nombre d'années fiscales affichées dans la grille. */
export const NB_ANNEES = 20;

/** Lignes de la grille, dans l'ordre d'affichage. */
export const METRIQUES: readonly Metric[] = [Metric.diluted_eps, Metric.dividend_per_share, Metric.annual_return, Metric.diluted_shares];

/** Le rendement annuel se saisit en pourcentage et s'envoie en fraction (CLAUDE.md : 0.70 = 70 %). */
export function estPourcentage(m: Metric): boolean {
  return m === Metric.annual_return;
}

const DECIMAL = /^-?[0-9]+(\.[0-9]+)?$/;

/** Accepte la virgule fr-CA et les espaces ; retourne une chaîne décimale exacte, ou null si la saisie est invalide. */
export function normaliserDecimal(saisie: string): string | null {
  const texte = saisie.replace(/[\s\u00a0\u202f]/g, '').replace(',', '.');
  return DECIMAL.test(texte) ? texte : null;
}

/** Déplace la virgule de `n` rangs (n = 2 ou -2) sur une chaîne décimale : opération de texte, sans nombre flottant. */
export function deplacerVirgule(decimal: string, n: 2 | -2): string {
  const negatif = decimal.startsWith('-');
  const [entier, fraction = ''] = (negatif ? decimal.slice(1) : decimal).split('.');
  let ent: string;
  let frac: string;
  if (n === 2) {
    const complete = fraction.padEnd(2, '0');
    ent = entier + complete.slice(0, 2);
    frac = complete.slice(2);
  } else {
    const complet = entier.padStart(3, '0');
    ent = complet.slice(0, -2);
    frac = complet.slice(-2) + fraction;
  }
  ent = ent.replace(/^0+(?=\d)/, '');
  frac = frac.replace(/0+$/, '');
  const texte = frac ? `${ent}.${frac}` : ent;
  return negatif && /[1-9]/.test(texte) ? `-${texte}` : texte;
}

/** Saisie en pourcentage (« 70 ») vers la fraction envoyée à l'API (« 0.7 »). */
export function pourcentageVersFraction(pourcentage: string): string {
  return deplacerVirgule(pourcentage, -2);
}

/** Fraction reçue de l'API (« 0.7 ») vers la saisie en pourcentage (« 70 »). */
export function fractionVersPourcentage(fraction: string): string {
  return deplacerVirgule(fraction, 2);
}

/** Bornes d'une saisie, dans l'unité saisie (pourcentage pour le rendement). */
export interface Bornes {
  min?: number;
  max?: number;
  /** Vrai si `min` est exclu. */
  minExclu?: boolean;
}

export const BORNES_METRIQUE: Record<Metric, Bornes> = {
  [Metric.diluted_eps]: { min: -1_000_000, max: 1_000_000 },
  [Metric.dividend_per_share]: { min: 0, max: 1_000_000 },
  [Metric.annual_return]: { min: -100, max: 1000 },
  [Metric.diluted_shares]: { min: 0, minExclu: true, max: 1_000_000_000_000 },
};

/** Valide une saisie décimale contre des bornes ; retourne la valeur normalisée ou la clé du message d'erreur. */
export function validerDecimal(saisie: string, bornes: Bornes): { valeur: string } | { erreur: CleTraduction } {
  const valeur = normaliserDecimal(saisie);
  if (valeur === null) {
    return { erreur: 'edit.erreur.nombre' };
  }
  // Comparaison de bornes seulement : la valeur envoyée reste la chaîne exacte.
  const n = Number(valeur);
  const sousMin = bornes.min !== undefined && (bornes.minExclu ? n <= bornes.min : n < bornes.min);
  if (sousMin || (bornes.max !== undefined && n > bornes.max)) {
    return { erreur: 'edit.erreur.bornes' };
  }
  return { valeur };
}

/** Valide la saisie d'une cellule de la grille et retourne la valeur à envoyer (fraction pour le rendement). */
export function validerCellule(metrique: Metric, saisie: string): { valeur: string } | { erreur: CleTraduction } {
  const resultat = validerDecimal(saisie, BORNES_METRIQUE[metrique]);
  if ('erreur' in resultat) {
    return resultat;
  }
  return { valeur: estPourcentage(metrique) ? pourcentageVersFraction(resultat.valeur) : resultat.valeur };
}

/** Texte affiché dans l'éditeur et la cellule pour une valeur reçue (virgule fr-CA ; pourcentage pour le rendement). */
export function texteDeValeur(metrique: Metric, valeur: string): string {
  const saisie = estPourcentage(metrique) ? fractionVersPourcentage(valeur) : valeur;
  return saisie.replace('.', ',');
}

/** Une cellule de la grille : une mesure pour une année. */
export interface Cellule {
  /** Texte de saisie courant (pourcentage pour le rendement). */
  texte: string;
  /** Valeur envoyée à l'API pour cette saisie (fraction pour le rendement). */
  valeur: string;
  source: string;
  devise: Currency;
  /** Vrai si modifiée dans la grille et pas encore enregistrée. */
  modifiee: boolean;
  conflit?: SourceConflict;
}

export interface LigneGrille {
  metrique: Metric;
  cellules: Record<number, Cellule | undefined>;
}

/** Les 20 années fiscales affichées : de la plus ancienne à la plus récente, jusqu'à la dernière année connue (ou l'année précédente). */
export function annees(facts: readonly AnnualFact[], anneeCourante: number): number[] {
  const derniere = Math.max(anneeCourante - 1, ...facts.map((f) => f.fiscalYear));
  return Array.from({ length: NB_ANNEES }, (_, i) => derniere - NB_ANNEES + 1 + i);
}

/** Assemble les lignes de la grille à partir de la fiche reçue (mise en forme seulement). */
export function lignesGrille(facts: readonly AnnualFact[], conflits: readonly SourceConflict[]): LigneGrille[] {
  return METRIQUES.map((metrique) => {
    const cellules: Record<number, Cellule | undefined> = {};
    for (const f of facts.filter((x) => x.metric === metrique)) {
      cellules[f.fiscalYear] = {
        texte: texteDeValeur(metrique, f.value),
        valeur: f.value,
        source: f.source,
        devise: f.currency,
        modifiee: false,
      };
    }
    for (const c of conflits.filter((x) => x.metric === metrique)) {
      const existante = cellules[c.fiscalYear];
      if (existante) {
        existante.conflit = c;
      }
    }
    return { metrique, cellules };
  });
}

/** Les valeurs modifiées dans la grille, au format du contrat (source `manual`). */
export function faitsModifies(lignes: readonly LigneGrille[]): AnnualFact[] {
  const faits: AnnualFact[] = [];
  for (const l of lignes) {
    for (const [annee, c] of Object.entries(l.cellules)) {
      if (c?.modifiee) {
        faits.push({ fiscalYear: Number(annee), metric: l.metrique, value: c.valeur, currency: c.devise, source: 'manual' });
      }
    }
  }
  return faits;
}
