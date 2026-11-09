Module etf : sous-paquet ingest.

- `xlsx.go` : import du classeur SÉLECTION ET GESTION (`Parse`, `Importer.Import`, D350 à D354) ; `convert.go` : conversion des textes (« 19,6 % », « 880M$ »).
- Le dépôt est une interface (`Repository`) implémentée avec les handlers (P2-10).
- Tests : `xlsx_test.go` remplit une copie du gabarit (D33) ; aucun test d'intégration tant que le dépôt PostgreSQL n'existe pas.
- `csv.go` : import CSV générique de transactions (`ParseCSV`, `TxImporter.ImportCSV`, D390 à D395), format dans `docs/import-csv.md` ; dépôt injecté `TransactionRepository`.
