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

- [ ] Définir les racines d'inventaire Android, Flutter/FVM et JDK.
- [ ] Inventorier versions et chemins sans lancer de gestionnaire de paquets.
- [ ] Mesurer les tailles avec budgets et diagnostics.
- [ ] Définir taille logique et, si justifié, taille allouée APFS.

## Lot 4 — Correlation

- [ ] Normaliser les versions et contraintes.
- [ ] Associer exigences explicites et ressources exactes.
- [ ] Représenter ressources manquantes et correspondances ambiguës.
- [ ] Calculer `NO_REFERENCE_FOUND` uniquement dans la couverture analysée.

## Lot 5 — Reporting

- [ ] `dev-audit scan`.
- [ ] `dev-audit explain`.
- [ ] Rapport terminal.
- [ ] Export JSON v1 et validation de schéma.
- [ ] Golden tests et codes de sortie.

## Lot 6 — Validation

- [ ] Construire un paquet macOS de test.
- [ ] Tester sur 10 à 15 machines avec vérité terrain.
- [ ] Mesurer associations explicites, erreurs et utilité perçue.
- [ ] Documenter la décision GO/PIVOT/STOP.
