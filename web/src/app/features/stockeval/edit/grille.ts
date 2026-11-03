import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, ViewEncapsulation, computed, inject, input, output, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { AgGridAngular } from 'ag-grid-angular';
import { AllCommunityModule, ColDef, ICellRendererParams, ModuleRegistry, ValueGetterParams, ValueSetterParams } from 'ag-grid-community';
import { AnnualFact, Currency, Metric } from '../../../api';
import { TraductionPipe, traduire } from '../../../i18n/t.pipe';
import { Cellule, LigneGrille, estPourcentage, faitsModifies, texteDeValeur, validerCellule } from './edit.data';

ModuleRegistry.registerModules([AllCommunityModule]);

/** Libellé de l'origine d'une valeur : « manuelle », « xlsx » ou le nom du fournisseur. */
export function libelleSource(source: string): string {
  return source === 'manual' ? traduire('edit.source.manual') : source;
}

/** Contenu d'une cellule : valeur, source en toutes lettres et badge « Conflit » avec icône (jamais la couleur seule). */
function rendreCellule(p: ICellRendererParams<LigneGrille>): HTMLElement {
  const racine = document.createElement('span');
  racine.className = 'cellule';
  const cellule = p.data?.cellules[Number(p.colDef?.colId?.slice(1))];
  if (!cellule) {
    return racine;
  }
  const valeur = document.createElement('span');
  valeur.className = 'cellule-valeur';
  valeur.textContent = estPourcentage(p.data!.metrique) ? `${cellule.texte} %` : cellule.texte;
  const source = document.createElement('small');
  source.className = 'cellule-source';
  source.textContent = libelleSource(cellule.source) + (cellule.modifiee ? ` · ${traduire('edit.modifiee')}` : '');
  racine.append(valeur, source);
  if (cellule.conflit) {
    const badge = document.createElement('small');
    badge.className = 'cellule-conflit';
    badge.dataset['testid'] = 'badge-conflit';
    badge.textContent = `⚠ ${traduire('edit.conflit')}`;
    racine.append(badge);
  }
  return racine;
}

