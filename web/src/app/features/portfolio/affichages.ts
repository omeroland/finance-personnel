import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import {
  Account,
  Exposure,
  IrrBasis,
  PerformanceMethod,
  PerformancePoint,
  PerformanceSeries,
  PlanOrder,
  SkippedOrder,
} from '../../api';
import { afficherDecimal, afficherPourcentage } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { afficherEcart, iconeSens, libelleClasse, libelleMotif, libelleSens } from './portfolio.data';

/** Exposition par transparence et écart au modèle. Tous les chiffres viennent de l'API. */
@Component({
  selector: 'app-exposition-table',
  imports: [TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    @if (exposition(); as e) {
      @if (e.classes.length === 0) {
        <p role="status" data-testid="expo-vide">{{ 'pf.expo.vide' | t }}</p>
      } @else {
        <dl data-testid="expo-resume">
          <dt>{{ 'pf.expo.jour' | t }}</dt>
          <dd>{{ e.asOf }}</dd>
          <dt>{{ 'pf.expo.valeur' | t }}</dt>
          <dd data-testid="expo-valeur">
            @if (e.totalValue) {
              {{ montant(e.totalValue) }} {{ e.currency }}
            } @else {
              <span class="absent">{{ 'pf.nonDisponible' | t }}</span>
            }
          </dd>
          <dt>{{ 'pf.expo.rfg' | t }}</dt>
          <dd data-testid="expo-rfg">
            @if (e.weightedMer !== undefined) {
              {{ pct(e.weightedMer) }}
            } @else {
              <span class="absent">{{ 'pf.nonDisponible' | t }}</span>
            }
          </dd>
        </dl>
        <div class="defile">
          <table class="simple" data-testid="expo-table">
            <caption class="sr-only">
              {{
                'pf.expo.titre' | t
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ 'pf.col.classe' | t }}</th>
                <th scope="col" class="num">{{ 'pf.col.reel' | t }}</th>
                <th scope="col" class="num">{{ 'pf.col.cible' | t }}</th>
                <th scope="col" class="num">{{ 'pf.col.ecart' | t }}</th>
              </tr>
            </thead>
            <tbody>
              @for (c of e.classes; track c.assetClass) {
                <tr [attr.data-testid]="'expo-' + c.assetClass">
                  <th scope="row">{{ classe(c.assetClass) }}</th>
                  <td class="num">{{ pct(c.actual) }}</td>
                  <td class="num">
                    @if (c.target !== undefined) {
                      {{ pct(c.target) }}
                    } @else {
                      <span class="absent">{{ 'pf.sansModele' | t }}</span>
                    }
                  </td>
                  <td class="num">
                    @if (c.deviation !== undefined) {
                      {{ ecart(c.deviation) }}
                    } @else {
                      <span class="absent">{{ 'pf.sansModele' | t }}</span>
                    }
                  </td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }
    }
  `,
})
export class ExpositionTable {
  readonly exposition = input.required<Exposure | null>();
  protected readonly pct = afficherPourcentage;
  protected readonly ecart = afficherEcart;
  protected readonly classe = libelleClasse;
  protected readonly montant = afficherDecimal;
}

/** Ordres d'un plan, en parts entières, dans l'ordre reçu (ventes d'abord, puis achats) ; ordres écartés listés. */
@Component({
  selector: 'app-ordres-table',
  imports: [TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    @if (ordres().length === 0) {
      <p role="status" data-testid="ordres-vide">{{ 'pf.ordres.vide' | t }}</p>
    } @else {
      <div class="defile">
        <table class="simple" data-testid="ordres">
          <caption class="sr-only">
            {{
              'pf.ordres.titre' | t
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ 'pf.col.sens' | t }}</th>
              <th scope="col">{{ 'pf.col.symbole' | t }}</th>
              @if (avecCompte()) {
                <th scope="col">{{ 'pf.col.compte' | t }}</th>
              }
              <th scope="col" class="num">{{ 'pf.col.parts' | t }}</th>
              <th scope="col" class="num">{{ 'pf.col.prix' | t }}</th>
              <th scope="col" class="num">{{ 'pf.col.montant' | t }}</th>
            </tr>
          </thead>
          <tbody>
            @for (o of ordres(); track $index) {
              <tr [attr.data-testid]="'ordre-' + $index">
                <td>
                  <span class="statut"
                    ><span aria-hidden="true">{{ icone(o.side) }}</span
                    >{{ sens(o.side) }}</span
                  >
                </td>
                <td>{{ o.symbol }}</td>
                @if (avecCompte()) {
                  <td>{{ nomCompte(o.accountId) }}</td>
                }
                <td class="num">{{ o.shares }}</td>
                <td class="num">{{ montant(o.price) }}</td>
                <td class="num">{{ montant(o.amount) }}</td>
              </tr>
            }
          </tbody>
        </table>
      </div>
    }
    @if (ecartes().length > 0) {
      <h3>{{ 'pf.ecartes.titre' | t }}</h3>
      <ul class="liste-simple" data-testid="ecartes">
        @for (s of ecartes(); track $index) {
          <li>{{ sens(s.side) }} {{ s.symbol }} : {{ montant(s.amount) }} ({{ motif(s.reason) }})</li>
        }
      </ul>
    }
  `,
})
export class OrdresTable {
  readonly ordres = input.required<PlanOrder[]>();
  readonly ecartes = input<SkippedOrder[]>([]);
  readonly comptes = input<Account[]>([]);
  protected readonly avecCompte = computed(() => this.ordres().some((o) => o.accountId));
  protected readonly sens = libelleSens;
  protected readonly icone = iconeSens;
  protected readonly motif = libelleMotif;
  protected readonly montant = afficherDecimal;

  protected nomCompte(id: string | undefined): string {
    return this.comptes().find((c) => c.id === id)?.name ?? id ?? '';
  }
}

/** Rendement d'un compte ou du consolidé : pondéré dans le temps et TRI, mensuel ou annuel d'une seule série. */
@Component({
  selector: 'app-serie-rendement',
  imports: [TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <section class="carte" [attr.data-testid]="testid()" [attr.aria-label]="titre()">
      <h3>{{ titre() }}</h3>
      @if (serie(); as s) {
        <dl>
          <dt>{{ 'pf.perf.methode' | t }}</dt>
          <dd data-testid="perf-methode">
            @if (s.method) {
              {{ methode(s.method) }}
            } @else {
              <span class="absent">{{ 'pf.nonDisponible' | t }}</span>
            }
          </dd>
          <dt>{{ 'pf.perf.twr' | t }}</dt>
          <dd data-testid="perf-twr">{{ valeur(s.twr) }}</dd>
          <dt>{{ 'pf.perf.irr' | t }}</dt>
          <dd data-testid="perf-irr">{{ valeur(s.irr) }}</dd>
        </dl>
        @if (s.irr !== undefined && s.irrBasis) {
          <p class="base" data-testid="perf-base">{{ base(s.irrBasis) }}</p>
        }
        @if (points().length === 0) {
          <p role="status" data-testid="perf-vide">{{ 'pf.perf.vide' | t }}</p>
        } @else {
          <div class="defile">
            <table class="simple" data-testid="perf-table">
              <caption class="sr-only">
                {{
                  titre()
                }}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{{ 'pf.col.periode' | t }}</th>
                  <th scope="col" class="num">{{ 'pf.perf.twr' | t }}</th>
                  <th scope="col" class="num">{{ 'pf.perf.irr' | t }}</th>
                </tr>
              </thead>
              <tbody>
                @for (p of points(); track p.period) {
                  <tr>
                    <th scope="row">{{ p.period }}</th>
                    <td class="num">{{ valeur(p.twr) }}</td>
                    <td class="num">{{ valeur(p.irr) }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
      }
    </section>
  `,
})
export class SerieRendement {
  readonly serie = input.required<PerformanceSeries>();
  readonly titre = input.required<string>();
  readonly testid = input('serie');
  readonly granularite = input<'monthly' | 'annual'>('monthly');
  protected readonly points = computed<PerformancePoint[]>(() => {
    const s = this.serie();
    return this.granularite() === 'annual' ? s.annual : s.monthly;
  });

  protected valeur(f: number | undefined): string {
    return f === undefined ? '–' : afficherPourcentage(f);
  }

  protected methode(m: PerformanceMethod): string {
    return traduire(`pf.perf.methode.${m}`);
  }

  protected base(b: IrrBasis): string {
    return traduire(`pf.perf.base.${b}`);
  }
}
