// P2-14 : calcul de référence INDÉPENDANT du backend (aucun import du code de l'application).
// Il applique les règles écrites dans docs/decisions.md (D331, D361, D409, D411, D413) avec une arithmétique
// exacte (fractions de BigInt) pour l'argent et des flottants seulement pour les ratios. Données synthétiques.

function pgcd(a: bigint, b: bigint): bigint {
  a = a < 0n ? -a : a;
  b = b < 0n ? -b : b;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

/** Fraction exacte. */
export class Q {
  readonly n: bigint;
  readonly d: bigint;
  constructor(n: bigint, d: bigint = 1n) {
    if (d === 0n) throw new Error('division par zéro');
    if (d < 0n) [n, d] = [-n, -d];
    const g = pgcd(n, d) || 1n;
    this.n = n / g;
    this.d = d / g;
  }
  /** Décimal en chaîne (« 12.5 », « -3 »), sans notation scientifique. */
  static de(s: string | number): Q {
    const t = String(s).trim();
    const m = /^(-?)(\d+)(?:\.(\d+))?$/.exec(t);
    if (!m) throw new Error(`décimal invalide : ${t}`);
    const frac = m[3] ?? '';
    const n = BigInt(m[2] + frac) * (m[1] === '-' ? -1n : 1n);
    return new Q(n, 10n ** BigInt(frac.length));
  }
  static readonly ZERO = new Q(0n);
  plus(o: Q): Q {
    return new Q(this.n * o.d + o.n * this.d, this.d * o.d);
  }
  moins(o: Q): Q {
    return new Q(this.n * o.d - o.n * this.d, this.d * o.d);
  }
  fois(o: Q): Q {
    return new Q(this.n * o.n, this.d * o.d);
  }
  sur(o: Q): Q {
    return new Q(this.n * o.d, this.d * o.n);
  }
  cmp(o: Q): number {
    const x = this.n * o.d - o.n * this.d;
    return x < 0n ? -1 : x > 0n ? 1 : 0;
  }
  /** Plus grand entier inférieur ou égal. */
  plancher(): bigint {
    const q = this.n / this.d;
    return this.n % this.d !== 0n && this.n < 0n ? q - 1n : q;
  }
  num(): number {
    return Number(this.n) / Number(this.d);
  }
  /** Arrondi à `places` décimales, demi vers l'extérieur de zéro, rendu sans zéros finaux (comme l'API). */
  texte(places = 6): string {
    const echelle = 10n ** BigInt(places);
    const neg = this.n < 0n;
    const abs = new Q(neg ? -this.n : this.n, this.d).fois(new Q(echelle));
    let ent = abs.n / abs.d;
    if ((abs.n % abs.d) * 2n >= abs.d) ent += 1n;
    let s = ent.toString().padStart(places + 1, '0');
    const entiere = s.slice(0, s.length - places);
    let dec = s.slice(s.length - places).replace(/0+$/, '');
    s = dec ? `${entiere}.${dec}` : entiere;
    return neg && ent !== 0n ? `-${s}` : s;
  }
}

export type TypeTx = 'buy' | 'sell' | 'drip' | 'dividend' | 'split' | 'contribution' | 'withdrawal' | 'fee';

export interface Tx {
  date: string; // AAAA-MM-JJ
  type: TypeTx;
  symbole?: string; // BOURSE:SYMBOLE
  qte?: string;
  prix?: string;
  montant?: string;
  frais?: string;
  ratio?: string;
  note?: string;
}

const estFlux = (t: Tx) => t.type === 'contribution' || t.type === 'withdrawal';

interface Lot {
  qte: Q;
  cout: Q;
}
export interface Etat {
  encaisse: Q;
  lots: Map<string, Lot>;
  dernierPrix: Map<string, Q>;
}

/** Applique les écritures `txs` dans l'ordre du tableau (déjà chronologique) selon le filtre `garder`. */
export function replier(txs: Tx[], garder: (t: Tx) => boolean = () => true): Etat {
  let encaisse = Q.ZERO;
  const lots = new Map<string, Lot>();
  const dernierPrix = new Map<string, Q>();
  const lot = (s: string): Lot => {
    let l = lots.get(s);
    if (!l) lots.set(s, (l = { qte: Q.ZERO, cout: Q.ZERO }));
    return l;
  };
  for (const t of txs.filter(garder)) {
    const brut = t.qte && t.prix ? Q.de(t.qte).fois(Q.de(t.prix)) : Q.ZERO;
    const frais = t.frais ? Q.de(t.frais) : Q.ZERO;
    const s = t.symbole ?? '';
    switch (t.type) {
      case 'contribution':
        encaisse = encaisse.plus(Q.de(t.montant!));
        break;
      case 'withdrawal':
      case 'fee':
        encaisse = encaisse.moins(Q.de(t.montant!));
        break;
      case 'dividend':
        encaisse = encaisse.plus(Q.de(t.montant!));
        break;
      case 'buy': {
        encaisse = encaisse.moins(brut).moins(frais);
        const l = lot(s);
        l.qte = l.qte.plus(Q.de(t.qte!));
        l.cout = l.cout.plus(brut).plus(frais); // coût d'achat frais compris (D409)
        dernierPrix.set(s, Q.de(t.prix!));
        break;
      }
      case 'drip': {
        const l = lot(s); // sans effet sur l'encaisse (D409)
        l.qte = l.qte.plus(Q.de(t.qte!));
        l.cout = l.cout.plus(brut);
        dernierPrix.set(s, Q.de(t.prix!));
        break;
      }
      case 'sell': {
        encaisse = encaisse.plus(brut).moins(frais);
        const l = lot(s);
        const q = Q.de(t.qte!);
        if (l.qte.cmp(Q.ZERO) > 0) l.cout = l.cout.moins(l.cout.sur(l.qte).fois(q)); // au coût moyen (D409)
        l.qte = l.qte.moins(q);
        if (l.qte.cmp(Q.ZERO) <= 0) {
          l.qte = Q.ZERO;
          l.cout = Q.ZERO;
        }
        dernierPrix.set(s, Q.de(t.prix!));
        break;
      }
      case 'split': {
        const r = Q.de(t.ratio!);
        lot(s).qte = lot(s).qte.fois(r); // le coût est conservé (D409)
        const p = dernierPrix.get(s);
        if (p) dernierPrix.set(s, p.sur(r)); // valorisation : dernier prix divisé par le ratio (D411)
        break;
      }
    }
  }
  return { encaisse, lots, dernierPrix };
}

export interface PositionAttendue {
  symbole: string;
  qte: string;
  coutMoyen: string;
}

/** Positions et encaisse attendues à la fin du registre, au sens de D409 (coût moyen à 6 décimales). */
export function positionsAttendues(txs: Tx[]): { encaisse: string; positions: PositionAttendue[] } {
  const e = replier(txs);
  const positions = [...e.lots.entries()]
    .filter(([, l]) => l.qte.cmp(Q.ZERO) > 0)
    .map(([symbole, l]) => ({ symbole, qte: l.qte.texte(6), coutMoyen: l.cout.sur(l.qte).texte(6) }))
    .sort((a, b) => a.symbole.localeCompare(b.symbole));
  return { encaisse: e.encaisse.texte(6), positions };
}

/** Valeur d'un compte (encaisse + titres au dernier prix d'écriture) : D411. */
function valeur(e: Etat): Q {
  let v = e.encaisse;
  for (const [s, l] of e.lots) {
    const p = e.dernierPrix.get(s);
    if (p) v = v.plus(l.qte.fois(p));
  }
  return v;
}

/** Valeur avant les flux externes de `date` (D411 : « valeur = avant flux »). */
function valeurAvantFlux(txs: Tx[], date: string): Q {
  return valeur(replier(txs, (t) => t.date < date || (t.date === date && !estFlux(t))));
}
function valeurFinale(txs: Tx[], fin: string): Q {
  return valeur(replier(txs, (t) => t.date <= fin));
}

export interface Rendement {
  twr: number;
  tri: number;
  debut: string;
  fin: string;
  flux: { date: string; montant: string }[];
  valeurFin: string;
}

/**
 * TWR cumulé (produit des rendements entre flux externes) et TRI annualisé (XIRR, jours/365) d'un ensemble de comptes
 * de même devise, jusqu'à `fin` (inclus). Les valeurs aux dates de flux sont additionnées entre comptes.
 */
export function rendementTransactions(comptes: Tx[][], fin: string): Rendement {
  const fluxDate = new Map<string, Q>();
  for (const txs of comptes)
    for (const t of txs)
      if (estFlux(t) && t.date <= fin) {
        const m = Q.de(t.montant!);
        fluxDate.set(t.date, (fluxDate.get(t.date) ?? Q.ZERO).plus(t.type === 'contribution' ? m : m.fois(Q.de('-1'))));
      }
  const dates = [...fluxDate.keys()].sort();
  let produit = 1;
  let base: Q | null = null;
  for (const d of dates) {
    const v = comptes.reduce((acc, txs) => acc.plus(valeurAvantFlux(txs, d)), Q.ZERO);
    if (base !== null && base.cmp(Q.ZERO) > 0) produit *= v.sur(base).num();
    base = v.plus(fluxDate.get(d)!);
  }
  const vFin = comptes.reduce((acc, txs) => acc.plus(valeurFinale(txs, fin)), Q.ZERO);
  produit *= vFin.sur(base!).num();

  // XIRR : -flux aux dates, +valeur finale à `fin`.
  const cf = dates.map((d) => ({ d, m: -fluxDate.get(d)!.num() }));
  cf.push({ d: fin, m: vFin.num() });
  const t0 = Date.parse(cf[0].d);
  const f = (r: number) => cf.reduce((s, x) => s + x.m / Math.pow(1 + r, (Date.parse(x.d) - t0) / 86_400_000 / 365), 0);
  let lo = -0.99;
  let hi = 10;
  for (let i = 0; i < 200; i++) {
    const mid = (lo + hi) / 2;
    if (f(lo) * f(mid) <= 0) hi = mid;
    else lo = mid;
  }
  return {
    twr: produit - 1,
    tri: (lo + hi) / 2,
    debut: dates[0],
    fin,
    flux: cf.map((x) => ({ date: x.d, montant: x.m.toFixed(2) })),
    valeurFin: vFin.texte(6),
  };
}

// --------------------------------------------------------------------------------- plan d'achat (D331, D361)

export interface LigneAchat {
  symbole: string;
  poids: string; // fraction
  prix: string;
}
export interface PlanAchatAttendu {
  parts: Record<string, bigint>;
  investi: string;
  encaisse: string;
}

/**
 * Parts entières : plancher de montant × poids ÷ prix par fonds (D331), puis, tant qu'une part est payable, une part
 * au fonds de plus grand écart strictement positif (montant cible − montant acheté) ; égalité : symbole le plus petit (D361).
 */
export function planAchat(montant: string, lignes: LigneAchat[]): PlanAchatAttendu {
  const parts: Record<string, bigint> = {};
  const cible = new Map<string, Q>();
  let reste = Q.de(montant);
  for (const l of lignes) {
    const c = Q.de(montant).fois(Q.de(l.poids));
    cible.set(l.symbole, c);
    parts[l.symbole] = c.sur(Q.de(l.prix)).plancher();
    reste = reste.moins(new Q(parts[l.symbole]).fois(Q.de(l.prix)));
  }
  for (;;) {
    let meilleur: { l: LigneAchat; ecart: Q } | null = null;
    for (const l of lignes) {
      const ecart = cible.get(l.symbole)!.moins(new Q(parts[l.symbole]).fois(Q.de(l.prix)));
      if (ecart.cmp(Q.ZERO) <= 0 || Q.de(l.prix).cmp(reste) > 0) continue;
      if (!meilleur || ecart.cmp(meilleur.ecart) > 0 || (ecart.cmp(meilleur.ecart) === 0 && l.symbole < meilleur.l.symbole))
        meilleur = { l, ecart };
    }
    if (!meilleur) break;
    parts[meilleur.l.symbole] += 1n;
    reste = reste.moins(Q.de(meilleur.l.prix));
  }
  return { parts, investi: Q.de(montant).moins(reste).texte(6), encaisse: reste.texte(6) };
}

// --------------------------------------------------------------------------------- exposition

export type Classe = 'equity_ca' | 'equity_us' | 'equity_intl' | 'bond_ca' | 'bond_other' | 'other_assets';
export const CLASSES: Classe[] = ['equity_ca', 'equity_us', 'equity_intl', 'bond_ca', 'bond_other', 'other_assets'];

export interface ProfilFnb {
  repartition: Partial<Record<Classe, number>>;
  rfg: number;
}

/** Exposition par transparence des positions (quantité × prix) : part de chaque classe et RFG pondéré. */
export function exposition(
  positions: { symbole: string; qte: Q; prix: Q }[],
  profils: Record<string, ProfilFnb>,
): { valeur: Q; classes: Record<Classe, number>; rfg: number } {
  let valeur = Q.ZERO;
  const parClasse: Record<Classe, number> = { equity_ca: 0, equity_us: 0, equity_intl: 0, bond_ca: 0, bond_other: 0, other_assets: 0 };
  let rfg = 0;
  for (const p of positions) {
    const v = p.qte.fois(p.prix);
    valeur = valeur.plus(v);
    const pr = profils[p.symbole];
    for (const c of CLASSES) parClasse[c] += v.num() * (pr.repartition[c] ?? 0);
    rfg += v.num() * pr.rfg;
  }
  const tot = valeur.num();
  for (const c of CLASSES) parClasse[c] /= tot;
  return { valeur, classes: parClasse, rfg: rfg / tot };
}
