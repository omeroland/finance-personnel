import { Transaction, TransactionCreate, TransactionType } from '../../api';
import { normaliserDecimal } from '../stockeval/edit/edit.data';

export type ChampTx = 'symbol' | 'quantity' | 'price' | 'amount' | 'fee' | 'splitRatio' | 'counterpartAccountId';

/** Champs affichés par type de transaction (D393 : champs requis par type). Présentation seulement, le serveur valide. */
export const CHAMPS_PAR_TYPE: Record<TransactionType, readonly ChampTx[]> = {
  [TransactionType.buy]: ['symbol', 'quantity', 'price', 'fee'],
  [TransactionType.sell]: ['symbol', 'quantity', 'price', 'fee'],
  [TransactionType.drip]: ['symbol', 'quantity', 'price'],
  [TransactionType.dividend]: ['symbol', 'amount'],
  [TransactionType.split]: ['symbol', 'splitRatio'],
  [TransactionType.transfer]: ['counterpartAccountId', 'amount', 'symbol', 'quantity'],
  [TransactionType.contribution]: ['amount'],
  [TransactionType.withdrawal]: ['amount'],
  [TransactionType.fee]: ['amount'],
};

/** Champs obligatoires par type ; pour un transfert, montant ou (symbole + quantité) est vérifié à part. */
export const REQUIS_PAR_TYPE: Record<TransactionType, readonly ChampTx[]> = {
  [TransactionType.buy]: ['symbol', 'quantity', 'price'],
  [TransactionType.sell]: ['symbol', 'quantity', 'price'],
  [TransactionType.drip]: ['symbol', 'quantity', 'price'],
  [TransactionType.dividend]: ['symbol', 'amount'],
  [TransactionType.split]: ['symbol', 'splitRatio'],
  [TransactionType.transfer]: ['counterpartAccountId'],
  [TransactionType.contribution]: ['amount'],
  [TransactionType.withdrawal]: ['amount'],
  [TransactionType.fee]: ['amount'],
};

/** Même motif que le schéma `Symbol` du contrat ; le serveur reste l'autorité. */
export const MOTIF_SYMBOLE = /^[A-Z]{2,10}:[A-Z0-9.-]{1,15}$/;

const POSITIF = /^[0-9]+(\.[0-9]+)?$/;

export type ErreursTx = Partial<Record<ChampTx | 'tradeDate', string>>;

/** Saisie du formulaire : texte brut de chaque champ. */
export type SaisieTx = Partial<Record<ChampTx, string>> & {
  tradeDate: string;
  note: string;
};

/** Valide la saisie et construit la requête ; les valeurs restent des chaînes décimales exactes (aucun calcul). */
export function construireTransaction(
  type: TransactionType,
  saisie: SaisieTx,
  devise: TransactionCreate['currency'],
): { requete: TransactionCreate } | { erreurs: ErreursTx } {
  const erreurs: ErreursTx = {};
  if (!/^\d{4}-\d{2}-\d{2}$/.test(saisie.tradeDate)) {
    erreurs.tradeDate = 'reg.erreur.date';
  }
  const requete: TransactionCreate = {
    type,
    tradeDate: saisie.tradeDate,
    currency: devise,
  };
  const brut = (c: ChampTx): string => (saisie[c] ?? '').trim();

  for (const champ of CHAMPS_PAR_TYPE[type]) {
    const texte = brut(champ);
    if (texte === '') {
      if (REQUIS_PAR_TYPE[type].includes(champ)) {
        erreurs[champ] = 'reg.erreur.requis';
      }
      continue;
    }
    if (champ === 'symbol') {
      const s = texte.toUpperCase();
      if (MOTIF_SYMBOLE.test(s)) {
        requete.symbol = s;
      } else {
        erreurs.symbol = 'reg.erreur.symbole';
      }
    } else if (champ === 'counterpartAccountId') {
      requete.counterpartAccountId = texte;
    } else {
      const n = normaliserDecimal(texte);
      if (n !== null && POSITIF.test(n) && /[1-9]/.test(n)) {
        requete[champ] = n;
      } else {
        erreurs[champ] = 'reg.erreur.positif';
      }
    }
  }
  if (type === TransactionType.transfer && !erreurs.amount && !erreurs.symbol && !erreurs.quantity) {
    const titres = requete.symbol !== undefined && requete.quantity !== undefined;
    if (requete.amount === undefined && !titres) {
      erreurs.amount = 'reg.erreur.transfert';
    }
  }
  const note = saisie.note.trim();
  if (note !== '') {
    requete.note = note;
  }
  return Object.keys(erreurs).length > 0 ? { erreurs } : { requete };
}

/** Écriture inverse : recopie les champs de l'original et ajoute `reversesId` (D404). */
export function ecritureInverse(t: Transaction): TransactionCreate {
  return {
    type: t.type,
    tradeDate: t.tradeDate,
    symbol: t.symbol,
    quantity: t.quantity,
    price: t.price,
    amount: t.amount,
    fee: t.fee,
    splitRatio: t.splitRatio,
    counterpartAccountId: t.counterpartAccountId,
    currency: t.currency,
    note: t.note,
    reversesId: t.id,
  };
}

export type StatutTx = 'originale' | 'inverse' | 'annulee';

/** Statut d'une ligne du registre, déduit des liens `reversesId` (aucun montant). */
export function statuts(registre: readonly Transaction[]): Map<string, StatutTx> {
  const annulees = new Set(registre.map((t) => t.reversesId).filter((id): id is string => id !== undefined));
  return new Map(
    registre.map(
      (t) => [t.id, t.reversesId !== undefined ? 'inverse' : annulees.has(t.id) ? 'annulee' : 'originale'] as const,
    ),
  );
}
