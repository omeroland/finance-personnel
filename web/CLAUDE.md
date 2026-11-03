# web/CLAUDE.md

Complète le `CLAUDE.md` racine pour tout ce qui est sous `web/`.

## Pile

- Angular, dernière version stable au moment de P1-14, épinglée dans `package.json` (versions exactes, pas de `^`).
- Composants autonomes et signaux ; pas de `NgModule`. Détection de changements sans zone si la version retenue la déclare stable.
- Angular Material pour les composants ; AG Grid Community pour la grille de saisie annuelle.
- Authentification : `angular-auth-oidc-client`, code d'autorisation + PKCE (S256).
- Client d'API généré par openapi-generator (`typescript-angular`) dans `src/app/api/` : ne jamais l'éditer.

## Organisation

```text
web/src/app/
├── api/                  généré
├── core/                 auth, intercepteurs, configuration à l'exécution (config.json)
├── shell/                coquille multi-modules (menu latéral)
└── features/
    └── stockeval/        une route chargée à la demande par module
```

## Règles

- Aucun calcul financier : les scores, statuts et verdicts viennent de l'API.
- Ratios reçus en fraction ; affichage en pourcentage avec `Intl.NumberFormat('fr-CA')`. Saisie en pourcentage, envoi en fraction.
- Un statut ne s'exprime jamais par la couleur seule : texte et icône toujours présents.
- Textes de l'interface en français (fr-CA) ; aucune chaîne en dur hors des fichiers de traduction.
- Développement contre le serveur simulé (`make mock`) tant que l'API réelle n'est pas fusionnée.

## Tests

- Unitaires : Vitest ou Karma, selon le défaut de la version retenue.
- Bout en bout : Playwright dans `e2e/web/`. Chaque écran a au moins un scénario.
