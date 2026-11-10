import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { AssetClass, Etf, EtfRegion, EtfType, PortfolioService } from '../../api';
import { afficherDecimal, afficherPourcentage, messageProbleme } from '../../core/probleme';
import { CleTraduction, TraductionPipe, traduire } from '../../i18n/t.pipe';
import { pourcentageVersFraction, validerDecimal } from '../stockeval/edit/edit.data';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Nombre de FNB comparables côte à côte. */
export const MIN_COMPARAISON = 2;
export const MAX_COMPARAISON = 3;
const ANNEES = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

/** Catalogue de FNB filtrable (type, région, RFG max) et comparaison de 2 ou 3 FNB. Aucune valeur n'est calculée ici. */
@Component({
  selector: 'app-etf-catalog',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, MatSelectModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './etf-catalog.html',
  styleUrl: './etf-catalog.scss',
})
export class EtfCatalog {
  private readonly api = inject(PortfolioService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly types = Object.values(EtfType);
  protected readonly regions = Object.values(EtfRegion);
  protected readonly classes = Object.values(AssetClass);
  protected readonly annees = ANNEES;

  protected readonly type = signal<EtfType | ''>('');
  protected readonly region = signal<EtfRegion | ''>('');
  /** RFG maximal saisi en pourcentage (« 0,5 »). */
  protected readonly rfgMax = signal('');
  protected readonly erreurRfg = signal<CleTraduction | null>(null);

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly fnb = signal<Etf[]>([]);
  protected readonly choisis = signal<string[]>([]);
  protected readonly comparaison = signal(false);
  protected readonly selection = computed(() =>
    this.choisis()
      .map((s) => this.fnb().find((f) => f.symbol === s))
      .filter((f): f is Etf => !!f),
  );
  protected readonly peutComparer = computed(
    () => this.choisis().length >= MIN_COMPARAISON && this.choisis().length <= MAX_COMPARAISON,
  );

  constructor() {
    this.charger();
  }

  protected appliquer(): void {
    let maxMer: number | undefined;
    const saisie = this.rfgMax().trim();
    if (saisie !== '') {
      const r = validerDecimal(saisie, { min: 0, max: 100 });
      if ('erreur' in r) {
        this.erreurRfg.set('fnb.erreur.rfg');
        return;
      }
      // Saisie en pourcentage, envoi en fraction par déplacement de la virgule sur le texte (D214).
      maxMer = Number(pourcentageVersFraction(r.valeur));
    }
    this.erreurRfg.set(null);
    this.charger(maxMer);
  }

  protected reinitialiser(): void {
    this.type.set('');
    this.region.set('');
    this.rfgMax.set('');
    this.erreurRfg.set(null);
    this.charger();
  }

  private charger(maxMer?: number): void {
    this.etat.set('chargement');
    this.api
      .listEtfs(this.type() || undefined, this.region() || undefined, maxMer)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.fnb.set(l.items);
          // On ne garde que les FNB encore listés.
          this.choisis.update((c) => c.filter((s) => l.items.some((f) => f.symbol === s)));
          if (!this.peutComparer()) {
            this.comparaison.set(false);
          }
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'fnb.erreur'));
          this.etat.set('erreur');
        },
      });
  }

  protected estChoisi(symbole: string): boolean {
    return this.choisis().includes(symbole);
  }

  protected basculer(symbole: string, coche: boolean): void {
    this.choisis.update((c) => (coche ? [...c.filter((s) => s !== symbole), symbole] : c.filter((s) => s !== symbole)));
    if (!this.peutComparer()) {
      this.comparaison.set(false);
    }
  }

  protected desactive(symbole: string): boolean {
    return !this.estChoisi(symbole) && this.choisis().length >= MAX_COMPARAISON;
  }

  protected comparer(): void {
    if (this.peutComparer()) {
      this.comparaison.set(true);
    }
  }

  protected fermerComparaison(): void {
    this.comparaison.set(false);
  }

  protected libelleType(t: EtfType): string {
    return traduire(`fnb.type.${t}`);
  }

  protected libelleRegion(r: EtfRegion | undefined): string {
    return r ? traduire(`fnb.region.${r}`) : '';
  }

  protected libelleClasse(c: AssetClass): string {
    return traduire(`fnb.classe.${c}`);
  }

  protected rfg(f: Etf): string | null {
    return f.mer === undefined ? null : afficherPourcentage(f.mer);
  }

  protected part(f: Etf, c: AssetClass): string | null {
    const a = f.allocation.find((x) => x.assetClass === c);
    return a ? afficherPourcentage(a.weight) : null;
  }

  protected rendement(f: Etf, annees: number): string | null {
    const r = f.returns?.find((x) => x.years === annees);
    return r ? afficherPourcentage(r.annualized) : null;
  }

  protected taille(f: Etf): string {
    return f.size ? `${afficherDecimal(f.size)} ${f.currency}` : '';
  }

  protected esg(f: Etf): CleTraduction | null {
    return f.esg === undefined ? null : f.esg ? 'fnb.esg.oui' : 'fnb.esg.non';
  }
}