/** Grille de saisie annuelle (AG Grid Community) : lignes = mesures, colonnes = années fiscales. */
@Component({
  selector: 'app-grille-annuelle',
  imports: [AgGridAngular, MatButtonModule, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  // Le contenu des cellules est créé par AG Grid : les styles doivent être globaux (préfixés par le sélecteur).
  encapsulation: ViewEncapsulation.None,
  styleUrl: './grille.scss',
  template: `
    <h2 id="grille-titre">{{ 'edit.grille.titre' | t }}</h2>
    <p class="aide">{{ 'edit.grille.aide' | t }}</p>
    <div class="ag-theme-quartz grille" role="region" aria-labelledby="grille-titre" data-testid="grille">
      <ag-grid-angular
        [theme]="'legacy'"
        [rowData]="lignes()"
        [columnDefs]="colonnes()"
        [defaultColDef]="defaut"
        [getRowId]="idLigne"
        [domLayout]="'autoHeight'"
        [suppressMovableColumns]="true"
        [suppressColumnVirtualisation]="true"
        [singleClickEdit]="false"
        [stopEditingWhenCellsLoseFocus]="true"
        (cellValueChanged)="apresChangement()"
      />
    </div>
    <p role="alert" class="erreur" data-testid="grille-erreur">{{ erreur() }}</p>

    @if (conflits().length) {
      <section class="conflits" data-testid="conflits" aria-labelledby="conflits-titre">
        <h3 id="conflits-titre">{{ 'edit.conflits.titre' | t }}</h3>
        <p class="aide">{{ 'edit.conflits.aide' | t }}</p>
        <ul>
          @for (c of conflits(); track c.cle) {
            <li [attr.data-testid]="'conflit-' + c.cle">
              <strong>{{ c.libelle }}</strong>
              @for (v of c.choix; track v.source) {
                <button mat-stroked-button type="button" [attr.data-testid]="'choisir-' + c.cle + '-' + v.source" (click)="choisir(c, v.valeur)">
                  {{ 'edit.conflits.garder' | t: { valeur: v.texte, source: v.libelleSource } }}
                </button>
              }
            </li>
          }
        </ul>
      </section>
    }

    <div class="actions">
      <button mat-flat-button type="button" data-testid="enregistrer-faits" [disabled]="enregistrement() || nbModifs() === 0" (click)="enregistrer()">
        {{ 'edit.enregistrer' | t }}
      </button>
      <span role="status" data-testid="nb-modifs">{{ 'edit.nbModifs' | t: { n: '' + nbModifs() } }}</span>
    </div>
  `,
})
export class GrilleAnnuelle {
  constructor() {
    // Feuille de style de la grille : fichier à part (angular.json, `inject: false`), chargé seulement avec cet écran.
    // Un fichier servi par l'origine respecte `style-src 'self'` (D114) ; le thème « legacy » n'injecte aucun <style>.
    const doc = inject(DOCUMENT);
    if (!doc.getElementById('ag-grid-css')) {
      const lien = doc.createElement('link');
      lien.id = 'ag-grid-css';
      lien.rel = 'stylesheet';
      lien.href = new URL('ag-grid.css', doc.baseURI).toString();
      doc.head.appendChild(lien);
    }
  }

  readonly lignes = input.required<LigneGrille[]>();
  readonly annees = input.required<number[]>();
  readonly deviseDefaut = input.required<Currency>();
  readonly enregistrement = input(false);
  readonly enregistrerFaits = output<AnnualFact[]>();
  readonly resoudre = output<AnnualFact>();

  protected readonly erreur = signal('');
  protected readonly nbModifs = signal(0);
  protected readonly defaut: ColDef<LigneGrille> = { editable: true, sortable: false, suppressMovable: true, resizable: false, width: 120 };
  protected readonly idLigne = (p: { data: LigneGrille }): string => p.data.metrique;

  protected readonly colonnes = computed<ColDef<LigneGrille>[]>(() => [
    {
      colId: 'metrique',
      headerName: traduire('edit.col.mesure'),
      pinned: 'left',
      editable: false,
      width: 220,
      valueGetter: (p: ValueGetterParams<LigneGrille>) => (p.data ? traduire(`edit.metrique.${p.data.metrique}`) : ''),
    },
    ...this.annees().map<ColDef<LigneGrille>>((annee) => ({
      colId: `a${annee}`,
      headerName: `${annee}`,
      valueGetter: (p: ValueGetterParams<LigneGrille>) => p.data?.cellules[annee]?.texte ?? '',
      valueSetter: (p: ValueSetterParams<LigneGrille>) => this.modifier(p.data, annee, String(p.newValue ?? '')),
      cellRenderer: rendreCellule,
      cellEditor: 'agTextCellEditor',
    })),
  ]);

  protected readonly conflits = computed(() =>
    this.lignes().flatMap((l) =>
      Object.entries(l.cellules).flatMap(([annee, c]) => {
        if (!c?.conflit) {
          return [];
        }
        const cle = `${l.metrique}-${annee}`;
        return [
          {
            cle,
            metrique: l.metrique,
            annee: Number(annee),
            devise: c.devise,
            libelle: `${traduire(`edit.metrique.${l.metrique}`)} ${annee}`,
            choix: c.conflit.values.map((v) => ({
              source: v.source,
              libelleSource: libelleSource(v.source),
              valeur: v.value,
              texte: (estPourcentage(l.metrique) ? `${texteDeValeur(l.metrique, v.value)} %` : texteDeValeur(l.metrique, v.value)),
            })),
          },
        ];
      }),
    ),
  );

  /** Valide la saisie ; refuse (valeur inchangée) et annonce l'erreur si elle est invalide ou vide. */
  private modifier(ligne: LigneGrille, annee: number, saisie: string): boolean {
    const texte = saisie.trim();
    const existante = ligne.cellules[annee];
    if (existante && texte === existante.texte) {
      return false;
    }
    const nom = `${traduire(`edit.metrique.${ligne.metrique}`)} ${annee}`;
    if (texte === '') {
      this.erreur.set(traduire('edit.erreur.vide', { cellule: nom }));
      return false;
    }
    const resultat = validerCellule(ligne.metrique, texte);
    if ('erreur' in resultat) {
      this.erreur.set(`${nom} : ${traduire(resultat.erreur)}`);
      return false;
    }
    this.erreur.set('');
    const cellule: Cellule = {
      texte: texte.replace(/[\s\u00a0\u202f]/g, '').replace('.', ','),
      valeur: resultat.valeur,
      source: 'manual',
      devise: existante?.devise ?? this.deviseDefaut(),
      modifiee: true,
      conflit: existante?.conflit,
    };
    ligne.cellules[annee] = cellule;
    return true;
  }

  protected apresChangement(): void {
    this.nbModifs.set(faitsModifies(this.lignes()).length);
  }

  protected enregistrer(): void {
    this.enregistrerFaits.emit(faitsModifies(this.lignes()));
  }

  protected choisir(c: { annee: number; metrique: Metric; devise: Currency }, valeur: string): void {
    this.resoudre.emit({ fiscalYear: c.annee, metric: c.metrique, value: valeur, currency: c.devise, source: 'manual' });
  }
}
