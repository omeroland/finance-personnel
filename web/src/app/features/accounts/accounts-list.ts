import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import { RouterLink } from '@angular/router';
import { Account, AccountsService } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { CompteDialog } from './compte-dialog';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Liste des comptes et création d'un compte. */
@Component({
  selector: 'app-accounts-list',
  imports: [MatButtonModule, RouterLink, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './accounts-list.html',
  styleUrl: './accounts.scss',
})
export class AccountsList {
  private readonly api = inject(AccountsService);
  private readonly dialog = inject(MatDialog);
  private readonly snack = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly etat = signal<Etat>('chargement');
  protected readonly comptes = signal<Account[]>([]);
  protected readonly message = signal('');

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listAccounts()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (liste) => {
          this.comptes.set(liste.items);
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'cpt.erreur'));
          this.etat.set('erreur');
        },
      });
  }

  protected libelleType(c: Account): string {
    return traduire(`cpt.type.${c.type}`);
  }

  protected creer(): void {
    this.dialog
      .open(CompteDialog, { width: '28rem' })
      .afterClosed()
      .subscribe((compte: Account | undefined) => {
        if (compte) {
          this.snack.open(traduire('cpt.cree', { nom: compte.name }), traduire('liste.fermer'), { duration: 5000 });
          this.charger();
        }
      });
  }
}
