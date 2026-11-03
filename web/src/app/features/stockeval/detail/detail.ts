import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatSnackBar } from '@angular/material/snack-bar';
import { MatTableModule } from '@angular/material/table';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { catchError, forkJoin, of } from 'rxjs';
import { Criterion, CriterionStatus, Evaluation, EvaluationRecord, EvaluationsService, Mode, Verdict } from '../../../api';
import { TraductionPipe, traduire } from '../../../i18n/t.pipe';
import { Comparaison, Ecart, ICONES_STATUT, ICONES_VERDICT, comparaisons, formaterValeur } from './detail.data';

const DATE_HEURE = new Intl.DateTimeFormat('fr-CA', { dateStyle: 'medium', timeStyle: 'short' });

type Etat = 'chargement' | 'pret' | 'erreur';

/** Fiche d'une action : résultat règle par règle (deux profils de seuils), comparaison corrigé/compat, historique archivé. */
@Component({
  selector: 'app-detail',
  imports: [MatButtonModule, MatTableModule, MatTabsModule, MatTooltipModule, RouterLink, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './detail.html',
  styleUrl: './detail.scss',
})
export class Detail {
  private readonly api = inject(EvaluationsService);
  private readonly snack = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly symbole = inject(ActivatedRoute).snapshot.paramMap.get('symbol') ?? '';
  protected readonly colonnes = ['critere', 'valeur', 'strict', 'assoupli', 'statut'];
  protected readonly colonnesComparaison = ['critere', 'corrige', 'compat', 'ecart'];
  protected readonly colonnesHistorique = ['date', 'mode', 'regles', 'scores'];

  protected readonly etat = signal<Etat>('chargement');
  protected readonly evaluation = signal<Evaluation | null>(null);
  /** Évaluation en mode compat ; null si elle n'a pas pu être chargée. */
  protected readonly compat = signal<Evaluation | null>(null);
  protected readonly comparaisons = computed<Comparaison[]>(() => {
    const e = this.evaluation();
    return e ? comparaisons(e, this.compat()) : [];
  });

  protected readonly historique = signal<EvaluationRecord[]>([]);
  protected readonly etatHistorique = signal<Etat>('chargement');
  protected readonly archivage = signal(false);

  constructor() {
    this.charger();
    this.chargerHistorique();
  }

  protected charger(): void {
    this.etat.set('chargement');
    forkJoin({
      corrige: this.api.getEvaluation(this.symbole, Mode.corrected),
      compat: this.api.getEvaluation(this.symbole, Mode.compat).pipe(catchError(() => of(null))),
    })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: ({ corrige, compat }) => {
          this.evaluation.set(corrige);
          this.compat.set(compat);
          this.etat.set('pret');
        },
        error: () => this.etat.set('erreur'),
      });
  }

  protected chargerHistorique(): void {
    this.etatHistorique.set('chargement');
    this.api
      .listEvaluations(this.symbole)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (liste) => {
          this.historique.set(liste.items);
          this.etatHistorique.set('pret');
        },
        error: () => this.etatHistorique.set('erreur'),
      });
  }

  protected archiver(): void {
    this.archivage.set(true);
    this.api
      .createEvaluation(this.symbole, { mode: Mode.corrected })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (enregistrement) => {
          this.historique.update((l) => [enregistrement, ...l]);
          this.archivage.set(false);
          this.snack.open(traduire('detail.archivee'), traduire('liste.fermer'), { duration: 5000 });
        },
        error: () => {
          this.archivage.set(false);
          this.snack.open(traduire('detail.archiverErreur'), traduire('liste.fermer'), { duration: 8000 });
        },
      });
  }

  protected valeur(c: Criterion): string {
    return formaterValeur(c.id, c.value) ?? traduire('detail.sansValeur');
  }

  protected libelleCritere(c: Criterion): string {
    return `${c.id} — ${c.label ?? ''}`;
  }

  protected libelleStatut(s: CriterionStatus): string {
    return traduire(`statut.${s}`);
  }

  protected explicationStatut(s: CriterionStatus): string {
    return traduire(`statut.${s}.explication`);
  }

  protected iconeStatut(s: CriterionStatus): string {
    return ICONES_STATUT[s];
  }

  protected libelleVerdict(v: Verdict | undefined): string {
    return v ? traduire(`verdict.${v}`) : traduire('verdict.indicatif');
  }

  protected iconeVerdict(v: Verdict | undefined): string {
    return v ? ICONES_VERDICT[v] : '–';
  }

  protected date(iso: string): string {
    return DATE_HEURE.format(new Date(iso));
  }

  protected libelleMode(m: Mode): string {
    return traduire(m === Mode.compat ? 'mode.compat' : 'mode.corrected');
  }

  protected libelleEcart(e: Ecart | null): string {
    return e ? traduire(`ecart.${e}`) : traduire('ecart.absent');
  }
}
