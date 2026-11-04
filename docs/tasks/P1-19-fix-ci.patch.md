# P1-19-fix : job Trivy pour `.github/workflows/ci.yml` (à approuver par le propriétaire)

`.github/` n'est pas modifié par cette PR (CLAUDE.md : les fichiers de CI ne changent qu'avec l'agent Plateforme et l'approbation du propriétaire). Voici le changement exact à appliquer après approbation. C'est le seul critère de P1-19-fix encore ouvert.

## Ajout, après le job `backup`

```yaml
  # P1-19-fix : Trivy sur toutes les images de la pile (D290). Hors `make ci` : construit les images (api, web, backup).
  trivy:
    runs-on: ubuntu-latest
    timeout-minutes: 25
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: make scan-images
        run: make scan-images
```

## Vérifications

- Aucune nouvelle action : `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1` est déjà utilisée par les autres jobs (SHA recoupé avec `gh api repos/actions/checkout/commits/v7.0.1`, 2026-11-04). Trivy tourne par son image Docker épinglée par digest, lancée par le Makefile ; Docker et Compose sont préinstallés sur `ubuntu-latest`.
- Le job échoue (code 1) dès qu'une image porte une vulnérabilité HIGH ou CRITICAL corrigeable, hors `.trivyignore`. Il échoue aussi quand une exception dépasse son échéance `exp:` (2026-12-04 pour toutes aujourd'hui) : prévoir de la renouveler avec une justification, jamais de la supprimer pour faire passer le job.
- Durée estimée : 8 à 12 minutes (construction de api, web et backup, environ 4 min ; téléchargement de la base Trivy et de 3 images tierces, environ 2 min ; 6 analyses, environ 3 min). `timeout-minutes: 25`. Mesure locale avec caches chauds : environ 4 min.
- Alternative moins coûteuse : un déclenchement `schedule` hebdomadaire plus `pull_request` limité aux chemins `deploy/**`, `backend/**`, `web/**`, `.trivyignore`, `Makefile`. Une base Trivy mise à jour peut faire échouer le job sans changement de code, ce qui est voulu (nouvelle CVE) mais bruyant sur les PR.
- Preuve exigée par la fiche (« lien d'une exécution ») : à ajouter à la PR après fusion du changement, avec une exécution rouge (CVE retirée de `.trivyignore` sur une branche jetable) puis verte.
