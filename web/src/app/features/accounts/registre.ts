import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, input, signal, OnInit } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Account, Transaction, TransactionType, TransactionsService } from '../../api';
import { afficherDecimal, messageProbleme } from '../../core/probleme';
import { CleTraduction, TraductionPipe, traduire } from '../../i18n/t.pipe';
import { InverseDialog } from './inverse-dialog';
import {
  CHAMPS_PAR_TYPE,
  ChampTx,
  ErreursTx,
  SaisieTx,
  StatutTx,
  construireTransaction,
  ecritureInverse,
  statuts,
} from './transaction.data';

type Etat = 'chargement' | 'pret' | 'erreur';

const ICONES: Record<StatutTx, string> = {
  originale: '●',
  inverse: '↺',
  annulee: '⊘',
};
const LIBELLES: Record<StatutTx, CleTraduction> = {
  originale: 'reg.statut.originale',
  inverse: 'reg.statut.inverse',
  annulee: 'reg.statut.annulee',
};
const ETIQUETTES: Record<ChampTx, CleTraduction> = {
  symbol: 'reg.champ.symbol',
  quantity: 'reg.champ.quantity',
  price: 'reg.champ.price',
  amount: 'reg.champ.amount',
  fee: 'reg.champ.fee',
  splitRatio: 'reg.champ.splitRatio',
  counterpartAccountId: 'reg.champ.counterpartAccountId',
};

function aujourdhui(): string {
  return new Date().toISOString().slice(0, 10);
}

/** Registre d'un compte en insertion seule : ajout d'une transaction et correction par écriture inverse. */
@Component({
  selector: 'app-registre',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, MatSelectModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './registre.html',
  styleUrl: './accounts.scss',
})
export class Registre implements OnInit {
  private readonly api = inject(TransactionsService);
  private readonly dialog = inject(MatDialog);
  private readonly snack = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  readonly compte = input.required<Account>();
  /** Autres comptes, pour la contrepartie d'un transfert. */
  readonly autres = input<Account[]>([]);

  protected readonly types = Object.values(TransactionType);
  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly registre = signal<Transaction[]>([]);
  protected readonly statutDe = computed(() => statuts(this.registre()));

  protected readonly type = signal<TransactionType>(TransactionType.buy);
  protected readonly champs = computed(() => CHAMPS_PAR_TYPE[this.type()]);
  protected readonly saisie = signal<SaisieTx>({
    tradeDate: aujourdhui(),
    note: '',
  });
  protected readonly erreurs = signal<ErreursTx>({});
  protected readonly erreurEnvoi = signal('');
  protected readonly envoi = signal(false);

  ngOnInit(): void {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listTransactions(this.compte().id)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (liste) => {
          this.registre.set(liste.items);
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'reg.erreur.chargement'));
          this.etat.set('erreur');
        },
      });
  }

  protected etiquette(c: ChampTx): CleTraduction {
    return ETIQUETTES[c];
  }

  protected libelleType(t: TransactionType): string {
    return traduire(`reg.type.${t}`);
  }

  protected changerType(t: TransactionType): void {
    this.type.set(t);
    this.erreurs.set({});
  }

  protected valeur(c: ChampTx | 'tradeDate' | 'note'): string {
    return (this.saisie() as Record<string, string | undefined>)[c] ?? '';
  }

  protected modifier(c: ChampTx | 'tradeDate' | 'note', v: string): void {
    this.saisie.update((s) => ({ ...s, [c]: v }));
  }

  protected erreurChamp(c: ChampTx | 'tradeDate'): CleTraduction | null {
    return (this.erreurs()[c] as CleTraduction | undefined) ?? null;
  }

  protected nomAutre(id: string): string {
    return this.autres().find((a) => a.id === id)?.name ?? id;
  }

  protected ajouter(): void {
    const resultat = construireTransaction(this.type(), this.saisie(), this.compte().currency);
    if ('erreurs' in resultat) {
      this.erreurs.set(resultat.erreurs);
      return;
    }
    this.erreurs.set({});
    this.erreurEnvoi.set('');
    this.envoi.set(true);
    this.api.createTransaction(this.compte().id, resultat.requete).subscribe({
      next: () => {
        this.envoi.set(false);
        this.saisie.set({ tradeDate: this.saisie().tradeDate, note: '' });
        this.snack.open(traduire('reg.ajoutee'), traduire('liste.fermer'), {
          duration: 5000,
        });
        this.charger();
      },
      error: (e: unknown) => {
        this.envoi.set(false);
        this.erreurEnvoi.set(messageProbleme(e, 'reg.erreur.envoi'));
      },
    });
  }

  protected annuler(t: Transaction): void {
    this.dialog
      .open(InverseDialog, { data: { transaction: t } })
      .afterClosed()
      .subscribe((ok) => {
        if (!ok) {
          return;
        }
        this.api.createTransaction(this.compte().id, ecritureInverse(t)).subscribe({
          next: () => {
            this.snack.open(traduire('reg.inverseAjoutee'), traduire('liste.fermer'), { duration: 5000 });
            this.charger();
          },
          error: (e: unknown) =>
            this.snack.open(messageProbleme(e, 'reg.erreur.inverse'), traduire('liste.fermer'), { duration: 8000 }),
        });
      });
  }

  protected icone(t: Transaction): string {
    return ICONES[this.statutDe().get(t.id) ?? 'originale'];
  }

  protected libelleStatut(t: Transaction): CleTraduction {
    return LIBELLES[this.statutDe().get(t.id) ?? 'originale'];
  }

  protected statut(t: Transaction): StatutTx {
    return this.statutDe().get(t.id) ?? 'originale';
  }

  protected montant(v: string | undefined): string {
    return afficherDecimal(v);
  }
}
