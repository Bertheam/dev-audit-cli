# Registre initial des risques

| Risque | Impact | Réponse Phase 0 |
|---|---|---|
| Expressions Gradle dynamiques | Mauvaise version ou fausse certitude | Reconnaître un sous-ensemble explicite ; sinon `PROBABLY_REQUIRED` ou `UNKNOWN`. |
| Projets hors des racines | Faux sentiment qu'une ressource n'est pas référencée | Afficher les racines et exclusions dans chaque rapport. |
| Auto-détection incomplète | Faux `MISSING` ou faux « sans référence » | Marquer la couverture heuristique incomplète ; n'autoriser que les correspondances positives. |
| Faux chemin détecté | Inventaire sans rapport avec l'environnement de développement | Exiger plusieurs marqueurs structurels, dédupliquer par identité de fichier et tester sur machine réelle. |
| Recherche profonde intrusive | Lecture de chemins privés ou parcours trop long | Limiter aux emplacements de développement, ignorer les zones générées, borner profondeur/entrées et ne rien transmettre. |
| Symlink vers un chemin externe | Sortie du périmètre autorisé | Ne pas suivre ; produire un résumé par racine. |
| Permissions refusées ou chemin disparu | Scan incomplet | Continuer et enregistrer un diagnostic de couverture. |
| Très grands dossiers | Scan trop lent | Budgets configurables et mesures bornées. |
| Mesure APFS trompeuse | Taille mal interprétée | Nommer explicitement la métrique et éviter « récupérable ». |
| Secrets dans les configurations | Fuite dans les rapports | Exporter seulement clé et valeur non sensible allowlistée. |
| Ordre non déterministe | Golden tests instables | Trier toutes les collections avant reporting. |
| Commande locale imprévisible | Build ou téléchargement implicite | Analyse statique d'abord ; allowlist, timeout et sortie limitée. |
| Dérive vers Docker/nettoyage | Dilution du test produit | Limiter la Phase 0 à la lecture statique des Dockerfile ; aucun daemon, lancement ou nettoyage. |
| Toolchain uniquement dans Docker | Faux sentiment que l'application est cassée sur l'hôte | Relations `HOST` et `DOCKER` séparées ; Docker ne devient jamais une installation locale. |
| Chemin de module Go provisoire | Renommage d'import avant publication | Fixer l'URL distante avant toute publication ou import public. |
