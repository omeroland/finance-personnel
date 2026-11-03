import { ChangeDetectionStrategy, Component, effect, inject, input, output } from '@angular/core';
import { AbstractControl, FormBuilder, ReactiveFormsModule, ValidationErrors, ValidatorFn } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { Currency, Snapshot } from '../../../api';
import { CleTraduction, TraductionPipe, traduire } from '../../../i18n/t.pipe';
import { Bornes, validerDecimal } from './edit.data';

/** Champs montants de l'instantané et leurs bornes (les capitaux propres peuvent être négatifs, D2). */
export const CHAMPS_MONTANTS = [
  { nom: 'price', bornes: { min: 0 } },
  { nom: 'annualDividend', bornes: { min: 0 } },
  { nom: 'longTermDebt', bornes: { min: 0 } },
  { nom: 'equity', bornes: {} },
  { nom: 'bookValuePerShare', bornes: {} },
] as const satisfies readonly { nom: string; bornes: Bornes }[];

const BORNES_FX: Bornes = { min: 0, minExclu: true, max: 1000 };

function validateur(bornes: Bornes): ValidatorFn {
  return (c: AbstractControl): ValidationErrors | null => {
    const texte = String(c.value ?? '').trim();
    if (texte === '') {
      return null;
    }
    const r = validerDecimal(texte, bornes);
    return 'erreur' in r ? { saisie: r.erreur } : null;
  };
}

function dateDuJour(): string {
  const d = new Date();
  const deux = (n: number): string => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${deux(d.getMonth() + 1)}-${deux(d.getDate())}`;
}

/** Formulaire de l'instantané : prix, dividende prévu, dette LT, capitaux propres, valeur comptable, taux de change. */
@Component({
  selector: 'app-instantane',
  imports: [MatButtonModule, MatFormFieldModule, MatInputModule, MatSelectModule, ReactiveFormsModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: `
    .champs {
      display: flex;
      flex-wrap: wrap;
      gap: 0 1rem;
    }
  `,
  template: `
    <h2>{{ 'edit.instantane.titre' | t }}</h2>
    <form [formGroup]="form" (ngSubmit)="envoyer()" data-testid="instantane">
      <div class="champs">
        <mat-form-field>
          <mat-label>{{ 'edit.instantane.devise' | t }}</mat-label>
          <mat-select formControlName="currency" data-testid="inst-currency">
            @for (d of devises; track d) {
              <mat-option [value]="d">{{ d }}</mat-option>
            }
          </mat-select>
        </mat-form-field>
        @for (champ of champs; track champ.nom) {
          <mat-form-field>
            <mat-label>{{ libelle(champ.nom) }}</mat-label>
            <input matInput inputmode="decimal" autocomplete="off" [formControlName]="champ.nom" [attr.data-testid]="'inst-' + champ.nom" />
            @if (form.controls[champ.nom].errors?.['saisie']; as cle) {
              <mat-error>{{ erreur(cle) }}</mat-error>
            }
          </mat-form-field>
        }
        <mat-form-field>
          <mat-label>{{ 'edit.instantane.fxRate' | t }}</mat-label>
          <input matInput inputmode="decimal" autocomplete="off" formControlName="fxRate" data-testid="inst-fxRate" />
          <mat-hint>{{ 'edit.instantane.fxAide' | t }}</mat-hint>
          @if (form.controls.fxRate.errors?.['saisie']; as cle) {
            <mat-error>{{ erreur(cle) }}</mat-error>
          }
        </mat-form-field>
      </div>
      <button mat-flat-button type="submit" data-testid="enregistrer-instantane" [disabled]="form.invalid || enregistrement()">
        {{ 'edit.instantane.enregistrer' | t }}
      </button>
    </form>
  `,
})
export class Instantane {
  private readonly fb = inject(FormBuilder);

  readonly snapshot = input<Snapshot | undefined>();
  readonly deviseDefaut = input.required<Currency>();
  readonly enregistrement = input(false);
  readonly enregistrerInstantane = output<Snapshot>();

  protected readonly devises = Object.values(Currency);
  protected readonly champs = CHAMPS_MONTANTS;
  protected readonly form = this.fb.nonNullable.group({
    currency: this.fb.nonNullable.control<Currency>(Currency.CAD),
    price: [''],
    annualDividend: [''],
    longTermDebt: [''],
    equity: [''],
    bookValuePerShare: [''],
    fxRate: [''],
  });

  constructor() {
    for (const c of CHAMPS_MONTANTS) {
      this.form.controls[c.nom].addValidators(validateur(c.bornes));
    }
    this.form.controls.fxRate.addValidators(validateur(BORNES_FX));
    effect(() => {
      const s = this.snapshot();
      const montant = (v: string | undefined): string => (v ?? '').replace('.', ',');
      this.form.reset({
        currency: s?.currency ?? this.deviseDefaut(),
        price: montant(s?.price),
        annualDividend: montant(s?.annualDividend),
        longTermDebt: montant(s?.longTermDebt),
        equity: montant(s?.equity),
        bookValuePerShare: montant(s?.bookValuePerShare),
        fxRate: s?.fxRate === undefined ? '' : String(s.fxRate).replace('.', ','),
      });
    });
  }

  protected libelle(nom: (typeof CHAMPS_MONTANTS)[number]['nom']): string {
    return traduire(`edit.instantane.${nom}`);
  }

  protected erreur(cle: string): string {
    return traduire(cle as CleTraduction);
  }

  protected envoyer(): void {
    if (this.form.invalid) {
      return;
    }
    const v = this.form.getRawValue();
    const decimal = (t: string): string | undefined => {
      const r = validerDecimal(t.trim(), {});
      return t.trim() && 'valeur' in r ? r.valeur : undefined;
    };
    const fx = decimal(v.fxRate);
    this.enregistrerInstantane.emit({
      currency: v.currency,
      price: decimal(v.price),
      annualDividend: decimal(v.annualDividend),
      longTermDebt: decimal(v.longTermDebt),
      equity: decimal(v.equity),
      bookValuePerShare: decimal(v.bookValuePerShare),
      fxRate: fx === undefined ? undefined : Number(fx),
      asOf: dateDuJour(),
      source: 'manual',
    });
  }
}
