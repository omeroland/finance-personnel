import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatTabsModule } from '@angular/material/tabs';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { Account, AccountsService } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { ImportCsv } from './import-csv';
import { Positions } from './positions';
import { Registre } from './registre';
import { Releves } from './releves';

type Etat = 'chargement' | 'pret' | 'erreur' | 'introuvable';

/** Page d'un compte : registre, relevés, positions et import CSV. L'API n'a pas de lecture d'un compte : on retrouve le compte dans la liste. */
@Component({
  selector: 'app-account-page',
  imports: [ImportCsv, MatButtonModule, MatTabsModule, Positions, Registre, Releves, RouterLink, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './accounts.scss',
  template: `
    <p>
      <a routerLink="/comptes" data-testid="retour">{{ 'cpt.retour' | t }}</a>
    </p>
    @switch (etat()) {
      @case ('chargement') {
        <p role="status">{{ 'cpt.chargement' | t }}</p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @case ('introuvable') {
        <p class="erreur" role="alert" data-testid="introuvable">
          <span class="icone" aria-hidden="true">✖</span>{{ 'cpt.introuvable' | t }}
        </p>
      }
      @default {
        @if (compte(); as c) {
          <h1 data-testid="compte-titre">{{ c.name }}</h1>
          <p>
            {{
              'cpt.resume'
                | t
                  : {
                      type: libelleType(),
                      courtier: c.broker,
                      devise: c.currency,
                    }
            }}
          </p>
          <mat-tab-group animationDuration="0ms" [attr.aria-label]="'cpt.sections' | t">
            <mat-tab [label]="'reg.titre' | t">
              <ng-template matTabContent><app-registre [compte]="c" [autres]="autres()" /></ng-template>
            </mat-tab>
            <mat-tab [label]="'rel.titre' | t">
              <ng-template matTabContent><app-releves [compte]="c" /></ng-template>
            </mat-tab>
            <mat-tab [label]="'pos.titre' | t">
              <ng-template matTabContent><app-positions [compte]="c" /></ng-template>
            </mat-tab>
            <mat-tab [label]="'csv.titre' | t">
              <ng-template matTabContent><app-import-csv [compte]="c" /></ng-template>
            </mat-tab>
          </mat-tab-group>
        }
      }
    }
  `,
})
export class AccountPage {
  private readonly api = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly id = inject(ActivatedRoute).snapshot.paramMap.get('id') ?? '';

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  private readonly comptes = signal<Account[]>([]);
  protected readonly compte = computed(() => this.comptes().find((c) => c.id === this.id) ?? null);
  protected readonly autres = computed(() => this.comptes().filter((c) => c.id !== this.id));
  protected readonly libelleType = computed(() => {
    const c = this.compte();
    return c ? traduire(`cpt.type.${c.type}`) : '';
  });

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listAccounts()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.comptes.set(l.items);
          this.etat.set(l.items.some((c) => c.id === this.id) ? 'pret' : 'introuvable');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'cpt.erreur'));
          this.etat.set('erreur');
        },
      });
  }
}
