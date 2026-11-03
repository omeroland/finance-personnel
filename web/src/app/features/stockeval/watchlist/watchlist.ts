import { ChangeDetectionStrategy, Component, DestroyRef, effect, inject, signal, viewChild } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar } from '@angular/material/snack-bar';
import { MatSort, MatSortModule } from '@angular/material/sort';
import { MatTableDataSource, MatTableModule } from '@angular/material/table';
import { RouterLink } from '@angular/router';
import { Exchange, Horizon, Mode, StocksService, Verdict } from '../../../api';
import { TraductionPipe, traduire } from '../../../i18n/t.pipe';
import { AjoutDialog } from './ajout-dialog';
import { RetraitDialog } from './retrait-dialog';
import { HORIZONS, HORIZON_COMPAT, Ligne, ORDRE_VERDICT, chargerLignes } from './watchlist.data';

/** Pictogrammes textuels des verdicts : toujours accompagnés du libellé, jamais la couleur seule. */
const ICONES: Record<Verdict, string> = {
  [Verdict.candidate]: '✔',
  [Verdict.watch]: '◐',
  [Verdict.rejected]: '✖',
  [Verdict.excluded]: '⊘',
};

type Etat = 'chargement' | 'pret' | 'erreur';

/** Liste de suivi du module 1 : tableau triable, filtres, ajout et retrait. Aucune valeur n'est calculée ici. */
@Component({
  selector: 'app-watchlist',
  imports: [MatButtonModule, MatFormFieldModule, MatSelectModule, MatSortModule, MatTableModule, RouterLink, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './watchlist.html',
  styleUrl: './watchlist.scss',
})
export class Watchlist {
  private readonly api = inject(StocksService);
  private readonly dialog = inject(MatDialog);
  private readonly snack = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly colonnes = ['symbole', 'nom', 'strict', 'assoupli', 'qualitePrix', 'verdict', 'horizon', 'actions'];
  protected readonly bourses = Object.values(Exchange);
  protected readonly verdicts = Object.values(Verdict);
  protected readonly modes = Object.values(Mode);
  protected readonly horizons = HORIZONS;

  protected readonly mode = signal<Mode>(Mode.corrected);
  protected readonly horizon = signal<Horizon>(Horizon.NUMBER_10);
  protected readonly bourse = signal<Exchange | ''>('');
  protected readonly verdict = signal<Verdict | ''>('');
  protected readonly etat = signal<Etat>('chargement');
  protected readonly nbLignes = signal(0);

  protected readonly donnees = new MatTableDataSource<Ligne>([]);
  private readonly tri = viewChild(MatSort);

  constructor() {
    this.donnees.sortingDataAccessor = (l, colonne) => {
      switch (colonne) {
        case 'symbole':
          return l.stock.symbol;
        case 'nom':
          return l.stock.name.toLocaleLowerCase('fr-CA');
        case 'strict':
          return l.score.strict;
        case 'assoupli':
          return l.score.relaxed;
        case 'qualitePrix':
          return l.score.quality.strict * 1000 + l.score.price.strict;
        case 'verdict':
          return l.score.verdict ? ORDRE_VERDICT[l.score.verdict] : 99;
        case 'horizon':
          return l.plusLongHorizon10sur10 ?? 0;
        default:
          return '';
      }
    };
    this.donnees.filterPredicate = (l) =>
      (!this.bourse() || l.stock.exchange === this.bourse()) && (!this.verdict() || l.score.verdict === this.verdict());

    effect(() => {
      const tri = this.tri();
      if (tri) {
        this.donnees.sort = tri;
      }
    });
    // Les filtres côté client déclenchent un nouveau passage du prédicat.
    effect(() => {
      this.bourse();
      this.verdict();
      this.donnees.filter = `${this.bourse()}|${this.verdict()}`;
      this.nbLignes.set(this.donnees.filteredData.length);
    });
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    chargerLignes(this.api, this.mode(), this.horizon())
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (lignes) => {
          this.donnees.data = lignes;
          this.donnees.filter = `${this.bourse()}|${this.verdict()}`;
          this.nbLignes.set(this.donnees.filteredData.length);
          this.etat.set('pret');
        },
        error: () => this.etat.set('erreur'),
      });
  }

  protected changerMode(m: Mode): void {
    this.mode.set(m);
    if (m === Mode.compat) {
      this.horizon.set(HORIZON_COMPAT);
    }
    this.charger();
  }

  protected changerHorizon(h: Horizon): void {
    this.horizon.set(h);
    this.charger();
  }

  protected libelleVerdict(v: Verdict | undefined): string {
    return v ? traduire(`verdict.${v}`) : traduire('verdict.indicatif');
  }

  protected icone(v: Verdict | undefined): string {
    return v ? ICONES[v] : '–';
  }

  protected ajouter(): void {
    this.dialog
      .open(AjoutDialog, { width: '28rem' })
      .afterClosed()
      .subscribe((stock) => {
        if (stock) {
          this.snack.open(traduire('liste.ajoutee', { symbole: stock.symbol }), traduire('liste.fermer'), { duration: 5000 });
          this.charger();
        }
      });
  }

  protected retirer(symbole: string): void {
    this.dialog
      .open(RetraitDialog, { data: { symbole } })
      .afterClosed()
      .subscribe((ok) => {
        if (!ok) {
          return;
        }
        this.api.deleteStock(symbole).subscribe({
          next: () => {
            this.snack.open(traduire('liste.retire', { symbole }), traduire('liste.fermer'), { duration: 5000 });
            this.charger();
          },
          error: () => this.snack.open(traduire('retirer.erreur'), traduire('liste.fermer'), { duration: 8000 }),
        });
      });
  }
}
