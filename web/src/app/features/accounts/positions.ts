import { ChangeDetectionStrategy, Component, DestroyRef, OnInit, inject, input, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { Account, AccountsService, PositionList } from '../../api';
import { afficherDecimal, messageProbleme } from '../../core/probleme';
import { TraductionPipe } from '../../i18n/t.pipe';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Positions d'un compte. Prix et valeur de marché sont absents tant que la phase 3 n'existe pas : « non disponible ». */
@Component({
  selector: 'app-positions',
  imports: [MatButtonModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './accounts.scss',
  template: `
    <h2>{{ 'pos.titre' | t }}</h2>
    @switch (etat()) {
      @case ('chargement') {
        <p role="status" data-testid="pos-chargement">
          {{ 'pos.chargement' | t }}
        </p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="pos-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @default {
        @if (liste(); as l) {
          <dl data-testid="pos-resume">
            <dt>{{ 'pos.encaisse' | t }}</dt>
            <dd data-testid="pos-encaisse">{{ montant(l.cash) }} {{ l.currency }}</dd>
            <dt>{{ 'pos.valeurTotale' | t }}</dt>
            <dd data-testid="pos-total">
              @if (l.totalValue) {
                {{ montant(l.totalValue) }} {{ l.currency }}
              } @else {
                <span class="absent">{{ 'pos.nonDisponible' | t }}</span>
              }
            </dd>
          </dl>
          @if (l.positions.length === 0) {
            <p role="status" data-testid="pos-vide">{{ 'pos.vide' | t }}</p>
          } @else {
            <div class="defile">
              <table class="simple" data-testid="positions">
                <caption class="sr-only">
                  {{
                    'pos.titre' | t
                  }}
                </caption>
                <thead>
                  <tr>
                    <th scope="col">{{ 'reg.champ.symbol' | t }}</th>
                    <th scope="col" class="num">
                      {{ 'reg.champ.quantity' | t }}
                    </th>
                    <th scope="col" class="num">{{ 'pos.coutMoyen' | t }}</th>
                    <th scope="col" class="num">{{ 'reg.champ.price' | t }}</th>
                    <th scope="col" class="num">
                      {{ 'pos.valeurMarche' | t }}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  @for (p of l.positions; track p.symbol) {
                    <tr>
                      <td>
                        {{ p.symbol }}
                        @if (p.name) {
                          <br /><small>{{ p.name }}</small>
                        }
                      </td>
                      <td class="num">{{ montant(p.quantity) }}</td>
                      <td class="num">
                        @if (p.averageCost) {
                          {{ montant(p.averageCost) }}
                        } @else {
                          <span class="absent">{{ 'pos.nonDisponible' | t }}</span>
                        }
                      </td>
                      <td class="num" data-col="prix">
                        @if (p.price) {
                          {{ montant(p.price) }}
                        } @else {
                          <span class="absent">{{ 'pos.nonDisponible' | t }}</span>
                        }
                      </td>
                      <td class="num" data-col="valeur">
                        @if (p.marketValue) {
                          {{ montant(p.marketValue) }} {{ p.currency }}
                        } @else {
                          <span class="absent">{{ 'pos.nonDisponible' | t }}</span>
                        }
                      </td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
          }
        }
      }
    }
  `,
})
export class Positions implements OnInit {
  private readonly api = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);

  readonly compte = input.required<Account>();

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly liste = signal<PositionList | null>(null);

  ngOnInit(): void {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listPositions(this.compte().id)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.liste.set(l);
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'pos.erreur'));
          this.etat.set('erreur');
        },
      });
  }

  protected montant(v: string | undefined): string {
    return afficherDecimal(v);
  }
}
