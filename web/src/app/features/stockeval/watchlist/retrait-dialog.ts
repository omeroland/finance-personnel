import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule } from '@angular/material/dialog';
import { TraductionPipe } from '../../../i18n/t.pipe';

/** Confirmation du retrait d'une action ; se ferme avec `true` si confirmé. */
@Component({
  selector: 'app-retrait-dialog',
  imports: [MatButtonModule, MatDialogModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h2 mat-dialog-title>{{ 'retirer.titre' | t }}</h2>
    <mat-dialog-content>{{ 'retirer.message' | t: { symbole: data.symbole } }}</mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button type="button" mat-dialog-close data-testid="retrait-annuler">{{ 'dialogue.annuler' | t }}</button>
      <button mat-flat-button type="button" [mat-dialog-close]="true" data-testid="retrait-confirmer">
        {{ 'retirer.confirmer' | t }}
      </button>
    </mat-dialog-actions>
  `,
})
export class RetraitDialog {
  protected readonly data = inject<{ symbole: string }>(MAT_DIALOG_DATA);
}
