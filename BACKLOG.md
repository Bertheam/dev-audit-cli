# Backlog Phase 0

## Lot 0 — Fondation

- [x] Initialiser Git.
- [x] Conserver le dossier produit original dans `docs/vision.md`.
- [x] Recentrer le README sur l'audit Flutter/Android en lecture seule.
- [x] Choisir Go et documenter son installation.
- [x] Définir l'architecture modulaire.
- [x] Écrire les ADR initiaux.
- [x] Définir le modèle de domaine et le schéma JSON v1.
- [x] Ajouter un rapport JSON minimal et des fixtures synthétiques.
- [x] Exécuter `go test ./...` après installation de Go.

Gate validé : aucun scanner réel, aucune primitive destructive, `go test ./...`
et `go vet ./...` réussis avec Go 1.27.1.

## Lot 1 — Discovery

- [x] Valider et canoniser les racines explicites.
- [x] Implémenter les exclusions configurables.
- [x] Détecter projets Flutter et Android.
- [x] Dédupliquer les projets imbriqués.
- [x] Gérer permissions, chemins disparus et entrées malformées.
- [x] Refuser de suivre les symlinks hors périmètre.
- [x] Définir des budgets de parcours.

Gate validé : tests unitaires, fixtures, erreurs simulées, symlinks, budgets et
invariance des fichiers passent. `go test -race -cover ./...` rapporte 89,1 %
de couverture pour `internal/discovery`. Aucun adaptateur de production ne
contient de primitive d'écriture ou de destruction.

## Lot 2 — Analyseurs

- [x] `.fvmrc`, flavors et ancien fichier FVM documenté.
- [x] Contraintes Dart et Flutter de `pubspec.yaml`.
- [x] Distribution Gradle Wrapper sans exposer l'URL complète.
- [x] AGP/Kotlin via plugins, buildscript et catalogues de versions utilisés.
- [x] `compileSdk`, `targetSdk`, `minSdk`, NDK et CMake.
- [x] Java/Kotlin toolchains explicitement déclarées.
- [x] Règles d'incertitude pour propriétés et plugins dynamiques.

Gate validé : cas explicites, dynamiques, historiques et malformés couverts ;
analyse bornée et sans sous-processus ; aucune version dynamique inventée ;
fixtures inchangées après dix analyses. `go test -race -cover ./...` rapporte
83,0 % de couverture pour `internal/analyzers`.

## Lot 3 — Inventory

- [x] Définir des racines typées Android, Flutter, FVM, Gradle et JDK.
- [x] Inventorier versions et chemins sans lancer de gestionnaire de paquets.
- [x] Mesurer les tailles avec budgets et diagnostics.
- [x] Définir la taille logique et différer la taille allouée APFS non fiable.

Gate validé : erreurs partielles, limites et symlinks sont couverts ; une mesure
incomplète n'expose aucun total partiel ; contenu et mtimes restent inchangés
après dix passages. `go test -race -cover ./...` rapporte 80,8 % de couverture
pour `internal/inventory`.

## Lot 4 — Correlation

- [x] Normaliser les versions et contraintes.
- [x] Associer exigences explicites et ressources exactes.
- [x] Représenter ressources manquantes et correspondances ambiguës.
- [x] Calculer `NO_REFERENCE_FOUND` uniquement dans la couverture analysée.

Gate validé : chaque exigence reçoit une relation déterministe ; les candidats
incertains empêchent les faux `MISSING` et faux `NO_REFERENCE_FOUND` ; la
couverture d'inventaire est déclarée par type de ressource. `go test -race
-cover ./...` rapporte 87,7 % de couverture pour `internal/correlation`.

## Lot 5 — Reporting

- [x] `dev-audit scan`.
- [x] `dev-audit explain`.
- [x] Rapport terminal.
- [x] Export JSON v1 et validation de schéma.
- [x] Golden tests et codes de sortie.

Gate validé : le pipeline complet est exposé avec timeout global et racines
typées ; le JSON est validé contre le schéma 2020-12 embarqué ; les rapports
terminal et JSON possèdent des golden tests ; les codes `0`, `1`, `2` et `3`
sont couverts. Avec le détecteur de concurrence, la couverture atteint 95,1 %
pour `internal/application`, 78,2 % pour `internal/report` et 79,3 % pour la CLI.

## Lot 6 — Validation

- [x] Fournir un installateur local produisant la commande autonome `dev-audit`.
- [ ] Construire un paquet macOS de test distribuable à d'autres machines.
- [ ] Tester sur 10 à 15 machines avec vérité terrain.
- [ ] Mesurer associations explicites, erreurs et utilité perçue.
- [ ] Documenter la décision GO/PIVOT/STOP.

Retours du premier Mac :

- [x] Consigner un résultat pseudonymisé dans
  [`docs/validation-results/2026-09-03-mac-01.md`](docs/validation-results/2026-09-03-mac-01.md).
- [x] Rendre `scan` utilisable sans fournir manuellement les chemins.
- [x] Ajouter une recherche profonde bornée et des signatures structurelles.
- [x] Interdire `MISSING` et `NO_REFERENCE_FOUND` sur couverture heuristique.
- [x] Réduire le bruit des diagnostics de symlinks à un résumé par racine.
- [x] Distinguer une toolchain hôte d'une toolchain JDK déclarée par Docker.
- [ ] Étendre prudemment l'analyse Docker aux toolchains non-Java pertinentes.
