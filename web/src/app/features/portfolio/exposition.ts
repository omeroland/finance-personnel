import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { forkJoin } from 'rxjs';
import { Account, AccountsService, Exposure, Model, PortfolioService } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { TraductionPipe } from '../../i18n/t.pipe';
import { ExpositionTable } from './affichages';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Exposition par transparence des comptes choisis, comparée à un modèle facultatif. Aucun calcul dans le client. */
@Component({
  selector: 'app-exposition',
  imports: [ExpositionTable, FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.expo.titre' | t }}</h2>
    @if (etat() === 'chargement') {
      <p role="status" data-testid="expo-chargement">
        {{ 'pf.chargement' | t }}
      </p>
    } @else if (etat() === 'erreur' && !pret()) {
      <p class="erreur" role="alert" data-testid="expo-erreur">
        <span class="icone" aria-hidden="true">✖</span>{{ message() }}
      </p>
      <button mat-stroked-button type="button" (click)="charger()">
        {{ 'liste.reessayer' | t }}
      </button>
    } @else {
      <form (ngSubmit)="afficher()">
        <mat-form-field class="large">
          <mat-label>{{ 'pf.expo.modeleComparaison' | t }}</mat-label>
          <select
            matNativeControl
            name="modele"
            data-testid="expo-modele"
            [ngModel]="modeleId()"
            (ngModelChange)="modeleId.set($event)"
          >
            <option value="">{{ 'pf.expo.sansModele' | t }}</option>
            @for (m of modeles(); track m.id) {
              <option [value]="m.id">{{ m.name }} ({{ 'pf.col.version' | t }} {{ m.version }})</option>
            }
          </select>
        </mat-form-field>
        <fieldset>
          <legend>{{ 'pf.reeq.comptes' | t }}</legend>
          <p>
            <small>{{ 'pf.expo.aideComptes' | t }}</small>
          </p>
          @for (c of comptes(); track c.id) {
            <label class="statut">
              <input
                type="checkbox"
                [attr.data-testid]="'expo-compte-' + c.id"
                [checked]="choisis().includes(c.id)"
                (change)="basculer(c.id, $any($event.target).checked)"
              />
              {{ c.name }} ({{ c.currency }})
            </label>
            <br />
          }
        </fieldset>
        <button mat-flat-button type="submit" data-testid="expo-afficher">
          {{ 'pf.expo.afficher' | t }}
        </button>
      </form>
      @if (etat() === 'erreur') {
        <p class="erreur" role="alert" data-testid="expo-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
      }
      <app-exposition-table [exposition]="exposition()" />
    }
  `,
})
export class Exposition {
  private readonly api = inject(PortfolioService);
  private readonly comptesApi = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly etat = signal<Etat>('chargement');
  protected readonly pret = signal(false);
  protected readonly message = signal('');
  protected readonly modeles = signal<Model[]>([]);
  protected readonly comptes = signal<Account[]>([]);
  protected readonly modeleId = signal('');
  protected readonly choisis = signal<string[]>([]);
  protected readonly exposition = signal<Exposure | null>(null);

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    forkJoin({
      modeles: this.api.listModels(),
      comptes: this.comptesApi.listAccounts(),
    })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: ({ modeles, comptes }) => {
          this.modeles.set(modeles.items);
          this.comptes.set(comptes.items);
          this.pret.set(true);
          this.afficher();
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'pf.expo.erreurChargement'));
          this.etat.set('erreur');
        },
      });
  }

  protected basculer(id: string, coche: boolean): void {
    this.choisis.update((l) => (coche ? [...l.filter((x) => x !== id), id] : l.filter((x) => x !== id)));
  }

  protected afficher(): void {
    this.etat.set('chargement');
    this.api
      .getExposure(this.choisis().length > 0 ? this.choisis() : undefined, this.modeleId() || undefined)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (e) => {
          this.exposition.set(e);
          this.etat.set('pret');
        },
        error: (err: unknown) => {
          this.exposition.set(null);
          this.message.set(messageProbleme(err, 'pf.expo.erreur'));
          this.etat.set('erreur');
        },
      });
  }
}
