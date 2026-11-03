import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { Subscription, catchError, of, switchMap, takeWhile, tap, timer } from 'rxjs';
import { ImportsService, Job, JobEntry, JobEntryLevelEnum, JobStatus } from '../../../api';
import { CleTraduction, TraductionPipe, traduire } from '../../../i18n/t.pipe';

/** Taille maximale d'un fichier, alignée sur l'API (10 Mo). */
export const TAILLE_MAX_OCTETS = 10 * 1024 * 1024;
/** Intervalle d'interrogation du job. */
export const INTERVALLE_MS = 2000;
/** Durée maximale de suivi du job. */
export const DUREE_MAX_MS = 120_000;

const ICONES: Record<JobEntryLevelEnum, string> = {
  info: 'ℹ',
  warning: '⚠',
  error: '✖',
};
const LIBELLES: Record<JobEntryLevelEnum, CleTraduction> = {
  info: 'import.niveau.info',
  warning: 'import.niveau.warning',
  error: 'import.niveau.error',
};

type Phase = 'attente' | 'envoi' | 'suivi' | 'termine' | 'delai' | 'echec';

/** Écran d'import de la feuille xlsx : envoi, suivi du job et compte rendu. Aucun calcul : tout vient de l'API. */
@Component({
  selector: 'app-stockeval-import',
  imports: [
    FormsModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
    TraductionPipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './import-page.html',
  styleUrl: './import-page.scss',
})
export class ImportPage {
  private readonly api = inject(ImportsService);
  private readonly destroyRef = inject(DestroyRef);
  private suivi?: Subscription;

  protected readonly fichier = signal<File | null>(null);
  protected readonly erreurFichier = signal<CleTraduction | null>(null);
  /** Année fiscale de « Year 1 » ; par défaut l'année précédente. */
  protected readonly annee = signal<number | null>(new Date().getFullYear() - 1);
  protected readonly phase = signal<Phase>('attente');
  protected readonly job = signal<Job | null>(null);
  protected readonly messageErreur = signal<string | null>(null);

  protected readonly anneeValide = computed(() => {
    const a = this.annee();
    return a !== null && Number.isInteger(a) && a >= 1980 && a <= 2100;
  });
  protected readonly peutEnvoyer = computed(
    () =>
      this.fichier() !== null &&
      this.erreurFichier() === null &&
      this.anneeValide() &&
      this.phase() !== 'envoi' &&
      this.phase() !== 'suivi',
  );
  protected readonly entrees = computed(() => this.job()?.entries ?? []);
  protected readonly nombres = computed(() => {
    const e = this.entrees();
    const n = (niveau: JobEntryLevelEnum) => e.filter((x) => x.level === niveau).length;
    return { info: n(JobEntryLevelEnum.info), warning: n(JobEntryLevelEnum.warning), error: n(JobEntryLevelEnum.error) };
  });
  protected readonly statutJob = computed<CleTraduction | null>(() => {
    switch (this.job()?.status) {
      case JobStatus.queued:
        return 'import.statut.queued';
      case JobStatus.running:
        return 'import.statut.running';
      case JobStatus.succeeded:
        return 'import.statut.succeeded';
      case JobStatus.failed:
        return 'import.statut.failed';
      default:
        return null;
    }
  });
  protected readonly iconeStatut = computed(() => {
    switch (this.job()?.status) {
      case JobStatus.failed:
        return '✖';
      case JobStatus.succeeded:
        return '✔';
      default:
        return '…';
    }
  });

  protected choisirFichier(event: Event): void {
    const input = event.target as HTMLInputElement;
    const f = input.files?.item(0) ?? null;
    this.fichier.set(f);
    if (f === null) {
      this.erreurFichier.set('import.erreur.aucunFichier');
    } else if (f.size > TAILLE_MAX_OCTETS) {
      // Refus avant l'envoi : la requête n'est jamais émise.
      this.erreurFichier.set('import.erreur.tropGros');
    } else if (f.size === 0) {
      this.erreurFichier.set('import.erreur.vide');
    } else if (!f.name.toLowerCase().endsWith('.xlsx')) {
      this.erreurFichier.set('import.erreur.format');
    } else {
      this.erreurFichier.set(null);
    }
  }

  protected envoyer(): void {
    const f = this.fichier();
    const a = this.annee();
    if (!this.peutEnvoyer() || f === null || a === null) {
      return;
    }
    this.suivi?.unsubscribe();
    this.job.set(null);
    this.messageErreur.set(null);
    this.phase.set('envoi');
    this.api
      .importXlsx(f, a)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (job) => this.suivre(job),
        error: () => {
          this.messageErreur.set(traduire('import.erreur.envoi'));
          this.phase.set('echec');
        },
      });
  }

  private suivre(initial: Job): void {
    this.job.set(initial);
    if (this.estTermine(initial)) {
      this.phase.set('termine');
      return;
    }
    this.phase.set('suivi');
    const debut = Date.now();
    // Interrogation toutes les 2 s, arrêtée à la fin du job ou après 2 min.
    this.suivi = timer(INTERVALLE_MS, INTERVALLE_MS)
      .pipe(
        takeWhile(() => Date.now() - debut < DUREE_MAX_MS),
        switchMap(() => this.api.getImport(initial.id).pipe(catchError(() => of(null)))),
        tap((job) => {
          if (job) {
            this.job.set(job);
            if (this.estTermine(job)) {
              this.phase.set('termine');
            }
          }
        }),
        takeWhile((job) => job === null || !this.estTermine(job)),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe({
        complete: () => {
          if (this.phase() === 'suivi') {
            this.phase.set('delai');
          }
        },
      });
  }

  private estTermine(job: Job): boolean {
    return job.status === JobStatus.succeeded || job.status === JobStatus.failed;
  }

  protected icone(e: JobEntry): string {
    return ICONES[e.level];
  }

  protected libelle(e: JobEntry): CleTraduction {
    return LIBELLES[e.level];
  }
}
