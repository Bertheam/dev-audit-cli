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
| Inventaire Docker confondu avec nettoyage | Exécution destructive involontaire | Allowlist stricte de commandes de lecture ; aucun `prune`, `rm`, `pull`, `run` ou build. |
| Toolchain uniquement dans Docker | Faux sentiment que l'application est cassée sur l'hôte | Relations `HOST`, déclaration `DOCKER` et présence locale `DOCKER_DAEMON` séparées. |
| Sortie Docker volumineuse ou lente | Scan bloqué ou mémoire excessive | Timeout par commande, timeout global, sortie bornée et budget de ressources. |
| Métadonnées Docker sensibles | Fuite de commandes, variables ou labels | Demander uniquement ID, référence, état, tailles et dates ; ne jamais collecter commandes, labels, env ou montages. |
| AVD volumineux mais encore utile | Perte de données d'émulateur ou de scénario de test | Marquer l'AVD sensible et `UNKNOWN` ; ne jamais déduire une action de sa taille seule. |
| Chemin de module Go provisoire | Renommage d'import avant publication | Fixer l'URL distante avant toute publication ou import public. |
