import { ChangeDetectionStrategy, Component } from '@angular/core';
import { TraductionPipe } from '../../i18n/t.pipe';

/** Page d'attente du module 1 ; la liste de suivi arrive avec P1-15. */
@Component({
  selector: 'app-stockeval-accueil',
  imports: [TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <h1>{{ 'stockeval.titre' | t }}</h1>
    <p>{{ 'stockeval.intro' | t }}</p>
  `,
})
export class Accueil {}
