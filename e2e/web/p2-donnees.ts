// P2-14 : données SYNTHÉTIQUES du parcours de la phase 2 (aucune donnée réelle). Les écritures sont décrites une
// seule fois ici : elles servent à produire les CSV envoyés à l'application ET au calcul de référence indépendant.
import type { Classe, ProfilFnb, Tx } from './p2-reference';

export const XEQT = 'TSX:XEQT';
export const VFV = 'TSX:VFV';
export const ZAG = 'TSX:ZAG';
export const XIC = 'TSX:XIC';

/** Profils de FNB synthétiques (répartition en fraction, RFG en fraction), posés dans le catalogue du propriétaire de test. */
export const PROFILS: Record<string, ProfilFnb & { type: string; region: string }> = {
  [XEQT]: { type: 'equity', region: 'global', repartition: { equity_ca: 0.25, equity_us: 0.45, equity_intl: 0.3 }, rfg: 0.002 },
  [VFV]: { type: 'equity', region: 'us', repartition: { equity_us: 1 }, rfg: 0.0008 },
  [ZAG]: { type: 'fixed_income', region: 'ca', repartition: { bond_ca: 1 }, rfg: 0.0009 },
  [XIC]: { type: 'equity', region: 'ca', repartition: { equity_ca: 1 }, rfg: 0.0006 },
};

/** Catalogue fictif de l'oracle (tools/oracle/etf_scenarios.py, SYN-*) : mêmes répartitions et RFG, symboles `TSX:SYN…`. */
export const SYN: Record<string, ProfilFnb & { type: string; region: string; oracle: string }> = {
  'TSX:SYNUS': { oracle: 'SYN-US', type: 'equity', region: 'us', repartition: { equity_us: 1 }, rfg: 0.0009 },
  'TSX:SYNINT': { oracle: 'SYN-INT', type: 'equity', region: 'intl', repartition: { equity_intl: 1 }, rfg: 0.002 },
  'TSX:SYNOBL': { oracle: 'SYN-OBL', type: 'fixed_income', region: 'ca', repartition: { bond_ca: 1 }, rfg: 0.001 },
  'TSX:SYNBAL': { oracle: 'SYN-BAL', type: 'balanced', region: 'global', repartition: { equity_ca: 0.15, equity_us: 0.25, equity_intl: 0.2, bond_ca: 0.3, bond_other: 0.1 }, rfg: 0.002 },
  'TSX:SYNGLB': { oracle: 'SYN-GLB', type: 'fixed_income', region: 'global', repartition: { bond_ca: 0.3, bond_other: 0.7 }, rfg: 0.0025 },
};

/** Compte CELI : cotisations, achats avec frais, vente, DRIP, fractionnement 2 pour 1, dividende, parts fractionnaires. */
export const CELI: Tx[] = [
  { date: '2025-01-10', type: 'contribution', montant: '10000.00' },
  { date: '2025-01-15', type: 'buy', symbole: XEQT, qte: '100', prix: '30.50', frais: '9.99' },
  { date: '2025-02-14', type: 'buy', symbole: ZAG, qte: '80', prix: '14.25' },
  { date: '2025-03-03', type: 'sell', symbole: XEQT, qte: '30', prix: '32.10', frais: '4.95' },
  { date: '2025-04-01', type: 'drip', symbole: XEQT, qte: '0.75', prix: '31.00' },
  { date: '2025-05-01', type: 'split', symbole: XEQT, ratio: '2' },
  { date: '2025-06-02', type: 'dividend', symbole: ZAG, montant: '45.60' },
  { date: '2025-06-16', type: 'buy', symbole: XEQT, qte: '10', prix: '17.00' },
  { date: '2025-07-01', type: 'contribution', montant: '2000.00' },
  { date: '2025-08-01', type: 'buy', symbole: VFV, qte: '10.5', prix: '100.25' },
  { date: '2025-10-15', type: 'sell', symbole: ZAG, qte: '20', prix: '14.60' },
];

