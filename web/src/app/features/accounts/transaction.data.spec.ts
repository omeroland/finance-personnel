import { Currency, Transaction, TransactionType } from '../../api';
import { construireTransaction, ecritureInverse, statuts } from './transaction.data';

describe('transaction.data', () => {
  const base = { tradeDate: '2026-01-15', note: '' };

  it('construit un achat avec des décimaux exacts (virgule fr-CA acceptée)', () => {
    const r = construireTransaction(
      TransactionType.buy,
      {
        ...base,
        symbol: 'tsx:xeqt',
        quantity: '100',
        price: '30,50',
        fee: '9.99',
      },
      Currency.CAD,
    );
    expect(r).toEqual({
      requete: {
        type: 'buy',
        tradeDate: '2026-01-15',
        symbol: 'TSX:XEQT',
        quantity: '100',
        price: '30.50',
        fee: '9.99',
        currency: 'CAD',
      },
    });
  });

  it('signale les champs requis et les nombres non positifs', () => {
    const r = construireTransaction(
      TransactionType.buy,
      { ...base, symbol: 'XEQT', quantity: '-1', price: '' },
      Currency.CAD,
    );
    expect('erreurs' in r && r.erreurs).toEqual({
      symbol: 'reg.erreur.symbole',
      quantity: 'reg.erreur.positif',
      price: 'reg.erreur.requis',
    });
  });

  it('exige un montant ou un symbole avec quantité pour un transfert', () => {
    const r = construireTransaction(TransactionType.transfer, { ...base, counterpartAccountId: 'abc' }, Currency.CAD);
    expect('erreurs' in r && r.erreurs.amount).toBe('reg.erreur.transfert');
  });

  const original: Transaction = {
    id: 't1',
    accountId: 'a1',
    type: TransactionType.contribution,
    tradeDate: '2026-01-10',
    amount: '5000',
    currency: Currency.CAD,
    createdAt: '2026-01-10T00:00:00Z',
  };

  it("l'écriture inverse recopie l'original et ajoute reversesId", () => {
    expect(ecritureInverse(original)).toMatchObject({
      type: 'contribution',
      amount: '5000',
      tradeDate: '2026-01-10',
      reversesId: 't1',
    });
  });

  it('classe originale, annulée et inverse', () => {
    const inverse = { ...original, id: 't2', reversesId: 't1' };
    const s = statuts([original, inverse, { ...original, id: 't3' }]);
    expect(s.get('t1')).toBe('annulee');
    expect(s.get('t2')).toBe('inverse');
    expect(s.get('t3')).toBe('originale');
  });
});
