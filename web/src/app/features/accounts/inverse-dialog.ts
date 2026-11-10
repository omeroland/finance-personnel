import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule } from '@angular/material/dialog';
import { Transaction } from '../../api';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';

/** Confirmation d'une écriture inverse ; se ferme avec `true` si confirmée. */
@Component({
  selector: 'app-inverse-dialog',
  imports: [MatButtonModule, MatDialogModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h2 mat-dialog-title>{{ 'reg.inverse.titre' | t }}</h2>
    <mat-dialog-content>
      {{ 'reg.inverse.message' | t: { type: libelle, date: data.transaction.tradeDate } }}
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button type="button" mat-dialog-close data-testid="inverse-annuler">
        {{ 'dialogue.annuler' | t }}
      </button>
      <button mat-flat-button type="button" [mat-dialog-close]="true" data-testid="inverse-confirmer">
        {{ 'reg.inverse.confirmer' | t }}
      </button>
    </mat-dialog-actions>
  `,
})
export class InverseDialog {
  protected readonly data = inject<{ transaction: Transaction }>(MAT_DIALOG_DATA);
  protected readonly libelle = traduire(`reg.type.${this.data.transaction.type}`);
}