/** Compte non enregistré (taxable). */
export const TAXABLE: Tx[] = [
  { date: '2025-02-03', type: 'contribution', montant: '5000.00' },
  { date: '2025-02-10', type: 'buy', symbole: XEQT, qte: '50', prix: '32.00', frais: '6.95' },
  { date: '2025-04-07', type: 'buy', symbole: ZAG, qte: '120', prix: '14.50' },
  { date: '2025-05-01', type: 'split', symbole: XEQT, ratio: '2' },
  { date: '2025-10-01', type: 'sell', symbole: ZAG, qte: '20', prix: '14.20' },
  { date: '2025-11-03', type: 'dividend', symbole: XEQT, montant: '31.25' },
  { date: '2025-11-20', type: 'withdrawal', montant: '200.00' },
];

/** Compte de l'oracle (reequilibrage-fidele-02) : 100 VFV, 20 XIC et 80 ZAG à 50 $, sans encaisse. */
export const ORACLE_RE: Tx[] = [
  { date: '2025-01-05', type: 'contribution', montant: '10000.00' },
  { date: '2025-01-06', type: 'buy', symbole: VFV, qte: '100', prix: '50' },
  { date: '2025-01-06', type: 'buy', symbole: XIC, qte: '20', prix: '50' },
  { date: '2025-01-06', type: 'buy', symbole: ZAG, qte: '80', prix: '50' },
];

/** Modèle de portefeuille du parcours : poids par fonds et cibles par classe (somme 1 des deux côtés). */
export const MODELE = {
  holdings: [
    { symbol: XEQT, weight: 0.5 },
    { symbol: VFV, weight: 0.2 },
    { symbol: ZAG, weight: 0.3 },
  ],
  // 0,5 x (25 % CA, 45 % US, 30 % intl) + 0,2 x US + 0,3 x obligations canadiennes
  cibles: { equity_ca: 0.125, equity_us: 0.425, equity_intl: 0.15, bond_ca: 0.3, bond_other: 0, other_assets: 0 } as Record<Classe, number>,
  cibles_pct: { equity_ca: '12,5', equity_us: '42,5', equity_intl: '15', bond_ca: '30', bond_other: '0', other_assets: '0' } as Record<Classe, string>,
  poids_pct: ['50', '20', '30'],
};

/** Plan d'achat saisi à l'écran : montant et prix saisis (le registre n'a pas de prix à jour). */
export const ACHAT = { montant: '10000.00', montantSaisi: '10 000,00', prix: { [XEQT]: '31.40', [VFV]: '112.35', [ZAG]: '14.80' } as Record<string, string> };

/** Plan de rééquilibrage saisi à l'écran : cotisation par compte et prix saisis. */
export const REEQ = {
  cotisationCeli: '1500',
  cotisationTaxable: '500',
  prix: { [XEQT]: '18.00', [VFV]: '105.00', [ZAG]: '14.50' } as Record<string, string>,
};

const ENTETE = 'Date;Compte;Type;Symbole;Quantité;Prix;Devise;Frais;Montant;Ratio;Contrepartie;Note';
const ENTETE_VIRGULE = ENTETE.replaceAll(';', ',');

function ligne(t: Tx, compte: string, sep: string, virgule: boolean): string {
  const nb = (s?: string) => (s === undefined ? '' : virgule ? s.replace('.', ',') : s);
  return [t.date, compte, t.type, t.symbole ?? '', nb(t.qte), nb(t.prix), 'CAD', nb(t.frais), nb(t.montant), nb(t.ratio), '', t.note ?? ''].join(sep);
}

/** CSV du compte : `;` et virgule décimale (format québécois) ou `,` et point décimal. */
export function csv(txs: Tx[], compte: string, style: 'quebec' | 'anglo', extra: string[] = []): string {
  const sep = style === 'quebec' ? ';' : ',';
  const lignes = txs.map((t) => ligne(t, compte, sep, style === 'quebec'));
  return [style === 'quebec' ? ENTETE : ENTETE_VIRGULE, ...lignes, ...extra].join('\n') + '\n';
}
