# ADR 0017 — Actions ciblées Gradle/Xcode et nouvelle CLI Android

- Statut : accepté
- Date : 2026-09-08

## Contexte

Le premier nettoyage réel du Mac 01 a montré trois limites du plan : la commande
Android proposée utilisait encore `sdkmanager`, désormais déprécié localement,
et les distributions Gradle Wrapper ainsi que Xcode DerivedData étaient
reconstructibles mais exclues avec `UNSUPPORTED_ACTION`.

## Décision

L'inventaire Android recherche d'abord un exécutable `android` dans les
répertoires `cmdline-tools` standards. Il enregistre le gestionnaire et son
chemin absolu ; le plan produit alors `android sdk remove <package>`. Si la
nouvelle CLI n'est pas disponible, `sdkmanager --uninstall <package>` reste un
repli compatible.

Les distributions Gradle Wrapper complètes et les enfants immédiats de
`Xcode/DerivedData` reçoivent une stratégie explicite
`targeted_directory_removal`. Une sélection manuelle peut produire
`/bin/rm -R <chemin>` seulement si le chemin absolu et nettoyé correspond à la
structure inventoriée :

- `wrapper/dists/gradle-<version>-<bin|all>/<clé>` avec répertoire
  d'installation cohérent ;
- `Xcode/DerivedData/<entrée>`, sans jamais accepter `DerivedData` lui-même.

Ces actions restent `SIMULATION_ONLY`, exigent une confirmation explicite et
avertissent d'arrêter les processus Gradle, Xcode et `xcodebuild`. Aucun moteur
d'exécution n'est ajouté.

## Conséquences

Un nouveau scan est nécessaire pour bénéficier des métadonnées de stratégie et
du chemin Android. Les anciens rapports restent valides, mais leurs caches
Gradle/Xcode demeurent sans action prise en charge.

Le champ JSON `official_command` est conservé pour la compatibilité du schéma
v1 ; il contient désormais plus généralement une commande ciblée. Une future
version majeure du schéma pourra le renommer sans ambiguïté.
