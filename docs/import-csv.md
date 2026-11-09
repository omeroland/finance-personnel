# Import CSV générique de transactions

Format de l'app (D10, D390 à D395) : aucun gabarit par courtier avant la phase 6. Code : `backend/internal/modules/etf/ingest/csv.go`. Exemple synthétique : `testdata/fixtures/transactions_generic.csv`.

## Fichier

- UTF-8, avec ou sans BOM. Taille maximale 10 Mo.
- Séparateur virgule ou point-virgule, détecté sur la première ligne.
- Première ligne non vide = en-têtes, insensibles à la casse et aux accents (`Quantité`, `quantite`, `QUANTITE`).
- Une ligne par transaction. Les lignes vides sont ignorées.

## Colonnes

| Colonne (alias) | Obligatoire | Contenu |
| --- | --- | --- |
| `date` | oui | `AAAA-MM-JJ` |
| `compte` (`account`) | oui | Nom d'un compte existant (casse ignorée) |
| `type` | oui | `buy`, `sell`, `dividend`, `drip`, `contribution`, `withdrawal`, `fee`, `transfer`, `split` |
| `symbole` (`symbol`) | selon le type | `BOURSE:SYMBOLE` ou symbole nu (préfixe `TSX:`) |
| `quantité` (`quantity`) | selon le type | Parts, strictement positif, fractions permises |
| `prix` (`price`) | selon le type | Prix par part, positif |
| `devise` (`currency`) | non | `CAD` ou `USD` ; vide = `CAD` avec avertissement |
| `frais` (`fee`) | non | Positif |
| `montant` (`amount`) | selon le type | Montant brut, positif |
| `ratio` | selon le type | Parts nouvelles pour une ancienne (fractionnement), > 0 |
| `contrepartie` (`counterpart`) | selon le type | Autre compte d'un transfert |
| `note` | non | 500 caractères au plus |

Champs requis par type : `buy`, `sell`, `drip` : symbole, quantité, prix. `dividend` : symbole, montant. `split` : symbole, ratio. `transfer` : contrepartie, et montant ou symbole + quantité. `contribution`, `withdrawal`, `fee` : montant.

Quantités, prix, montants et frais sont toujours positifs : le sens vient du type.

## Nombres

Lus en décimal exact. Virgule (`12,5`) ou point (`12.5`) décimal ; espaces, espaces insécables et `$` ignorés (`1 234,56`). Plus de 6 décimales : arrondi avec avertissement. Notation scientifique et nombres négatifs refusés.

## Route (synchrone, D420 et D421)

`POST /api/v1/accounts/{id}/imports/csv?broker=generic`, `multipart/form-data` avec le champ `file`. L'import est synchrone : la réponse 200 est le rapport (`CsvImportReport`), sans job. Seules les lignes du compte de la route sont écrites ; les autres sont refusées. 400 : fichier vide, sans en-têtes, illisible ou champ absent ; 404 : compte d'un autre propriétaire ; 413 : plus de 10 Mo.

## Rapport

Une entrée par problème : numéro de ligne (en-têtes = ligne 1), niveau (`info`, `warning`, `error`), colonne, message. Une ligne invalide est signalée et sautée ; les autres sont importées. Fichier vide, sans en-têtes reconnus ou sans colonne obligatoire : erreur du fichier entier.

## Dédoublonnage

Clé naturelle : compte, date, type, symbole, quantité, prix, montant, ratio. Ré-importer le même fichier n'ajoute rien (« déjà au registre »). Deux lignes identiques dans le même fichier sont deux transactions (rang d'occurrence). Corriger une écriture se fait par une écriture inverse dans le registre, pas par un ré-import.

## Exemple

```csv
date,compte,type,symbole,quantité,prix,devise,frais,montant,ratio,contrepartie,note
2024-01-10,CELI,contribution,,,,CAD,,5000.00,,,
2024-01-15,CELI,buy,XEQT,100,30.50,CAD,9.99,,,,
2024-05-01,CELI,split,XEQT,,,CAD,,,2,,
```
