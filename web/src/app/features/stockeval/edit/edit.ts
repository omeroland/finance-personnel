import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatSnackBar } from '@angular/material/snack-bar';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { Observable, catchError, of, switchMap, tap } from 'rxjs';
import { AnnualFact, Evaluation, EvaluationsService, Mode, Qualitative, Snapshot, StockDetail, StocksService, Verdict } from '../../../api';
import { CleTraduction, TraductionPipe, traduire } from '../../../i18n/t.pipe';
import { ICONES_VERDICT } from '../detail/detail.data';
import { annees, lignesGrille } from './edit.data';
import { GrilleAnnuelle } from './grille';
import { Instantane } from './instantane';
import { Qualitatif } from './qualitatif';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Saisie d'une action sans la feuille : séries annuelles, instantané et règles 1 à 4. Le score affiché vient toujours de l'API. */
@Component({
  selector: 'app-edit',
  imports: [GrilleAnnuelle, Instantane, MatButtonModule, Qualitatif, RouterLink, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './edit.html',
  styleUrl: './edit.scss',
})
export class Edit {
  private readonly stocks = inject(StocksService);
  private readonly evaluations = inject(EvaluationsService);
  private readonly snack = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly symbole = inject(ActivatedRoute).snapshot.paramMap.get('symbol') ?? '';
  protected readonly etat = signal<Etat>('chargement');
  protected readonly fiche = signal<StockDetail | null>(null);
  protected readonly evaluation = signal<Evaluation | null>(null);
  protected readonly enregistrement = signal(false);

  protected readonly annees = computed(() => annees(this.fiche()?.facts ?? [], new Date().getFullYear()));
  protected readonly lignes = computed(() => lignesGrille(this.fiche()?.facts ?? [], this.fiche()?.conflicts ?? []));

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.stocks
      .getStock(this.symbole)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (fiche) => {
          this.fiche.set(fiche);
          this.etat.set('pret');
          this.chargerScore();
        },
        error: () => this.etat.set('erreur'),
      });
  }

  private chargerScore(): void {
    this.evaluations
      .getEvaluation(this.symbole, Mode.corrected)
      .pipe(
        catchError(() => of(null)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe((e) => this.evaluation.set(e));
  }

  /** Enregistre puis recharge la fiche (sources, conflits) et le score calculé par l'API. */
  private enregistrer(ecriture: Observable<unknown>, message: CleTraduction): void {
    this.enregistrement.set(true);
    ecriture
      .pipe(
        switchMap(() => this.stocks.getStock(this.symbole)),
        tap((fiche) => this.fiche.set(fiche)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        next: () => {
          this.enregistrement.set(false);
          this.snack.open(traduire(message), traduire('liste.fermer'), { duration: 5000 });
          this.chargerScore();
        },
        error: () => {
          this.enregistrement.set(false);
          this.snack.open(traduire('edit.erreur.enregistrement'), traduire('liste.fermer'), { duration: 8000 });
        },
      });
  }

  protected enregistrerFaits(faits: AnnualFact[]): void {
    if (faits.length) {
      this.enregistrer(this.stocks.replaceFacts(this.symbole, { facts: faits }), 'edit.enregistre.faits');
    }
  }

  /** Résoudre un conflit : la valeur choisie est réécrite en source `manual`, qui prime sur les fournisseurs. */
  protected resoudre(fait: AnnualFact): void {
    this.enregistrer(this.stocks.replaceFacts(this.symbole, { facts: [fait] }), 'edit.enregistre.conflit');
  }

  protected enregistrerInstantane(snapshot: Snapshot): void {
    this.enregistrer(this.stocks.updateStock(this.symbole, { snapshot }), 'edit.enregistre.instantane');
  }

  protected enregistrerQualitatif(q: Qualitative): void {
    this.enregistrer(this.stocks.replaceQualitative(this.symbole, q), 'edit.enregistre.qualitatif');
  }

  protected libelleVerdict(v: Verdict | undefined): string {
    return v ? traduire(`verdict.${v}`) : traduire('verdict.indicatif');
  }

  protected iconeVerdict(v: Verdict | undefined): string {
    return v ? ICONES_VERDICT[v] : '–';
  }
}
