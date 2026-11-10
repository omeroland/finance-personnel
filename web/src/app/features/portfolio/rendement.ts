import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Account, AccountsService, Performance, PortfolioService } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { SerieRendement } from './affichages';

type Etat = 'chargement' | 'pret' | 'erreur';
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/**
 * Rendement pondéré dans le temps et TRI, par compte et consolidé. Mensuel et annuel sortent d'une seule série (D381) :
 * le bouton ne fait que choisir la liste à afficher. La base du TRI (D441) est expliquée en texte.
 */
@Component({
  selector: 'app-rendement',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, SerieRendement, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.perf.titre' | t }}</h2>
    <details class="carte" data-testid="perf-explication">
      <summary>{{ 'pf.perf.explicationTitre' | t }}</summary>
      <ul class="liste-simple">
        <li>{{ 'pf.perf.expTwr' | t }}</li>
        <li>{{ 'pf.perf.expIrr' | t }}</li>
        <li>{{ 'pf.perf.base.annualized' | t }}</li>
        <li>{{ 'pf.perf.base.cumulative' | t }}</li>
      </ul>
    </details>
    <form (ngSubmit)="charger()" novalidate>
      <div class="ligne-saisie">
        <mat-form-field>
          <mat-label>{{ 'pf.perf.debut' | t }}</mat-label>
          <input
            matInput
            type="date"
            name="debut"
            data-testid="perf-debut"
            [ngModel]="debut()"
            (ngModelChange)="debut.set($event)"
          />
        </mat-form-field>
        <mat-form-field>
          <mat-label>{{ 'pf.perf.fin' | t }}</mat-label>
          <input
            matInput
            type="date"
            name="fin"
            data-testid="perf-fin"
            [ngModel]="fin()"
            (ngModelChange)="fin.set($event)"
          />
        </mat-form-field>
        <mat-form-field>
          <mat-label>{{ 'pf.perf.granularite' | t }}</mat-label>
          <select
            matNativeControl
            name="granularite"
            data-testid="perf-granularite"
            [ngModel]="granularite()"
            (ngModelChange)="granularite.set($event)"
          >
            <option value="monthly">{{ 'pf.perf.mensuel' | t }}</option>
            <option value="annual">{{ 'pf.perf.annuel' | t }}</option>
          </select>
        </mat-form-field>
      </div>
      <fieldset>
        <legend>{{ 'pf.reeq.comptes' | t }}</legend>
        <p>
          <small>{{ 'pf.expo.aideComptes' | t }}</small>
        </p>
        @for (c of comptes(); track c.id) {
          <label class="statut">
            <input
              type="checkbox"
              [attr.data-testid]="'perf-compte-' + c.id"
              [checked]="choisis().includes(c.id)"
              (change)="basculer(c.id, $any($event.target).checked)"
            />
            {{ c.name }}
          </label>
          <br />
        }
      </fieldset>
      @if (erreurSaisie()) {
        <p class="erreur" role="alert" data-testid="perf-erreur-saisie">
          <span class="icone" aria-hidden="true">✖</span>{{ erreurSaisie() }}
        </p>
      }
      <button mat-flat-button type="submit" data-testid="perf-afficher">
        {{ 'pf.perf.afficher' | t }}
      </button>
    </form>

    @switch (etat()) {
      @case ('chargement') {
        <p role="status" data-testid="perf-chargement">
          {{ 'pf.chargement' | t }}
        </p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="perf-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @default {
        @if (rendement(); as r) {
          <p data-testid="perf-periode">
            {{ 'pf.perf.periode' | t: { debut: r.from, fin: r.to } }}
          </p>
          <app-serie-rendement
            [serie]="r.consolidated"
            [titre]="'pf.perf.consolide' | t"
            testid="perf-consolide"
            [granularite]="granularite()"
          />
          @for (s of r.accounts; track $index) {
            <app-serie-rendement
              [serie]="s"
              [titre]="nomCompte(s.accountId)"
              [testid]="'serie-' + s.accountId"
              [granularite]="granularite()"
            />
          }
        }
      }
    }
  `,
})
export class Rendement {
  private readonly api = inject(PortfolioService);
  private readonly comptesApi = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly comptes = signal<Account[]>([]);
  protected readonly choisis = signal<string[]>([]);
  protected readonly debut = signal('');
  protected readonly fin = signal('');
  protected readonly granularite = signal<'monthly' | 'annual'>('monthly');
  protected readonly erreurSaisie = signal('');
  protected readonly rendement = signal<Performance | null>(null);

  constructor() {
    this.comptesApi
      .listAccounts()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => this.comptes.set(l.items),
        error: () => undefined,
      });
    this.charger();
  }

  protected nomCompte(id: string | undefined): string {
    return this.comptes().find((c) => c.id === id)?.name ?? id ?? '';
  }

  protected basculer(id: string, coche: boolean): void {
    this.choisis.update((l) => (coche ? [...l.filter((x) => x !== id), id] : l.filter((x) => x !== id)));
  }

  protected charger(): void {
    this.erreurSaisie.set('');
    if ((this.debut() && !DATE.test(this.debut())) || (this.fin() && !DATE.test(this.fin()))) {
      this.erreurSaisie.set(traduire('pf.perf.erreurDate'));
      return;
    }
    this.etat.set('chargement');
    this.api
      .getPerformance(
        this.choisis().length > 0 ? this.choisis() : undefined,
        this.debut() || undefined,
        this.fin() || undefined,
      )
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (r) => {
          this.rendement.set(r);
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.rendement.set(null);
          this.message.set(messageProbleme(e, 'pf.perf.erreur'));
          this.etat.set('erreur');
        },
      });
  }
}
