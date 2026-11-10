# Tâches — phase 1 (MVP)

Une tâche passe à « prête » quand toutes ses dépendances sont « terminée ». Mettre à jour la colonne Statut à chaque fusion.

| ID | Tâche | Agent | Dépend de | Statut |
| --- | --- | --- | --- | --- |
| [P1-01](P1-01.md) | Squelette du monodépôt | Plateforme | — | terminée |
| [P1-02](P1-02.md) | Documents et données de test dans le dépôt | Orchestrateur | P1-01 | terminée (réserve D300) |
| [P1-03](P1-03.md) | Contrat OpenAPI du module 1 et génération | Backend Go | P1-01 | terminée |
| [P1-04](P1-04.md) | Référentiel `rules.yaml` et son chargeur | Backend Go | P1-02 | terminée |
| [P1-05](P1-05.md) | Moteur : calcul des mesures | Backend Go | P1-04 | terminée |
| [P1-06](P1-06.md) | Moteur : pointage, statuts, profils et verdict | Backend Go | P1-04, P1-05 | terminée |
| [P1-07](P1-07.md) | Oracle LibreOffice | Vérificateur | P1-02 | terminée |
| [P1-08](P1-08.md) | Moteur : mode compat | Backend Go | P1-05, P1-07 | terminée |
| [P1-09](P1-09.md) | Schéma SQL du socle et du module 1 | Backend Go | P1-01 | terminée |
| [P1-10](P1-10.md) | Dépôts PostgreSQL du module 1 | Backend Go | P1-09 | terminée |
| [P1-11](P1-11.md) | Import de la feuille Simply Investing | Données | P1-02, P1-10 | terminée |
| [P1-12](P1-12.md) | Handlers d'API du module 1 | Backend Go | P1-03, P1-06, P1-08, P1-10 | terminée |
| [P1-13](P1-13.md) | Authentification JWT et realm Keycloak | Plateforme | P1-03 | terminée |
| [P1-14](P1-14.md) | Angular : squelette, connexion et coquille multi-modules | Frontend Angular | P1-03, P1-13 | terminée |
| [P1-15](P1-15.md) | Angular : liste de suivi | Frontend Angular | P1-14 | terminée |
| [P1-16](P1-16.md) | Angular : fiche d'une action | Frontend Angular | P1-15 | terminée |
| [P1-17](P1-17.md) | Angular : grille de saisie et règles 1 à 4 | Frontend Angular | P1-16 | terminée |
| [P1-18](P1-18.md) | Angular : import de la feuille | Frontend Angular | P1-14 | terminée |
| [P1-19](P1-19.md) | Conteneurs, Compose, Traefik et en-têtes de sécurité | Plateforme | P1-01 | terminée |
| [P1-20](P1-20.md) | Sauvegardes chiffrées et restauration automatisée | Plateforme | P1-09, P1-19 | terminée |
| [P1-21](P1-21.md) | Observabilité | Plateforme | P1-12 | terminée |
| [P1-22](P1-22.md) | Intégration bout en bout | Vérificateur | P1-12, P1-17, P1-18 | terminée |
| [P1-23](P1-23.md) | Déploiement sur le VPS | Plateforme | P1-13, P1-19, P1-20 | à faire |
| [P1-24](P1-24.md) | Porte de phase 1 | Vérificateur | toutes | à faire |
| [P1-13-fix](P1-13-fix.md) | Correctif porte P1-24 : console d'administration Keycloak exposée | Plateforme | P1-13, P1-19 | terminée |
| [P1-19-fix](P1-19-fix.md) | Correctif porte P1-24 : Trivy sur toutes les images et dans la CI | Plateforme | P1-19 | terminée |

## Décisions techniques à ajouter au journal (D23 à D34)

| # | Sujet | Décision MVP | À réévaluer |
| --- | --- | --- | --- |
| D23 | Migrations | goose, SQL pur, embarqué dans le binaire | golang-migrate |
| D24 | Lecture xlsx | excelize (Go) | — |
| D25 | Argent en Go | shopspring/decimal | cockroachdb/apd si besoin de précision arbitraire |
| D26 | Accès PostgreSQL | pgx v5 + sqlc | — |
| D27 | Chaîne du contrat | oapi-codegen (strict), openapi-generator `typescript-angular`, Prism, Spectral | Orval pour Angular |
| D28 | Oracle | Python + LibreOffice sans interface, recalcul forcé par UNO | — |
| D29 | Authentification | go-oidc v3 côté API ; angular-auth-oidc-client côté web | — |
| D30 | Instruments | `core.instrument` créé dès la phase 1 ; une action est un instrument de type `stock` | — |
| D31 | Unités à l'import | Ratio de distribution ou dette / capitaux propres > 5, ou rendement > 1 : lu comme un pourcentage, divisé par 100, avec avertissement | Lire le format de cellule |
| D32 | Rafraîchissement par fournisseur | Route au contrat, répond 501 jusqu'à la phase 3 | — |
| D33 | Données de test xlsx | Ta feuille fournie est un gabarit vide ; les tests remplissent une copie par programme | Une copie réelle anonymisée |
| D34 | Limitation de débit et audit | Débit limité par Traefik ; journal d'audit écrit par les dépôts | Limitation dans l'API |

# Tâches — phase 2 (Portefeuille de FNB)

Fiches rédigées d'avance (D302). Phase ouverte sans la signature de la porte de phase 1, sur instruction du propriétaire (D306).

| ID | Tâche | Agent | Dépend de | Statut |
| --- | --- | --- | --- | --- |
| [P2-01](P2-01.md) | Contrat OpenAPI du module 2 et du socle | Backend Go | P1-03 | terminée |
| [P2-02](P2-02.md) | Schéma : comptes, transactions, relevés, profils FNB, modèles | Backend Go | P2-01 | terminée |
| [P2-03](P2-03.md) | Moteur : exposition par transparence et RFG pondéré | Backend Go | P2-02 | terminée |
| [P2-04](P2-04.md) | Moteur : plan d'achat en parts entières | Backend Go | P2-03 | terminée |
| [P2-05](P2-05.md) | Moteur : rééquilibrage par bandes | Backend Go | P2-03 | terminée |
| [P2-06](P2-06.md) | Moteur : rendement pondéré dans le temps et TRI | Backend Go | P2-02 | terminée |
| [P2-07](P2-07.md) | Oracle et vecteurs de l'outil SÉLECTION ET GESTION | Vérificateur | P2-01 | terminée |
| [P2-08](P2-08.md) | Import de l'outil SÉLECTION ET GESTION | Données | P2-02 | terminée |
| [P2-09](P2-09.md) | Import CSV générique de transactions | Données | P2-02 | terminée |
| [P2-10](P2-10.md) | Handlers : comptes, transactions, relevés, positions, catalogue | Backend Go | P2-02, P2-08, P2-09 | terminée (import CSV synchrone, P2-10b) |
| [P2-11](P2-11.md) | Handlers : modèles, plans, exposition, performance | Backend Go | P2-03, P2-04, P2-05, P2-06, P2-10 | terminée |
| P2-11b | Ajustements du contrat de phase 2 : prix saisis, base du TRI, cotisation par compte | Backend Go | P2-11 | terminée |
| [P2-12](P2-12.md) | Web : comptes, registre, relevés, catalogue | Frontend Angular | P2-10 | terminée |
| [P2-13](P2-13.md) | Web : portefeuille modèle, plans et tableau de bord | Frontend Angular | P2-11, P2-12 | à faire |
| [P2-14](P2-14.md) | Porte de phase 2 et parcours complet | Vérificateur | P2-13 | à faire |
