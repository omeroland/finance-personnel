import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { forkJoin } from 'rxjs';
import {
  Account,
  AccountsService,
  ContributionByAccount,
  Currency,
  Model,
  PortfolioService,
  RebalancePlan,
  RebalancePlanRequest,
} from '../../api';
import { afficherDecimal, afficherPourcentage, messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { OrdresTable } from './affichages';
import { estSansPrix, libelleClasse, symbolesCites, validerMontant } from './portfolio.data';

type Etat = 'chargement' | 'pret' | 'erreur';

/**
 * Plan de rééquilibrage : un ou plusieurs comptes, cotisation par compte (D443), prix saisis (D440).
 * Les ordres (ventes d'abord, puis achats, D442), les classes hors bande et l'encaisse restante viennent de l'API.
 */
@Component({
  selector: 'app-plan-reequilibrage',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, OrdresTable, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.reeq.titre' | t }}</h2>
    @switch (etat()) {
      @case ('chargement') {
        <p role="status" data-testid="reeq-chargement">
          {{ 'pf.chargement' | t }}
        </p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="reeq-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @default {
        @if (modeles().length === 0) {
          <p role="status" data-testid="reeq-vide">
            {{ 'pf.achat.aucunModele' | t }}
          </p>
        } @else if (comptes().length === 0) {
          <p role="status" data-testid="reeq-aucun-compte">
            {{ 'pf.reeq.aucunCompte' | t }}
          </p>
        } @else {
          <form (ngSubmit)="calculer()" novalidate>
            <div class="ligne-saisie">
              <mat-form-field class="large">
                <mat-label>{{ 'pf.col.modele' | t }}</mat-label>
                <select
                  matNativeControl
                  name="modele"
                  data-testid="reeq-modele"
                  [ngModel]="modeleId()"
                  (ngModelChange)="choisirModele($event)"
                >
                  @for (m of modeles(); track m.id) {
                    <option [value]="m.id">{{ m.name }} ({{ 'pf.col.version' | t }} {{ m.version }})</option>
                  }
                </select>
              </mat-form-field>
              <mat-form-field>
                <mat-label>{{ 'pf.col.devise' | t }}</mat-label>
                <select
                  matNativeControl
                  name="devise"
                  data-testid="reeq-devise"
                  [ngModel]="devise()"
                  (ngModelChange)="choisirDevise($event)"
                >
                  @for (d of devises; track d) {
                    <option [value]="d">{{ d }}</option>
                  }
                </select>
              </mat-form-field>
            </div>

            <fieldset>
              <legend>{{ 'pf.reeq.comptes' | t }}</legend>
              <p>
                <small>{{ 'pf.reeq.aideComptes' | t }}</small>
              </p>
              @if (comptesDevise().length === 0) {
                <p role="status" data-testid="reeq-aucun-compte-devise">
                  {{ 'pf.reeq.aucunCompteDevise' | t }}
                </p>
              }
              @for (c of comptesDevise(); track c.id) {
                <div class="ligne-saisie" [attr.data-testid]="'reeq-compte-' + c.id">
                  <label>
                    <input
                      type="checkbox"
                      [attr.data-testid]="'reeq-inclure-' + c.id"
                      [checked]="choisis().includes(c.id)"
                      (change)="basculer(c.id, $any($event.target).checked)"
                    />
                    {{ c.name }}
                  </label>
                  <mat-form-field>
                    <mat-label>{{ 'pf.reeq.cotisation' | t }} ({{ c.currency }})</mat-label>
                    <input
                      matInput
                      inputmode="decimal"
                      autocomplete="off"
                      [name]="'cotisation-' + c.id"
                      [attr.data-testid]="'reeq-cotisation-' + c.id"
                      [disabled]="choisis().length > 0 && !choisis().includes(c.id)"
                      [ngModel]="cotisations()[c.id] ?? ''"
                      (ngModelChange)="majCotisation(c.id, $event)"
                    />
                  </mat-form-field>
                </div>
              }
            </fieldset>

            <fieldset>
              <legend>{{ 'pf.prix.titre' | t }}</legend>
              <p>
                <small>{{ 'pf.prix.aide' | t }}</small>
              </p>
              <div class="ligne-saisie">
                @for (h of fonds(); track h) {
                  <mat-form-field>
                    <mat-label>{{ h }}</mat-label>
                    <input
                      matInput
                      inputmode="decimal"
                      autocomplete="off"
                      [name]="'prix-' + h"
                      [attr.data-testid]="'reeq-prix-' + h"
                      [ngModel]="prix()[h] ?? ''"
                      (ngModelChange)="majPrix(h, $event)"
                    />
                    @if (manquants().includes(h)) {
                      <mat-hint class="requis"><span aria-hidden="true">✖</span> {{ 'pf.prix.requis' | t }}</mat-hint>
                    }
                  </mat-form-field>
                }
              </div>
            </fieldset>

            @if (erreurSaisie()) {
              <p class="erreur" role="alert" data-testid="reeq-erreur-saisie">
                <span class="icone" aria-hidden="true">✖</span>{{ erreurSaisie() }}
              </p>
            }
            <button mat-flat-button type="submit" data-testid="reeq-calculer" [disabled]="envoi()">
              {{ 'pf.reeq.calculer' | t }}
            </button>
          </form>

          @if (sansPrix(); as sp) {
            <section class="alerte" role="alert" data-testid="reeq-sans-prix">
              <h3><span class="icone" aria-hidden="true">✖</span>{{ 'pf.sansPrix.titre' | t }}</h3>
              <p>{{ sp.detail }}</p>
              @if (manquants().length > 0) {
                <p>{{ 'pf.sansPrix.liste' | t }}</p>
                <ul class="liste-simple" data-testid="reeq-fonds-sans-prix">
                  @for (s of manquants(); track s) {
                    <li>{{ s }}</li>
                  }
                </ul>
              }
              <p>{{ 'pf.sansPrix.action' | t }}</p>
            </section>
          }
          @if (erreurApi()) {
            <p class="erreur" role="alert" data-testid="reeq-erreur-api">
              <span class="icone" aria-hidden="true">✖</span>{{ erreurApi() }}
            </p>
          }
          @if (plan(); as p) {
            <section class="carte" aria-labelledby="titre-plan-reeq" data-testid="plan-reeq">
              <h3 id="titre-plan-reeq">
                {{ 'pf.reeq.resultat' | t: { version: '' + p.modelVersion } }}
              </h3>
              <dl>
                <dt>{{ 'pf.reeq.valeurAvant' | t }}</dt>
                <dd data-testid="reeq-valeur">
                  @if (p.totalValue) {
                    {{ nombre(p.totalValue) }} {{ p.currency }}
                  } @else {
                    <span class="absent">{{ 'pf.nonDisponible' | t }}</span>
                  }
                </dd>
                <dt>{{ 'pf.reeq.encaisseApres' | t }}</dt>
                <dd data-testid="reeq-encaisse">{{ nombre(p.cashAfter) }} {{ p.currency }}</dd>
              </dl>
              @if (p.accountCash.length > 0) {
                <h4>{{ 'pf.reeq.encaisseComptes' | t }}</h4>
                <div class="defile">
                  <table class="simple" data-testid="reeq-encaisse-comptes">
                    <caption class="sr-only">
                      {{
                        'pf.reeq.encaisseComptes' | t
                      }}
                    </caption>
                    <thead>
                      <tr>
                        <th scope="col">{{ 'pf.col.compte' | t }}</th>
                        <th scope="col" class="num">{{ 'pf.col.encaisseFinale' | t }}</th>
                      </tr>
                    </thead>
                    <tbody>
                      @for (e of p.accountCash; track e.accountId) {
                        <tr [attr.data-testid]="'reeq-encaisse-' + e.accountId">
                          <th scope="row">{{ nomCompte(e.accountId) }}</th>
                          <td class="num">{{ nombre(e.cash) }} {{ p.currency }}</td>
                        </tr>
                      }
                    </tbody>
                  </table>
                </div>
              }
              <h4>{{ 'pf.reeq.classes' | t }}</h4>
              <div class="defile">
                <table class="simple" data-testid="reeq-classes">
                  <caption class="sr-only">
                    {{
                      'pf.reeq.classes' | t
                    }}
                  </caption>
                  <thead>
                    <tr>
                      <th scope="col">{{ 'pf.col.classe' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.avant' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.cible' | t }}</th>
                      <th scope="col">{{ 'pf.col.bandeAvant' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.apres' | t }}</th>
                      <th scope="col">{{ 'pf.col.bandeApres' | t }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    @for (c of p.classes; track c.assetClass) {
                      <tr [attr.data-testid]="'reeq-classe-' + c.assetClass">
                        <th scope="row">{{ classe(c.assetClass) }}</th>
                        <td class="num">{{ pct(c.actual) }}</td>
                        <td class="num">{{ pct(c.target) }}</td>
                        <td>
                          @if (c.outOfBand) {
                            <span class="statut requis"
                              ><span aria-hidden="true">⚠</span>{{ 'pf.bande.hors' | t }}</span
                            >
                          } @else {
                            <span class="statut"><span aria-hidden="true">✔</span>{{ 'pf.bande.dans' | t }}</span>
                          }
                        </td>
                        <td class="num">{{ pct(c.after) }}</td>
                        <td>
                          @if (c.outOfBandAfter) {
                            <span class="statut requis"
                              ><span aria-hidden="true">⚠</span>{{ 'pf.bande.hors' | t }}</span
                            >
                          } @else {
                            <span class="statut"><span aria-hidden="true">✔</span>{{ 'pf.bande.dans' | t }}</span>
                          }
                        </td>
                      </tr>
                    }
                  </tbody>
                </table>
              </div>
              <h4>{{ 'pf.reeq.fonds' | t }}</h4>
              <div class="defile">
                <table class="simple" data-testid="reeq-fonds">
                  <caption class="sr-only">
                    {{
                      'pf.reeq.fonds' | t
                    }}
                  </caption>
                  <thead>
                    <tr>
                      <th scope="col">{{ 'pf.col.symbole' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.avant' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.cible' | t }}</th>
                      <th scope="col" class="num">{{ 'pf.col.apres' | t }}</th>
                      <th scope="col">{{ 'pf.col.bandeAvant' | t }}</th>
                      <th scope="col">{{ 'pf.col.bandeApres' | t }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    @for (f of p.funds; track f.symbol) {
                      <tr [attr.data-testid]="'reeq-fonds-' + f.symbol">
                        <th scope="row">{{ f.symbol }}</th>
                        <td class="num">{{ pct(f.before) }}</td>
                        <td class="num">{{ pct(f.target) }}</td>
                        <td class="num">{{ pct(f.after) }}</td>
                        <td>
                          @if (f.outOfBandBefore) {
                            <span class="statut requis"
                              ><span aria-hidden="true">⚠</span>{{ 'pf.bande.hors' | t }}</span
                            >
                          } @else {
                            <span class="statut"><span aria-hidden="true">✔</span>{{ 'pf.bande.dans' | t }}</span>
                          }
                        </td>
                        <td>
                          @if (f.outOfBandAfter) {
                            <span class="statut requis"
                              ><span aria-hidden="true">⚠</span>{{ 'pf.bande.hors' | t }}</span
                            >
                          } @else {
                            <span class="statut"><span aria-hidden="true">✔</span>{{ 'pf.bande.dans' | t }}</span>
                          }
                        </td>
                      </tr>
                    }
                  </tbody>
                </table>
              </div>
              <h4>{{ 'pf.ordres.titre' | t }}</h4>
              <p>
                <small>{{ 'pf.ordres.aideOrdre' | t }}</small>
              </p>
              <app-ordres-table [ordres]="p.orders" [ecartes]="p.skipped" [comptes]="comptes()" />
            </section>
          }
        }
      }
    }
  `,
})
export class PlanReequilibrage {
  private readonly api = inject(PortfolioService);
  private readonly comptesApi = inject(AccountsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly devises = Object.values(Currency);
  protected readonly nombre = afficherDecimal;
  protected readonly pct = afficherPourcentage;
  protected readonly classe = libelleClasse;

  protected nomCompte(id: string): string {
    return this.comptes().find((c) => c.id === id)?.name ?? id;
  }

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly modeles = signal<Model[]>([]);
  protected readonly comptes = signal<Account[]>([]);
  protected readonly modeleId = signal('');
  protected readonly devise = signal<Currency>(Currency.CAD);
  protected readonly choisis = signal<string[]>([]);
  protected readonly cotisations = signal<Record<string, string>>({});
  protected readonly prix = signal<Record<string, string>>({});
  protected readonly comptesDevise = computed(() => this.comptes().filter((c) => c.currency === this.devise()));
  protected readonly fonds = computed(
    () =>
      this.modeles()
        .find((m) => m.id === this.modeleId())
        ?.holdings.map((h) => h.symbol) ?? [],
  );

  protected readonly envoi = signal(false);
  protected readonly erreurSaisie = signal('');
  protected readonly erreurApi = signal('');
  protected readonly sansPrix = signal<{ detail: string } | null>(null);
  protected readonly manquants = signal<string[]>([]);
  protected readonly plan = signal<RebalancePlan | null>(null);

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    forkJoin({
      modeles: this.api.listModels(),
      comptes: this.comptesApi.listAccounts(),
    })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: ({ modeles, comptes }) => {
          this.modeles.set(modeles.items);
          this.comptes.set(comptes.items);
          this.modeleId.set(modeles.items[0]?.id ?? '');
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'pf.reeq.erreurChargement'));
          this.etat.set('erreur');
        },
      });
  }

  private reinitialiserResultat(): void {
    this.plan.set(null);
    this.sansPrix.set(null);
    this.manquants.set([]);
    this.erreurApi.set('');
    this.erreurSaisie.set('');
  }

  protected choisirModele(id: string): void {
    this.modeleId.set(id);
    this.prix.set({});
    this.reinitialiserResultat();
  }

  protected choisirDevise(d: Currency): void {
    this.devise.set(d);
    this.choisis.set([]);
    this.cotisations.set({});
    this.reinitialiserResultat();
  }

  protected basculer(id: string, coche: boolean): void {
    this.choisis.update((l) => (coche ? [...l.filter((x) => x !== id), id] : l.filter((x) => x !== id)));
  }

  protected majCotisation(id: string, valeur: string): void {
    this.cotisations.update((c) => ({ ...c, [id]: valeur }));
  }

  protected majPrix(symbole: string, valeur: string): void {
    this.prix.update((p) => ({ ...p, [symbole]: valeur }));
  }

  protected calculer(): void {
    this.reinitialiserResultat();
    const comptesPlan =
      this.choisis().length > 0
        ? this.comptesDevise().filter((c) => this.choisis().includes(c.id))
        : this.comptesDevise();
    const contributions: ContributionByAccount[] = [];
    for (const c of comptesPlan) {
      const saisie = (this.cotisations()[c.id] ?? '').trim();
      if (saisie === '') {
        continue;
      }
      const r = validerMontant(saisie, true);
      if ('erreur' in r) {
        this.erreurSaisie.set(`${c.name} : ${traduire(r.erreur)}`);
        return;
      }
      contributions.push({ accountId: c.id, amount: r.valeur });
    }
    const prix: Record<string, string> = {};
    for (const s of this.fonds()) {
      const saisie = (this.prix()[s] ?? '').trim();
      if (saisie === '') {
        continue;
      }
      const r = validerMontant(saisie, false);
      if ('erreur' in r) {
        this.erreurSaisie.set(`${s} : ${traduire(r.erreur)}`);
        return;
      }
      prix[s] = r.valeur;
    }
    const demande: RebalancePlanRequest = {
      modelId: this.modeleId(),
      currency: this.devise(),
    };
    if (this.choisis().length > 0) {
      demande.accountIds = this.choisis();
    }
    if (contributions.length > 0) {
      demande.contributions = contributions;
    }
    if (Object.keys(prix).length > 0) {
      demande.prices = prix;
    }
    this.envoi.set(true);
    this.api
      .createRebalancePlan(demande)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (p) => {
          this.envoi.set(false);
          this.plan.set(p);
        },
        error: (e: unknown) => {
          this.envoi.set(false);
          if (estSansPrix(e)) {
            this.sansPrix.set({
              detail: messageProbleme(e, 'pf.sansPrix.repli'),
            });
            this.manquants.set(symbolesCites(e));
          } else {
            this.erreurApi.set(messageProbleme(e, 'pf.reeq.erreur'));
          }
        },
      });
  }
}
