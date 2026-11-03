import { ChangeDetectionStrategy, Component, effect, inject, input, output } from '@angular/core';
import { FormBuilder, ReactiveFormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatRadioModule } from '@angular/material/radio';
import { Answer, Qualitative, RuleAnswer } from '../../../api';
import { TraductionPipe, traduire } from '../../../i18n/t.pipe';

const DATE_HEURE = new Intl.DateTimeFormat('fr-CA', { dateStyle: 'medium', timeStyle: 'short' });

type NomRegle = 'rule1' | 'rule2' | 'rule3' | 'rule4';
const REGLES: readonly NomRegle[] = ['rule1', 'rule2', 'rule3', 'rule4'];
/** Rachat manuel : « non saisi » est l'absence de valeur (règle 10). */
type Rachat = 'inconnu' | 'oui' | 'non';

/** Règles 1 à 4 : oui / non / vide, note et date de revue (fixée par le serveur). */
@Component({
  selector: 'app-qualitatif',
  imports: [MatButtonModule, MatFormFieldModule, MatInputModule, MatRadioModule, ReactiveFormsModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: `
    fieldset {
      margin-block: 1rem;
      border: 1px solid var(--mat-sys-outline-variant);
    }
    textarea {
      min-width: 24rem;
    }
  `,
  template: `
    <h2>{{ 'edit.qualitatif.titre' | t }}</h2>
    <p class="note" data-testid="revue">
      {{ qualitatif()?.reviewedAt ? ('edit.qualitatif.revue' | t: { date: dateRevue() }) : ('edit.qualitatif.jamais' | t) }}
    </p>
    <form [formGroup]="form" (ngSubmit)="envoyer()" data-testid="qualitatif">
      @for (r of regles; track r) {
        <fieldset [formGroupName]="r">
          <legend>{{ 'edit.regle' | t: { n: r.slice(4) } }}</legend>
          <mat-radio-group formControlName="answer" [attr.aria-label]="'edit.regle' | t: { n: r.slice(4) }">
            <mat-radio-button value="yes" [attr.data-testid]="r + '-yes'">{{ 'edit.reponse.yes' | t }}</mat-radio-button>
            <mat-radio-button value="no" [attr.data-testid]="r + '-no'">{{ 'edit.reponse.no' | t }}</mat-radio-button>
            <mat-radio-button value="unanswered" [attr.data-testid]="r + '-unanswered'">{{ 'edit.reponse.unanswered' | t }}</mat-radio-button>
          </mat-radio-group>
          <mat-form-field>
            <mat-label>{{ 'edit.note' | t }}</mat-label>
            <textarea matInput rows="2" maxlength="2000" formControlName="note" [attr.data-testid]="r + '-note'"></textarea>
          </mat-form-field>
        </fieldset>
      }
      <fieldset>
        <legend>{{ 'edit.rachat' | t }}</legend>
        <mat-radio-group formControlName="rachat" [attr.aria-label]="'edit.rachat' | t">
          <mat-radio-button value="inconnu">{{ 'edit.rachat.inconnu' | t }}</mat-radio-button>
          <mat-radio-button value="oui">{{ 'edit.reponse.yes' | t }}</mat-radio-button>
          <mat-radio-button value="non">{{ 'edit.reponse.no' | t }}</mat-radio-button>
        </mat-radio-group>
      </fieldset>
      <button mat-flat-button type="submit" data-testid="enregistrer-qualitatif" [disabled]="enregistrement()">
        {{ 'edit.qualitatif.enregistrer' | t }}
      </button>
    </form>
  `,
})
export class Qualitatif {
  private readonly fb = inject(FormBuilder);

  readonly qualitatif = input<Qualitative | undefined>();
  readonly enregistrement = input(false);
  readonly enregistrerQualitatif = output<Qualitative>();

  protected readonly regles = REGLES;
  private groupe() {
    return this.fb.nonNullable.group({ answer: 'unanswered' as Answer, note: '' });
  }
  protected readonly form = this.fb.nonNullable.group({
    rule1: this.groupe(),
    rule2: this.groupe(),
    rule3: this.groupe(),
    rule4: this.groupe(),
    rachat: 'inconnu' as Rachat,
  });

  constructor() {
    effect(() => {
      const q = this.qualitatif();
      const lire = (r?: RuleAnswer) => ({ answer: r?.answer ?? ('unanswered' as Answer), note: r?.note ?? '' });
      this.form.reset({
        rule1: lire(q?.rule1),
        rule2: lire(q?.rule2),
        rule3: lire(q?.rule3),
        rule4: lire(q?.rule4),
        rachat: q?.manualBuyback === undefined ? 'inconnu' : q.manualBuyback ? 'oui' : 'non',
      });
    });
  }

  protected dateRevue(): string {
    const iso = this.qualitatif()?.reviewedAt;
    return iso ? DATE_HEURE.format(new Date(iso)) : traduire('edit.qualitatif.jamais');
  }

  protected envoyer(): void {
    const v = this.form.getRawValue();
    const regle = (r: { answer: Answer; note: string }): RuleAnswer => ({ answer: r.answer, ...(r.note.trim() ? { note: r.note.trim() } : {}) });
    this.enregistrerQualitatif.emit({
      rule1: regle(v.rule1),
      rule2: regle(v.rule2),
      rule3: regle(v.rule3),
      rule4: regle(v.rule4),
      ...(v.rachat === 'inconnu' ? {} : { manualBuyback: v.rachat === 'oui' }),
    });
  }
}
