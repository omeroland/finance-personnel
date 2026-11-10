import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Currency, Model, PortfolioService, PurchasePlan } from '../../api';
import { afficherDecimal, messageProbleme } from '../../core/probleme';
import { TraductionPipe, traduire } from '../../i18n/t.pipe';
import { OrdresTable } from './affichages';
import { estSansPrix, symbolesCites, validerMontant } from './portfolio.data';

type Etat = 'chargement' | 'pret' | 'erreur';

/** Plan d'achat : montant, prix saisis facultatifs par fonds (D440), ordres en parts entières venus de l'API. */
@Component({
  selector: 'app-plan-achat',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, OrdresTable, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.achat.titre' | t }}</h2>
    @switch (etat()) {
      @case ('chargement') {
        <p role="status" data-testid="achat-chargement">
          {{ 'pf.chargement' | t }}
        </p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="achat-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @default {
        @if (modeles().length === 0) {
          <p role="status" data-testid="achat-vide">
            {{ 'pf.achat.aucunModele' | t }}
          </p>
        } @else {
          <form (ngSubmit)="calculer()" novalidate>
            <div class="ligne-saisie">
              <mat-form-field class="large">
                <mat-label>{{ 'pf.col.modele' | t }}</mat-label>
                <select
                  matNativeControl
                  name="modele"
                  data-testid="achat-modele"
                  [ngModel]="modeleId()"
                  (ngModelChange)="choisir($event)"
                >
                  @for (m of modeles(); track m.id) {
                    <option [value]="m.id">{{ m.name }} ({{ 'pf.col.version' | t }} {{ m.version }})</option>
                  }
                </select>
              </mat-form-field>
              <mat-form-field>
                <mat-label>{{ 'pf.achat.montant' | t }}</mat-label>
                <input
                  matInput
                  inputmode="decimal"
                  autocomplete="off"
                  name="montant"
                  data-testid="achat-montant"
                  [ngModel]="montant()"
                  (ngModelChange)="montant.set($event)"
                />
              </mat-form-field>
              <mat-form-field>
                <mat-label>{{ 'pf.col.devise' | t }}</mat-label>
                <select
                  matNativeControl
                  name="devise"
                  data-testid="achat-devise"
                  [ngModel]="devise()"
                  (ngModelChange)="devise.set($event)"
                >
                  @for (d of devises; track d) {
                    <option [value]="d">{{ d }}</option>
                  }
                </select>
              </mat-form-field>
            </div>

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
                      [attr.data-testid]="'prix-' + h"
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
              <p class="erreur" role="alert" data-testid="achat-erreur-saisie">
                <span class="icone" aria-hidden="true">✖</span>{{ erreurSaisie() }}
              </p>
            }
            <button mat-flat-button type="submit" data-testid="achat-calculer" [disabled]="envoi()">
              {{ 'pf.achat.calculer' | t }}
            </button>
          </form>

          @if (sansPrix(); as sp) {
            <section class="alerte" role="alert" data-testid="achat-sans-prix">
              <h3><span class="icone" aria-hidden="true">✖</span>{{ 'pf.sansPrix.titre' | t }}</h3>
              <p>{{ sp.detail }}</p>
              @if (manquants().length > 0) {
                <p>{{ 'pf.sansPrix.liste' | t }}</p>
                <ul class="liste-simple" data-testid="achat-fonds-sans-prix">
                  @for (s of manquants(); track s) {
                    <li>{{ s }}</li>
                  }
                </ul>
              }
              <p>{{ 'pf.sansPrix.action' | t }}</p>
            </section>
          }
          @if (erreurApi()) {
            <p class="erreur" role="alert" data-testid="achat-erreur-api">
              <span class="icone" aria-hidden="true">✖</span>{{ erreurApi() }}
            </p>
          }
          @if (plan(); as p) {
            <section class="carte" aria-labelledby="titre-plan-achat" data-testid="plan-achat">
              <h3 id="titre-plan-achat">
                {{ 'pf.achat.resultat' | t: { version: '' + p.modelVersion } }}
              </h3>
              <dl>
                <dt>{{ 'pf.achat.demande' | t }}</dt>
                <dd data-testid="achat-demande">{{ nombre(p.amount) }} {{ p.currency }}</dd>
                <dt>{{ 'pf.achat.investi' | t }}</dt>
                <dd data-testid="achat-investi">{{ nombre(p.invested) }} {{ p.currency }}</dd>
                <dt>{{ 'pf.achat.encaisse' | t }}</dt>
                <dd data-testid="achat-encaisse">{{ nombre(p.cashRemainder) }} {{ p.currency }}</dd>
              </dl>
              <app-ordres-table [ordres]="p.orders" />
            </section>
          }
        }
      }
    }
  `,
})
export class PlanAchat {
  private readonly api = inject(PortfolioService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly devises = Object.values(Currency);
  protected readonly nombre = afficherDecimal;

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly modeles = signal<Model[]>([]);
  protected readonly modeleId = signal('');
  protected readonly montant = signal('');
  protected readonly devise = signal<Currency>(Currency.CAD);
  protected readonly prix = signal<Record<string, string>>({});
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
  protected readonly plan = signal<PurchasePlan | null>(null);

  constructor() {
    this.charger();
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listModels()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.modeles.set(l.items);
          this.modeleId.set(l.items[0]?.id ?? '');
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'pf.modeles.erreur'));
          this.etat.set('erreur');
        },
      });
  }

  protected choisir(id: string): void {
    this.modeleId.set(id);
    this.prix.set({});
    this.reinitialiserResultat();
  }

  protected majPrix(symbole: string, valeur: string): void {
    this.prix.update((p) => ({ ...p, [symbole]: valeur }));
  }

  private reinitialiserResultat(): void {
    this.plan.set(null);
    this.sansPrix.set(null);
    this.manquants.set([]);
    this.erreurApi.set('');
    this.erreurSaisie.set('');
  }

  protected calculer(): void {
    this.reinitialiserResultat();
    const montant = validerMontant(this.montant(), false);
    if ('erreur' in montant) {
      this.erreurSaisie.set(`${traduire('pf.achat.montant')} : ${traduire(montant.erreur)}`);
      return;
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
    this.envoi.set(true);
    this.api
      .createPurchasePlan(this.modeleId(), {
        amount: montant.valeur,
        currency: this.devise(),
        ...(Object.keys(prix).length > 0 ? { prices: prix } : {}),
      })
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
            this.erreurApi.set(messageProbleme(e, 'pf.achat.erreur'));
          }
        },
      });
  }
}
