import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { AssetClass, Bands, Model, ModelHolding, ModelWrite, PortfolioService } from '../../api';
import { afficherPourcentage, messageProbleme } from '../../core/probleme';
import { CleTraduction, TraductionPipe, traduire } from '../../i18n/t.pipe';
import { CLASSES, MAX_FONDS, fractionVersSaisie, libelleClasse, saisieVersFraction } from './portfolio.data';

type Etat = 'chargement' | 'pret' | 'erreur';

interface LigneFonds {
  symbole: string;
  poids: string;
}

/**
 * Portefeuilles modèles versionnés : liste (dernière version), création (version 1), nouvelle version (PUT).
 * Saisie en pourcentage, envoi en fraction par déplacement de la virgule sur le texte (D214). Les totaux (100 %)
 * sont validés par le serveur (D313, D417) : le client n'additionne rien et affiche le refus tel quel.
 */
@Component({
  selector: 'app-modeles',
  imports: [FormsModule, MatButtonModule, MatFormFieldModule, MatInputModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h2>{{ 'pf.modeles.titre' | t }}</h2>
    @switch (etat()) {
      @case ('chargement') {
        <p role="status" data-testid="modeles-chargement">
          {{ 'pf.chargement' | t }}
        </p>
      }
      @case ('erreur') {
        <p class="erreur" role="alert" data-testid="modeles-erreur">
          <span class="icone" aria-hidden="true">✖</span>{{ message() }}
        </p>
        <button mat-stroked-button type="button" (click)="charger()">
          {{ 'liste.reessayer' | t }}
        </button>
      }
      @default {
        @if (modeles().length === 0) {
          <p role="status" data-testid="modeles-vide">
            {{ 'pf.modeles.vide' | t }}
          </p>
        } @else {
          <div class="defile">
            <table class="simple" data-testid="modeles">
              <caption class="sr-only">
                {{
                  'pf.modeles.titre' | t
                }}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{{ 'pf.col.nom' | t }}</th>
                  <th scope="col" class="num">{{ 'pf.col.version' | t }}</th>
                  <th scope="col">{{ 'pf.col.cibles' | t }}</th>
                  <th scope="col">{{ 'pf.col.fonds' | t }}</th>
                  <th scope="col">
                    <span class="sr-only">{{ 'pf.col.actions' | t }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                @for (m of modeles(); track m.id) {
                  <tr [attr.data-testid]="'modele-' + m.id">
                    <th scope="row">{{ m.name }}</th>
                    <td class="num">{{ m.version }}</td>
                    <td>
                      <ul class="liste-simple">
                        @for (c of m.targets; track c.assetClass) {
                          <li>{{ classe(c.assetClass) }} : {{ pct(c.target) }}</li>
                        }
                        @if (m.cashTarget !== undefined) {
                          <li>{{ 'pf.encaisse' | t }} : {{ pct(m.cashTarget) }}</li>
                        }
                      </ul>
                    </td>
                    <td>
                      <ul class="liste-simple">
                        @for (h of m.holdings; track h.symbol) {
                          <li>{{ h.symbol }} : {{ pct(h.weight) }}</li>
                        }
                      </ul>
                    </td>
                    <td>
                      <button
                        mat-stroked-button
                        type="button"
                        [attr.data-testid]="'nouvelle-version-' + m.id"
                        (click)="editer(m)"
                      >
                        {{ 'pf.modeles.nouvelleVersion' | t }}
                      </button>
                    </td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
        <p>
          <button mat-flat-button type="button" data-testid="nouveau-modele" (click)="nouveau()">
            {{ 'pf.modeles.nouveau' | t }}
          </button>
        </p>
      }
    }
    @if (confirmation()) {
      <p class="statut" role="status" data-testid="modele-confirmation">
        <span aria-hidden="true">✔</span>{{ confirmation() }}
      </p>
    }

    @if (formulaire()) {
      <section class="carte" aria-labelledby="titre-form" data-testid="modele-formulaire">
        <h2 id="titre-form">
          @if (cible(); as c) {
            {{ 'pf.form.titreVersion' | t: { nom: c.name, version: '' + (c.version + 1) } }}
          } @else {
            {{ 'pf.form.titreNouveau' | t }}
          }
        </h2>
        <form (ngSubmit)="enregistrer()" novalidate>
          <mat-form-field class="large">
            <mat-label>{{ 'pf.col.nom' | t }}</mat-label>
            <input
              matInput
              name="nom"
              autocomplete="off"
              maxlength="100"
              data-testid="champ-nom"
              [readonly]="cible() !== null"
              [ngModel]="nom()"
              (ngModelChange)="nom.set($event)"
            />
            @if (cible()) {
              <mat-hint>{{ 'pf.form.nomFixe' | t }}</mat-hint>
            }
          </mat-form-field>

          <fieldset>
            <legend>{{ 'pf.form.cibles' | t }}</legend>
            <p>
              <small>{{ 'pf.form.aidePourcentage' | t }}</small>
            </p>
            <div class="ligne-saisie">
              @for (c of classes; track c) {
                <mat-form-field>
                  <mat-label>{{ classe(c) }} (%)</mat-label>
                  <input
                    matInput
                    inputmode="decimal"
                    autocomplete="off"
                    [name]="'cible-' + c"
                    [attr.data-testid]="'cible-' + c"
                    [ngModel]="cibles()[c]"
                    (ngModelChange)="majCible(c, $event)"
                  />
                </mat-form-field>
              }
              <mat-form-field>
                <mat-label>{{ 'pf.encaisse' | t }} (%)</mat-label>
                <input
                  matInput
                  inputmode="decimal"
                  autocomplete="off"
                  name="cible-encaisse"
                  data-testid="cible-encaisse"
                  [ngModel]="encaisse()"
                  (ngModelChange)="encaisse.set($event)"
                />
              </mat-form-field>
            </div>
          </fieldset>

          <fieldset>
            <legend>{{ 'pf.form.fonds' | t: { max: '' + maxFonds } }}</legend>
            <datalist id="symboles-catalogue">
              @for (s of symboles(); track s) {
                <option [value]="s"></option>
              }
            </datalist>
            @for (l of fonds(); track $index) {
              <div class="ligne-saisie" [attr.data-testid]="'fonds-' + $index">
                <mat-form-field>
                  <mat-label>{{ 'pf.col.symbole' | t }}</mat-label>
                  <input
                    matInput
                    list="symboles-catalogue"
                    autocomplete="off"
                    [name]="'sym-' + $index"
                    [attr.data-testid]="'fonds-symbole-' + $index"
                    [ngModel]="l.symbole"
                    (ngModelChange)="majFonds($index, 'symbole', $event)"
                  />
                </mat-form-field>
                <mat-form-field>
                  <mat-label>{{ 'pf.col.poids' | t }} (%)</mat-label>
                  <input
                    matInput
                    inputmode="decimal"
                    autocomplete="off"
                    [name]="'poids-' + $index"
                    [attr.data-testid]="'fonds-poids-' + $index"
                    [ngModel]="l.poids"
                    (ngModelChange)="majFonds($index, 'poids', $event)"
                  />
                </mat-form-field>
                <button
                  mat-button
                  type="button"
                  [attr.data-testid]="'retirer-fonds-' + $index"
                  [attr.aria-label]="'pf.form.retirerFonds' | t: { n: '' + ($index + 1) }"
                  (click)="retirer($index)"
                >
                  {{ 'pf.form.retirer' | t }}
                </button>
              </div>
            }
            <button
              mat-stroked-button
              type="button"
              data-testid="ajouter-fonds"
              [disabled]="fonds().length >= maxFonds"
              (click)="ajouter()"
            >
              {{ 'pf.form.ajouterFonds' | t }}
            </button>
          </fieldset>

          <fieldset>
            <legend>{{ 'pf.form.bandes' | t }}</legend>
            <p>
              <small>{{ 'pf.form.aideBandes' | t }}</small>
            </p>
            <div class="ligne-saisie">
              <mat-form-field>
                <mat-label>{{ 'pf.form.bandeRelative' | t }} (%)</mat-label>
                <input
                  matInput
                  inputmode="decimal"
                  autocomplete="off"
                  name="bande-relative"
                  data-testid="bande-relative"
                  [ngModel]="bandeRelative()"
                  (ngModelChange)="bandeRelative.set($event)"
                />
              </mat-form-field>
              <mat-form-field>
                <mat-label>{{ 'pf.form.bandeEncaisse' | t }} (%)</mat-label>
                <input
                  matInput
                  inputmode="decimal"
                  autocomplete="off"
                  name="bande-encaisse"
                  data-testid="bande-encaisse"
                  [ngModel]="bandeEncaisse()"
                  (ngModelChange)="bandeEncaisse.set($event)"
                />
              </mat-form-field>
            </div>
          </fieldset>

          @if (erreurForm()) {
            <p class="erreur" role="alert" data-testid="form-erreur">
              <span class="icone" aria-hidden="true">✖</span>{{ erreurForm() }}
            </p>
          }
          <button mat-flat-button type="submit" data-testid="enregistrer" [disabled]="envoi()">
            {{ 'pf.form.enregistrer' | t }}
          </button>
          <button mat-button type="button" data-testid="annuler" (click)="fermer()">
            {{ 'pf.form.annuler' | t }}
          </button>
        </form>
      </section>
    }
  `,
})
export class Modeles {
  private readonly api = inject(PortfolioService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly classes = CLASSES;
  protected readonly maxFonds = MAX_FONDS;
  protected readonly classe = libelleClasse;
  protected readonly pct = afficherPourcentage;

  protected readonly etat = signal<Etat>('chargement');
  protected readonly message = signal('');
  protected readonly modeles = signal<Model[]>([]);
  protected readonly symboles = signal<string[]>([]);
  protected readonly confirmation = signal('');

  protected readonly formulaire = signal(false);
  /** Modèle dont on publie une nouvelle version ; null pour une création. */
  protected readonly cible = signal<Model | null>(null);
  protected readonly nom = signal('');
  protected readonly cibles = signal<Record<AssetClass, string>>(Modeles.ciblesVides());
  protected readonly encaisse = signal('');
  protected readonly fonds = signal<LigneFonds[]>([]);
  protected readonly bandeRelative = signal('');
  protected readonly bandeEncaisse = signal('');
  protected readonly erreurForm = signal('');
  protected readonly envoi = signal(false);

  constructor() {
    this.charger();
    this.api
      .listEtfs()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => this.symboles.set(l.items.map((f) => f.symbol)),
        error: () => undefined,
      });
  }

  private static ciblesVides(): Record<AssetClass, string> {
    return Object.fromEntries(CLASSES.map((c) => [c, ''])) as Record<AssetClass, string>;
  }

  protected charger(): void {
    this.etat.set('chargement');
    this.api
      .listModels()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (l) => {
          this.modeles.set(l.items);
          this.etat.set('pret');
        },
        error: (e: unknown) => {
          this.message.set(messageProbleme(e, 'pf.modeles.erreur'));
          this.etat.set('erreur');
        },
      });
  }

  protected nouveau(): void {
    this.cible.set(null);
    this.nom.set('');
    this.cibles.set(Modeles.ciblesVides());
    this.encaisse.set('');
    this.fonds.set([{ symbole: '', poids: '' }]);
    this.bandeRelative.set('');
    this.bandeEncaisse.set('');
    this.ouvrir();
  }

  protected editer(m: Model): void {
    this.cible.set(m);
    this.nom.set(m.name);
    // Une classe sans cible dans la version reçue s'affiche à 0 : le champ reste visible et modifiable.
    const cibles = Object.fromEntries(CLASSES.map((c) => [c, '0'])) as Record<AssetClass, string>;
    for (const c of m.targets) {
      cibles[c.assetClass] = fractionVersSaisie(c.target);
    }
    this.cibles.set(cibles);
    this.encaisse.set(fractionVersSaisie(m.cashTarget));
    this.fonds.set(
      m.holdings.map((h) => ({
        symbole: h.symbol,
        poids: fractionVersSaisie(h.weight),
      })),
    );
    this.bandeRelative.set(fractionVersSaisie(m.bands?.relative));
    this.bandeEncaisse.set(fractionVersSaisie(m.bands?.cashPoints));
    this.ouvrir();
  }

  private ouvrir(): void {
    this.erreurForm.set('');
    this.confirmation.set('');
    this.formulaire.set(true);
  }

  protected fermer(): void {
    this.formulaire.set(false);
  }

  protected majCible(c: AssetClass, valeur: string): void {
    this.cibles.update((x) => ({ ...x, [c]: valeur }));
  }

  protected majFonds(i: number, champ: keyof LigneFonds, valeur: string): void {
    this.fonds.update((l) => l.map((x, j) => (j === i ? { ...x, [champ]: valeur } : x)));
  }

  protected ajouter(): void {
    if (this.fonds().length < MAX_FONDS) {
      this.fonds.update((l) => [...l, { symbole: '', poids: '' }]);
    }
  }

  protected retirer(i: number): void {
    this.fonds.update((l) => l.filter((_, j) => j !== i));
  }

  /** Construit la demande ; retourne la clé du premier message d'erreur de saisie sinon. */
  private construire(): { modele: ModelWrite } | { erreur: string } {
    const nom = this.nom().trim();
    if (nom === '' || nom.length > 100) {
      return { erreur: traduire('pf.form.erreur.nom') };
    }
    const targets: ModelWrite['targets'] = [];
    for (const c of CLASSES) {
      const r = saisieVersFraction(this.cibles()[c]);
      if ('erreur' in r) {
        return {
          erreur: `${libelleClasse(c)} : ${traduire(r.erreur as CleTraduction)}`,
        };
      }
      targets.push({ assetClass: c, target: r.valeur });
    }
    const modele: ModelWrite = { name: nom, targets, holdings: [] };
    if (this.encaisse().trim() !== '') {
      const r = saisieVersFraction(this.encaisse());
      if ('erreur' in r) {
        return { erreur: `${traduire('pf.encaisse')} : ${traduire(r.erreur)}` };
      }
      modele.cashTarget = r.valeur;
    }
    const holdings: ModelHolding[] = [];
    for (const [i, l] of this.fonds().entries()) {
      const symbole = l.symbole.trim().toUpperCase();
      if (!/^[A-Z0-9]+:[A-Z0-9][A-Z0-9.-]*$/.test(symbole)) {
        return {
          erreur: traduire('pf.form.erreur.symbole', { n: '' + (i + 1) }),
        };
      }
      const r = saisieVersFraction(l.poids);
      if ('erreur' in r) {
        return { erreur: `${symbole} : ${traduire(r.erreur)}` };
      }
      holdings.push({ symbol: symbole, weight: r.valeur });
    }
    if (holdings.length === 0 || holdings.length > MAX_FONDS) {
      return {
        erreur: traduire('pf.form.erreur.nbFonds', { max: '' + MAX_FONDS }),
      };
    }
    modele.holdings = holdings;
    const bandes: Bands = {};
    if (this.bandeRelative().trim() !== '') {
      const r = saisieVersFraction(this.bandeRelative());
      if ('erreur' in r) {
        return {
          erreur: `${traduire('pf.form.bandeRelative')} : ${traduire(r.erreur)}`,
        };
      }
      bandes.relative = r.valeur;
    }
    if (this.bandeEncaisse().trim() !== '') {
      const r = saisieVersFraction(this.bandeEncaisse());
      if ('erreur' in r) {
        return {
          erreur: `${traduire('pf.form.bandeEncaisse')} : ${traduire(r.erreur)}`,
        };
      }
      bandes.cashPoints = r.valeur;
    }
    if (Object.keys(bandes).length > 0) {
      modele.bands = bandes;
    }
    return { modele };
  }

  protected enregistrer(): void {
    const r = this.construire();
    if ('erreur' in r) {
      this.erreurForm.set(r.erreur);
      return;
    }
    this.erreurForm.set('');
    this.envoi.set(true);
    const cible = this.cible();
    const appel = cible ? this.api.updateModel(cible.id, r.modele) : this.api.createModel(r.modele);
    appel.pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (m) => {
        this.envoi.set(false);
        this.formulaire.set(false);
        this.confirmation.set(
          traduire('pf.modeles.enregistre', {
            nom: m.name,
            version: '' + m.version,
          }),
        );
        this.charger();
      },
      error: (e: unknown) => {
        this.envoi.set(false);
        this.erreurForm.set(messageProbleme(e, 'pf.form.erreur.enregistrement'));
      },
    });
  }
}
