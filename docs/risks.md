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
| Simulateur iOS booté ou contenant des données utiles | Perte d'état applicatif, de tests ou de diagnostics | Marquer chaque appareil CoreSimulator sensible et `UNKNOWN` ; aucune action Apple dans le plan. |
| Runtime Xcode en cours de téléchargement ou d'installation | Corruption d'un composant et indisponibilité de la plateforme | Considérer tout téléchargement observable comme potentiellement actif ; ne jamais modifier les composants et différer le gate réel. |
| Taille Xcode/CoreSimulator lente à mesurer | Dépassement du timeout ou rapport incomplet | Réutiliser les budgets de parcours ; ne publier aucune taille partielle et conserver un diagnostic. |
| Ancienneté déduite d'un `mtime` ou d'une création | Faux classement d'une ressource encore utile | Accepter uniquement une dernière utilisation allowlistée et enregistrer le seuil appliqué. |
| `NO_REFERENCE_FOUND` présenté comme inutilisé | Suppression d'une ressource utile hors couverture | Exiger aussi reconstruction prouvée et ancienneté fiable pour `ORPHELINE_PROBABLE` ; sinon conserver `INCONNUE`. |
| Tailles Docker partagées additionnées | Surestimation de l'espace récupérable | Séparer taille observée et estimation potentielle ; exclure cache partagé ou mutable et ne pas publier de total garanti. |
| Plan automatique incluant une ressource risquée | Perte future si le plan est un jour exécuté | Invariant de domaine : exiger `ORPHELINE_PROBABLE` et une estimation ; interdire `SENSIBLE`, `INCONNUE` et `UTILISEE`. |
| Commande de plan trop large ou injectable | Nettoyage hors de la ressource choisie | Construire des tableaux d'arguments depuis des identifiants validés ; interdire shell, `system prune`, options forcées et suppressions directes. |
| Plan modifié ou rapport remplacé entre revue et action | Exécution future différente de la simulation validée | Lier le plan au SHA-256 source, calculer son identifiant sur le contenu et créer le fichier sans écrasement. |
| Estimations additionnées prises pour un gain garanti | Mauvaise décision de capacité | Nom explicite du total, somme des seules estimations connues sélectionnées et avertissement systématique. |
| Chemin de module Go provisoire | Renommage d'import avant publication | Fixer l'URL distante avant toute publication ou import public. |
