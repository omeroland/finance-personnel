import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { Currency, Problem, Stock, StocksService } from '../../../api';
import { TraductionPipe, traduire } from '../../../i18n/t.pipe';

/** Même motif que le schéma `Symbol` du contrat ; la validation côté serveur reste la référence. */
export const MOTIF_SYMBOLE = /^[A-Z]{2,10}:[A-Z0-9.-]{1,15}$/;

/** Message affiché pour une erreur de l'API (RFC 9457) : champs en erreur si présents, sinon texte générique. */
export function messageErreur(e: unknown): string {
  if (e instanceof HttpErrorResponse) {
    if (e.status === 409) {
      return traduire('ajout.conflit');
    }
    const p = e.error as Partial<Problem> | null;
    const champs = p?.errors?.map((x) => `${x.field} : ${x.message}`).join(' ; ');
    if (champs) {
      return champs;
    }
    if (p?.detail) {
      return p.detail;
    }
  }
  return traduire('ajout.erreur');
}

/** Dialogue d'ajout d'une action ; se ferme avec l'action créée. */
@Component({
  selector: 'app-ajout-dialog',
  imports: [
    MatButtonModule,
    MatCheckboxModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    ReactiveFormsModule,
    TraductionPipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h2 mat-dialog-title>{{ 'ajout.titre' | t }}</h2>
    <form [formGroup]="form" (ngSubmit)="soumettre()">
      <mat-dialog-content>
        <mat-form-field class="plein">
          <mat-label>{{ 'ajout.symbole' | t }}</mat-label>
          <input matInput formControlName="symbol" autocomplete="off" data-testid="ajout-symbole" />
          <mat-hint>{{ 'ajout.symboleAide' | t }}</mat-hint>
          @if (form.controls.symbol.invalid) {
            <mat-error>{{ 'ajout.symboleInvalide' | t }}</mat-error>
          }
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'ajout.nom' | t }}</mat-label>
          <input matInput formControlName="name" autocomplete="off" data-testid="ajout-nom" />
          @if (form.controls.name.invalid) {
            <mat-error>{{ 'ajout.nomRequis' | t }}</mat-error>
          }
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'ajout.devise' | t }}</mat-label>
          <mat-select formControlName="quoteCurrency" data-testid="ajout-devise">
            @for (d of devises; track d) {
              <mat-option [value]="d">{{ d }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'ajout.deviseRapport' | t }}</mat-label>
          <mat-select formControlName="reportingCurrency" data-testid="ajout-devise-rapport">
            @for (d of devises; track d) {
              <mat-option [value]="d">{{ d }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
        <mat-checkbox formControlName="isFinancial" data-testid="ajout-financiere">{{ 'ajout.financiere' | t }}</mat-checkbox>
        @if (erreur()) {
          <p class="erreur" role="alert" data-testid="ajout-erreur">{{ erreur() }}</p>
        }
      </mat-dialog-content>
      <mat-dialog-actions align="end">
        <button mat-button type="button" mat-dialog-close data-testid="ajout-annuler">{{ 'dialogue.annuler' | t }}</button>
        <button mat-flat-button type="submit" [disabled]="envoi()" data-testid="ajout-confirmer">
          {{ 'ajout.confirmer' | t }}
        </button>
      </mat-dialog-actions>
    </form>
  `,
  styles: `
    .plein {
      width: 100%;
    }
    .erreur {
      color: var(--mat-sys-error);
      margin-block-start: 0.75rem;
    }
  `,
})
export class AjoutDialog {
  private readonly api = inject(StocksService);
  private readonly ref = inject<MatDialogRef<AjoutDialog, Stock>>(MatDialogRef);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly devises = Object.values(Currency);
  protected readonly erreur = signal('');
  protected readonly envoi = signal(false);

  protected readonly form = this.fb.group({
    symbol: ['', [Validators.required, Validators.pattern(MOTIF_SYMBOLE)]],
    name: ['', [Validators.required, Validators.maxLength(200)]],
    quoteCurrency: [Currency.CAD, Validators.required],
    reportingCurrency: [Currency.CAD, Validators.required],
    isFinancial: [false],
  });

  protected soumettre(): void {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    this.erreur.set('');
    this.envoi.set(true);
    const v = this.form.getRawValue();
    this.api.addStock({ ...v, symbol: v.symbol.trim(), name: v.name.trim() }).subscribe({
      next: (stock) => this.ref.close(stock),
      error: (e: unknown) => {
        this.envoi.set(false);
        this.erreur.set(messageErreur(e));
      },
    });
  }
}
