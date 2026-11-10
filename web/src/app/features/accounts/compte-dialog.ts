import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { Account, AccountType, AccountsService, Currency } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';

/** Dialogue de création d'un compte ; se ferme avec le compte créé. */
@Component({
  selector: 'app-compte-dialog',
  imports: [
    MatButtonModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    ReactiveFormsModule,
    TraductionPipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h2 mat-dialog-title>{{ 'cpt.creer' | t }}</h2>
    <form [formGroup]="form" (ngSubmit)="soumettre()">
      <mat-dialog-content>
        <mat-form-field class="plein">
          <mat-label>{{ 'cpt.col.nom' | t }}</mat-label>
          <input matInput formControlName="name" autocomplete="off" maxlength="100" data-testid="compte-nom" />
          @if (form.controls.name.invalid) {
            <mat-error>{{ 'cpt.nomRequis' | t }}</mat-error>
          }
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'cpt.col.type' | t }}</mat-label>
          <mat-select formControlName="type" data-testid="compte-type">
            @for (t of types; track t) {
              <mat-option [value]="t">{{ libelleType(t) }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'cpt.col.courtier' | t }}</mat-label>
          <input matInput formControlName="broker" autocomplete="off" maxlength="100" data-testid="compte-courtier" />
          @if (form.controls.broker.invalid) {
            <mat-error>{{ 'cpt.courtierRequis' | t }}</mat-error>
          }
        </mat-form-field>
        <mat-form-field class="plein">
          <mat-label>{{ 'cpt.col.devise' | t }}</mat-label>
          <mat-select formControlName="currency" data-testid="compte-devise">
            @for (d of devises; track d) {
              <mat-option [value]="d">{{ d }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
        @if (erreur()) {
          <p class="erreur" role="alert" data-testid="compte-erreur">
            {{ erreur() }}
          </p>
        }
      </mat-dialog-content>
      <mat-dialog-actions align="end">
        <button mat-button type="button" mat-dialog-close data-testid="compte-annuler">
          {{ 'dialogue.annuler' | t }}
        </button>
        <button mat-flat-button type="submit" [disabled]="envoi()" data-testid="compte-confirmer">
          {{ 'cpt.confirmer' | t }}
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
    }
  `,
})
export class CompteDialog {
  private readonly api = inject(AccountsService);
  private readonly ref = inject<MatDialogRef<CompteDialog, Account>>(MatDialogRef);
  private readonly fb = inject(FormBuilder).nonNullable;

  protected readonly types = Object.values(AccountType);
  protected readonly devises = Object.values(Currency);
  protected readonly erreur = signal('');
  protected readonly envoi = signal(false);

  protected readonly form = this.fb.group({
    name: ['', [Validators.required, Validators.maxLength(100)]],
    type: [AccountType.celi, Validators.required],
    broker: ['', [Validators.required, Validators.maxLength(100)]],
    currency: [Currency.CAD, Validators.required],
  });

  protected libelleType(t: AccountType): string {
    return traduire(`cpt.type.${t}`);
  }

  protected soumettre(): void {
    const v = this.form.getRawValue();
    if (this.form.invalid || v.name.trim() === '' || v.broker.trim() === '') {
      this.form.markAllAsTouched();
      return;
    }
    this.erreur.set('');
    this.envoi.set(true);
    this.api.createAccount({ ...v, name: v.name.trim(), broker: v.broker.trim() }).subscribe({
      next: (c) => this.ref.close(c),
      error: (e: unknown) => {
        this.envoi.set(false);
        this.erreur.set(messageProbleme(e, 'cpt.erreurCreation'));
      },
    });
  }
}
