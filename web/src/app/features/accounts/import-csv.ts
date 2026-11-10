import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, input, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { Account, CsvImportEntry, CsvImportReport, ImportsService } from '../../api';
import { messageProbleme } from '../../core/probleme';
import { CleTraduction, TraductionPipe, traduire } from '../../i18n/t.pipe';

/** Taille maximale d'un fichier, alignée sur l'API (10 Mo). */
export const TAILLE_MAX_OCTETS = 10 * 1024 * 1024;
/** Seul adapter de la phase 2 (D315). */
export const COURTIER = 'generic';

const ICONES: Record<string, string> = { info: 'ℹ', warning: '⚠', error: '✖' };
const LIBELLES: Record<string, CleTraduction> = {
  info: 'import.niveau.info',
  warning: 'import.niveau.warning',
  error: 'import.niveau.error',
};

/** Import CSV d'un compte : un compte par appel, rapport ligne par ligne. Tout vient de l'API. */
@Component({
  selector: 'app-import-csv',
  imports: [FormsModule, MatButtonModule, MatProgressBarModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './accounts.scss',
  template: `
    <h2>{{ 'csv.titre' | t }}</h2>
    <p>{{ 'csv.intro' | t: { compte: compte().name } }}</p>
    <form (ngSubmit)="envoyer()" [attr.aria-label]="'csv.titre' | t">
      <div class="champ">
        <label for="csv-fichier">{{ 'csv.fichier' | t }}</label>
        <input
          id="csv-fichier"
          type="file"
          accept=".csv,text/csv"
          data-testid="csv-fichier"
          [attr.aria-describedby]="erreurFichier() ? 'csv-erreur-fichier' : 'csv-aide'"
          [attr.aria-invalid]="erreurFichier() ? 'true' : null"
          (change)="choisir($event)"
        />
        <small id="csv-aide">{{ 'csv.aide' | t }}</small>
        @if (erreurFichier(); as cle) {
          <p id="csv-erreur-fichier" class="erreur" role="alert" data-testid="csv-erreur-fichier">
            <span class="icone" aria-hidden="true">✖</span>{{ cle | t }}
          </p>
        }
      </div>
      <button mat-flat-button type="submit" data-testid="csv-envoyer" [disabled]="!peutEnvoyer()">
        {{ 'csv.envoyer' | t }}
      </button>
    </form>

    <div aria-live="polite">
      @if (envoi()) {
        <p>{{ 'csv.envoiEnCours' | t }}</p>
        <mat-progress-bar mode="indeterminate" />
      }
      @if (erreurEnvoi()) {
        <p class="erreur" role="alert" data-testid="csv-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ erreurEnvoi() }}
        </p>
      }
    </div>

    @if (rapport(); as r) {
      <section data-testid="csv-rapport" aria-labelledby="csv-titre-rapport">
        <h3 id="csv-titre-rapport">{{ 'csv.rapport' | t }}</h3>
        <ul class="compteurs">
          <li data-testid="csv-lues">
            {{ 'csv.lues' | t: { n: '' + r.rowsRead } }}
          </li>
          <li data-testid="csv-creees">
            {{ 'csv.creees' | t: { n: '' + r.created } }}
          </li>
          <li data-testid="csv-doublons">
            {{ 'csv.doublons' | t: { n: '' + r.duplicates } }}
          </li>
          <li data-testid="csv-refusees">
            {{ 'csv.refusees' | t: { n: '' + r.rejected } }}
          </li>
          <li data-testid="csv-invalides">
            {{ 'csv.invalides' | t: { n: '' + r.invalid } }}
          </li>
          <li data-testid="csv-avertissements">
            {{ 'csv.avertissements' | t: { n: '' + r.warnings } }}
          </li>
        </ul>
        @if (r.entries.length === 0) {
          <p>{{ 'csv.aucuneEntree' | t }}</p>
        } @else {
          <div class="defile">
            <table class="simple" data-testid="csv-lignes">
              <caption class="sr-only">
                {{
                  'csv.rapport' | t
                }}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{{ 'import.col.ligne' | t }}</th>
                  <th scope="col">{{ 'import.col.niveau' | t }}</th>
                  <th scope="col">{{ 'csv.col.colonne' | t }}</th>
                  <th scope="col">{{ 'import.col.message' | t }}</th>
                </tr>
              </thead>
              <tbody>
                @for (e of r.entries; track $index) {
                  <tr [attr.data-niveau]="e.level">
                    <td>{{ e.line }}</td>
                    <td>
                      <span class="icone" aria-hidden="true">{{ icone(e) }}</span
                      >{{ libelle(e) }}
                    </td>
                    <td>{{ e.column ?? '' }}</td>
                    <td>{{ e.message }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
      </section>
    }
  `,
})
export class ImportCsv {
  private readonly api = inject(ImportsService);
  private readonly destroyRef = inject(DestroyRef);

  readonly compte = input.required<Account>();

  protected readonly fichier = signal<File | null>(null);
  protected readonly erreurFichier = signal<CleTraduction | null>(null);
  protected readonly envoi = signal(false);
  protected readonly erreurEnvoi = signal('');
  protected readonly rapport = signal<CsvImportReport | null>(null);
  protected readonly peutEnvoyer = computed(
    () => this.fichier() !== null && this.erreurFichier() === null && !this.envoi(),
  );

  protected choisir(event: Event): void {
    const f = (event.target as HTMLInputElement).files?.item(0) ?? null;
    this.fichier.set(f);
    if (f === null) {
      this.erreurFichier.set('csv.erreur.aucunFichier');
    } else if (f.size > TAILLE_MAX_OCTETS) {
      // Refus avant l'envoi : la requête n'est jamais émise.
      this.erreurFichier.set('import.erreur.tropGros');
    } else if (f.size === 0) {
      this.erreurFichier.set('import.erreur.vide');
    } else if (!f.name.toLowerCase().endsWith('.csv')) {
      this.erreurFichier.set('csv.erreur.format');
    } else {
      this.erreurFichier.set(null);
    }
  }

  protected envoyer(): void {
    const f = this.fichier();
    if (!this.peutEnvoyer() || f === null) {
      return;
    }
    this.rapport.set(null);
    this.erreurEnvoi.set('');
    this.envoi.set(true);
    this.api
      .importCsv(COURTIER, this.compte().id, f)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (r) => {
          this.rapport.set(r);
          this.envoi.set(false);
        },
        error: (e: unknown) => {
          this.envoi.set(false);
          this.erreurEnvoi.set(
            e instanceof HttpErrorResponse && e.status === 413
              ? traduire('csv.erreur.refuse413')
              : messageProbleme(e, 'csv.erreur.envoi'),
          );
        },
      });
  }

  protected icone(e: CsvImportEntry): string {
    return ICONES[e.level] ?? '•';
  }

  protected libelle(e: CsvImportEntry): string {
    const cle = LIBELLES[e.level];
    return cle ? traduire(cle) : e.level;
  }
}
