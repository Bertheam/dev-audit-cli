# ADR 0010 — Auto-détection bornée des racines

- Statut : accepté
- Date : 2026-09-03

## Contexte

Le premier test réel a montré qu'exiger d'un utilisateur les chemins Android,
Flutter, FVM, Gradle, JDK et projets rend le MVP fragile. Un utilisateur non
expert peut omettre une installation et provoquer un faux sentiment de
couverture. La CLI doit être immédiatement utile sans configuration préalable,
sans pour autant scanner silencieusement tout le disque ni exécuter les outils
trouvés.

## Décision

`dev-audit scan` active l'auto-détection par défaut pour chaque famille dont
aucune racine explicite n'est fournie. Il cherche successivement :

- variables d'environnement documentées ou conventionnelles ;
- exécutables `flutter`, `adb`, `java` et `javac` présents dans le `PATH`, sans
  les lancer ;
- emplacements macOS, Homebrew, Gradle, SDKMAN, asdf, mise et applications
  JetBrains usuels ;
- marqueurs structurels dans une recherche profonde limitée aux dossiers de
  développement courants.

La recherche est bornée par le contexte global, 250 000 entrées par emplacement,
750 000 au total et une profondeur de 10. Une zone volumineuse n'empêche donc
pas la recherche de continuer dans les suivantes. Le parcours profond ne suit
pas les symlinks et ignore les répertoires générés, caches de projet,
environnements virtuels et fixtures `testdata`. Un exécutable trouvé dans le
`PATH` peut être résolu vers sa cible, qui doit ensuite respecter tous les
marqueurs structurels.
`--deep-search=false` désactive seulement la passe approfondie.
`--auto-detect=false` rétablit le mode entièrement explicite.

Une racine explicitement fournie remplace la détection automatique de sa
famille. Les résultats automatiques sont exposés dans les diagnostics avec leur
provenance.

## Sémantique de couverture

L'auto-détection est une preuve positive, jamais une preuve exhaustive :

- une ressource trouvée peut satisfaire une exigence et produire `MATCHED` ;
- une famille d'inventaire automatique ne peut pas produire `MISSING` ;
- des racines de projets automatiques ne peuvent pas produire
  `NO_REFERENCE_FOUND`.

Les conclusions d'absence restent réservées aux périmètres explicitement
déclarés et parcourus sans avertissement ni erreur.

## Marqueurs

- Android SDK : au moins deux dossiers caractéristiques parmi `platforms`,
  `build-tools`, `ndk`, `cmake`, `platform-tools` et `cmdline-tools` ;
- Flutter SDK : fichier `bin/flutter` et dossier `packages/flutter` ;
- FVM : cache dont un enfant direct respecte les marqueurs Flutter ;
- Gradle User Home : Wrapper, cache de modules ou dossier `jdks` ;
- JDK : `release`, `bin/java` et `bin/javac` ;
- projet : marqueurs Flutter ou paire Gradle Settings/Wrapper déjà reconnue par
  Discovery.

## Références

- Android `ANDROID_HOME` et compatibilité `ANDROID_SDK_ROOT` :
  <https://developer.android.com/tools/variables>
- Flutter dans le `PATH` : <https://docs.flutter.dev/install/add-to-path>
- Gradle User Home, `~/.gradle` et `jdks` :
  <https://docs.gradle.org/current/userguide/directory_layout.html>
- auto-détection des toolchains Gradle :
  <https://docs.gradle.org/current/userguide/toolchains.html>
- configuration FVM et `FVM_CACHE_PATH` :
  <https://github.com/conceptadev/fvm/blob/main/docs/pages/documentation/getting-started/configuration.mdx>

## Conséquences

La commande simple fonctionne sans connaissance des chemins et conserve les
options répétables pour les audits reproductibles. Le parcours automatique peut
produire des diagnostics de limite ou manquer une installation atypique ; ces
cas restent `UNKNOWN`. La détection statique des toolchains déclarées dans
Docker a ensuite été traitée séparément par l'ADR 0011.
