import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { RouterLink } from '@angular/router';
import { catchError, forkJoin, of } from 'rxjs';
import { Account, AccountsService, Exposure, Model, Performance, PortfolioService, PositionList } from '../../api';
import { afficherDecimal, messageProbleme } from '../../core/probleme';
import { TraductionPipe } from '../../i18n/t.pipe';
import { ExpositionTable, SerieRendement } from './affichages';

type Etat = 'chargement' | 'pret' | 'erreur';

interface PositionsCompte {
  compte: Account;
  liste: PositionList | null;
}

/**
 * Tableau de bord consolidé (F19) : positions par compte, exposition, écarts au modèle et rendement consolidé.
 * Chaque section charge et échoue seule : une erreur d'une section n'efface pas les autres. Aucun calcul ici.
 */
@Component({
  selector: 'app-dashboard',
  imports: [
    ExpositionTable,
    FormsModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    RouterLink,
    SerieRendement,
    TraductionPipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.tableau.titre' | t }}</h2>

    <section aria-labelledby="titre-positions" data-testid="tableau-positions">
      <h3 id="titre-positions">{{ 'pf.tableau.positions' | t }}</h3>
      @switch (etatPositions()) {
        @case ('chargement') {
          <p role="status" data-testid="tableau-positions-chargement">
            {{ 'pf.chargement' | t }}
          </p>
        }
        @case ('erreur') {
          <p class="erreur" role="alert" data-testid="tableau-positions-erreur">
            <span class="icone" aria-hidden="true">✖</span>{{ messagePositions() }}
          </p>
          <button mat-stroked-button type="button" (click)="chargerPositions()">
            {{ 'liste.reessayer' | t }}
          </button>
        }
        @default {
          @if (positions().length === 0) {
            <p role="status" data-testid="tableau-vide">
              {{ 'pf.tableau.aucunCompte' | t }}
            </p>
          }
          @for (p of positions(); track p.compte.id) {
            <div class="carte" [attr.data-testid]="'tableau-compte-' + p.compte.id">
              <h4>
                <a [routerLink]="['/comptes', p.compte.id]">{{ p.compte.name }}</a>
              </h4>
              @if (p.liste; as l) {
                <dl>
                  <dt>{{ 'pos.encaisse' | t }}</dt>
                  <dd>{{ nombre(l.cash) }} {{ l.currency }}</dd>
                  <dt>{{ 'pos.valeurTotale' | t }}</dt>
                  <dd data-testid="tableau-valeur">
                    @if (l.totalValue) {
                      {{ nombre(l.totalValue) }} {{ l.currency }}
                    } @else {
                      <span class="absent">{{ 'pos.nonDisponible' | t }}</span>
                    }
                  </dd>
                </dl>
                @if (l.positions.length === 0) {
                  <p role="status">{{ 'pos.vide' | t }}</p>
                } @else {
                  <div class="defile">
                    <table class="simple">
                      <caption class="sr-only">
                        {{
                          p.compte.name
                        }}
                      </caption>
                      <thead>
                        <tr>
                          <th scope="col">{{ 'reg.champ.symbol' | t }}</th>
                          <th scope="col" class="num">
                            {{ 'reg.champ.quantity' | t }}
                          </th>
                          <th scope="col" class="num">
                            {{ 'pos.valeurMarche' | t }}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        @for (x of l.positions; track x.symbol) {
                          <tr>
                            <th scope="row">{{ x.symbol }}</th>
                            <td class="num">{{ nombre(x.quantity) }}</td>
                            <td class="num">
                              @if (x.marketValue) {
                                {{ nombre(x.marketValue) }} {{ x.currency }}
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
              } @else {
                <p class="erreur" role="alert">
                  <span class="icone" aria-hidden="true">✖</span>{{ 'pf.tableau.positionsIndisponibles' | t }}
                </p>
              }
            </div>
          }
        }
      }
    </section>

    <section aria-labelledby="titre-expo" data-testid="tableau-exposition">
      <h3 id="titre-expo">{{ 'pf.tableau.exposition' | t }}</h3>
      <mat-form-field class="large">
        <mat-label>{{ 'pf.expo.modeleComparaison' | t }}</mat-label>
        <select
          matNativeControl
          name="modele"
          data-testid="tableau-modele"
          [ngModel]="modeleId()"
          (ngModelChange)="choisirModele($event)"
        >
          <option value="">{{ 'pf.expo.sansModele' | t }}</option>
          @for (m of modeles(); track m.id) {
            <option [value]="m.id">{{ m.name }} ({{ 'pf.col.version' | t }} {{ m.version }})</option>
          }
        </select>
      </mat-form-field>
      @switch (etatExposition()) {
        @case ('chargement') {
          <p role="status">{{ 'pf.chargement' | t }}</p>
        }
        @case ('erreur') {
          <p class="erreur" role="alert" data-testid="tableau-exposition-erreur">
            <span class="icone" aria-hidden="true">✖</span>{{ messageExposition() }}
          </p>
          <button mat-stroked-button type="button" (click)="chargerExposition()">
            {{ 'liste.reessayer' | t }}
          </button>
        }
        @default {
          <app-exposition-table [exposition]="exposition()" />
        }
      }
    </section>

    <section aria-labelledby="titre-rendement" data-testid="tableau-rendement">
      <h3 id="titre-rendement">{{ 'pf.tableau.rendement' | t }}</h3>
      @switch (etatRendement()) {
        @case ('chargement') {
          <p role="status">{{ 'pf.chargement' | t }}</p>
        }
        @case ('erreur') {
          <p class="erreur" role="alert" data-testid="tableau-rendement-erreur">
            <span class="icone" aria-hidden="true">✖</span>{{ messageRendement() }}
          </p>
          <button mat-stroked-button type="button" (click)="chargerRendement()">
            {{ 'liste.reessayer' | t }}
          </button>
        }
        @default {
          @if (rendement(); as r) {
            <app-serie-rendement
              [serie]="r.consolidated"
              [titre]="'pf.perf.consolide' | t"
              testid="tableau-serie"
              granularite="annual"
            />
          }
        }
      }
    </section>
  `,
})
export class Dashboard {
  private readonly api = inject(PortfolioService);
  private readonly comptesApi = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);
  protected readonly nombre = afficherDecimal;

  protected readonly etatPositions = signal<Etat>('chargement');
  protected readonly messagePositions = signal('');
  protected readonly positions = signal<PositionsCompte[]>([]);

  protected readonly modeles = signal<Model[]>([]);
  protected readonly modeleId = signal('');
  protected readonly etatExposition = signal<Etat>('chargement');
  protected readonly messageExposition = signal('');
  protected readonly exposition = signal<Exposure | null>(null);

  protected readonly etatRendement = signal<Etat>('chargement');
  protected readonly messageRendement = signal('');
  protected readonly rendement = signal<Performance | null>(null);

  constructor() {
    this.chargerPositions();
    this.api
      .listModels()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.modeles.set(l.items);
          this.modeleId.set(l.items[0]?.id ?? '');
          this.chargerExposition();
        },
        // Sans la liste des modèles, l'exposition reste affichable sans comparaison.
        error: () => this.chargerExposition(),
      });
    this.chargerRendement();
  }

  protected chargerPositions(): void {
    this.etatPositions.set('chargement');
    this.comptesApi
      .listAccounts()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          if (l.items.length === 0) {
            this.positions.set([]);
            this.etatPositions.set('pret');
            return;
          }
          forkJoin(l.items.map((compte) => this.comptesApi.listPositions(compte.id).pipe(catchError(() => of(null)))))
            .pipe(takeUntilDestroyed(this.destroyRef))
            .subscribe((listes) => {
              this.positions.set(l.items.map((compte, i) => ({ compte, liste: listes[i] })));
              this.etatPositions.set('pret');
            });
        },
        error: (e: unknown) => {
          this.messagePositions.set(messageProbleme(e, 'cpt.erreur'));
          this.etatPositions.set('erreur');
        },
      });
  }

  protected choisirModele(id: string): void {
    this.modeleId.set(id);
    this.chargerExposition();
  }

  protected chargerExposition(): void {
    this.etatExposition.set('chargement');
    this.api
      .getExposure(undefined, this.modeleId() || undefined)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (e) => {
          this.exposition.set(e);
          this.etatExposition.set('pret');
        },
        error: (e: unknown) => {
          this.messageExposition.set(messageProbleme(e, 'pf.expo.erreur'));
          this.etatExposition.set('erreur');
        },
      });
  }

  protected chargerRendement(): void {
    this.etatRendement.set('chargement');
    this.api
      .getPerformance()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (r) => {
          this.rendement.set(r);
          this.etatRendement.set('pret');
        },
        error: (e: unknown) => {
          this.messageRendement.set(messageProbleme(e, 'pf.perf.erreur'));
          this.etatRendement.set('erreur');
        },
      });
  }
}
