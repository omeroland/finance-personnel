Module stockeval : sous-paquet ingest.

- `xlsx.go` : import de la feuille Simply Investing (`Importer.Import`, D130 à D132).
- Tests : `workbook_test.go` remplit une copie du gabarit (D33) ; `make test-int` ajoute un test sur PostgreSQL.
