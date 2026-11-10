import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Account, Statement, StatementCreate, TransactionsService } from '../../api';
import { afficherDecimal, messageProbleme } from '../../core/probleme';
import { CleTraduction, TraductionPipe } from '../../i18n/t.pipe';
import { normaliserDecimal } from '../stockeval/edit/edit.data';

const MOIS = /^\d{4}-(0[1-9]|1[0-2])$/;

/** Saisie d'un relevé de valeur mensuel. L'API n'a pas de lecture des relevés : la liste ne garde que ceux de la session. */
@Component({
  selector: 'app-releves',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './accounts.scss',
  template: `
    <h2>{{ 'rel.titre' | t }}</h2>
    <p>{{ 'rel.intro' | t }}</p>
    <form class="formulaire" (ngSubmit)="enregistrer()" [attr.aria-label]="'rel.titre' | t">
      <mat-form-field>
        <mat-label>{{ 'rel.mois' | t }}</mat-label>
        <input matInput type="month" name="mois" data-testid="rel-mois" [(ngModel)]="mois" />
        <mat-hint align="end">{{ 'rel.moisAide' | t }}</mat-hint>
        @if (erreurs().mois; as cle) {
          <mat-hint class="erreur" role="alert"><span class="icone" aria-hidden="true">✖</span>{{ cle | t }}</mat-hint>
        }
      </mat-form-field>
      <mat-form-field>
        <mat-label>{{ 'rel.valeur' | t }}</mat-label>
        <input
          matInput
          inputmode="decimal"
          autocomplete="off"
          name="valeur"
          data-testid="rel-valeur"
          [(ngModel)]="valeur"
        />
        @if (erreurs().valeur; as cle) {
          <mat-hint class="erreur" role="alert"><span class="icone" aria-hidden="true">✖</span>{{ cle | t }}</mat-hint>
        }
      </mat-form-field>
      <mat-form-field>
        <mat-label>{{ 'rel.cotisation' | t }}</mat-label>
        <input
          matInput
          inputmode="decimal"
          autocomplete="off"
          name="cotisation"
          data-testid="rel-cotisation"
          [(ngModel)]="cotisation"
        />
        <mat-hint align="end">{{ 'rel.cotisationAide' | t }}</mat-hint>
        @if (erreurs().cotisation; as cle) {
          <mat-hint class="erreur" role="alert"><span class="icone" aria-hidden="true">✖</span>{{ cle | t }}</mat-hint>
        }
      </mat-form-field>
      <p class="champ">{{ 'reg.devise' | t: { devise: compte().currency } }}</p>
      <div class="champ">
        <button mat-flat-button type="submit" data-testid="rel-enregistrer" [disabled]="envoi()">
          {{ 'rel.enregistrer' | t }}
        </button>
      </div>
    </form>
    @if (erreurEnvoi()) {
      <p class="erreur" role="alert" data-testid="rel-erreur">
        <span class="icone" aria-hidden="true">✖</span>{{ erreurEnvoi() }}
      </p>
    }
    @if (saisis().length > 0) {
      <h3>{{ 'rel.session' | t }}</h3>
      <table class="simple" data-testid="rel-liste">
        <thead>
          <tr>
            <th scope="col">{{ 'rel.mois' | t }}</th>
            <th scope="col" class="num">{{ 'rel.valeur' | t }}</th>
            <th scope="col" class="num">{{ 'rel.cotisation' | t }}</th>
          </tr>
        </thead>
        <tbody>
          @for (s of saisis(); track s.id) {
            <tr>
              <td>{{ s.month }}</td>
              <td class="num">{{ montant(s.endValue) }} {{ s.currency }}</td>
              <td class="num">{{ montant(s.contribution) }}</td>
            </tr>
          }
        </tbody>
      </table>
    }
  `,
})
export class Releves {
  private readonly api = inject(TransactionsService);

  readonly compte = input.required<Account>();

  protected mois = '';
  protected valeur = '';
  protected cotisation = '';
  protected readonly erreurs = signal<Partial<Record<'mois' | 'valeur' | 'cotisation', CleTraduction>>>({});
  protected readonly erreurEnvoi = signal('');
  protected readonly envoi = signal(false);
  protected readonly saisis = signal<Statement[]>([]);

  protected montant(v: string | undefined): string {
    return afficherDecimal(v);
  }

  protected enregistrer(): void {
    const erreurs: Partial<Record<'mois' | 'valeur' | 'cotisation', CleTraduction>> = {};
    if (!MOIS.test(this.mois)) {
      erreurs.mois = 'rel.erreur.mois';
    }
    const v = normaliserDecimal(this.valeur);
    if (v === null || v.startsWith('-')) {
      erreurs.valeur = 'rel.erreur.valeur';
    }
    const c = this.cotisation.trim() === '' ? undefined : normaliserDecimal(this.cotisation);
    if (c === null) {
      erreurs.cotisation = 'rel.erreur.cotisation';
    }
    this.erreurs.set(erreurs);
    if (Object.keys(erreurs).length > 0 || v === null) {
      return;
    }
    const requete: StatementCreate = {
      month: this.mois,
      endValue: v,
      currency: this.compte().currency,
    };
    if (c) {
      requete.contribution = c;
    }
    this.erreurEnvoi.set('');
    this.envoi.set(true);
    this.api.createStatement(this.compte().id, requete).subscribe({
      next: (s) => {
        this.envoi.set(false);
        this.saisis.update((l) => [s, ...l]);
        this.mois = '';
        this.valeur = '';
        this.cotisation = '';
      },
      error: (e: unknown) => {
        this.envoi.set(false);
        this.erreurEnvoi.set(messageProbleme(e, 'rel.erreur.envoi'));
      },
    });
  }
}
