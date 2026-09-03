# ADR 0007 — Inventaire explicite et taille logique

- Statut : accepté, complété par l'ADR 0010
- Date : 2026-09-02

## Contexte

L'inventaire doit identifier les toolchains installées et leur empreinte sans
lancer `sdkmanager`, Flutter, FVM, Java, Homebrew ou un autre gestionnaire. Sur
APFS, une « taille réelle » unique serait trompeuse à cause des fichiers creux,
de la compression, des clones, des snapshots et des blocs partagés.

## Décision

Le moteur reçoit cinq listes distinctes de racines explicitement autorisées :

- racines Android SDK ;
- racines Flutter SDK directes ;
- racines de cache FVM dont les enfants directs sont des SDK ;
- racines `GRADLE_USER_HOME` ;
- racines JDK, qui peuvent viser un `JAVA_HOME` ou un dossier de bundles macOS.

Il n'infère pas encore de chemin depuis l'environnement ou le dossier personnel.
Cette sélection appartiendra à la CLI, devra être visible avant le scan et reste
à valider au Lot 5.

Les ressources couvertes sont :

- plateformes Android, Build Tools, NDK courant ou `ndk-bundle`, et CMake ;
- SDK Flutter directs et versions stockées par FVM, avec version Dart embarquée
  lorsqu'elle est disponible ;
- distributions Gradle Wrapper complètes et caches des plugins AGP/Kotlin ;
- JDK directs ou sous un bundle `*.jdk/Contents/Home`.

Les versions proviennent uniquement de fichiers ou noms allowlistés :

- `source.properties` avec `AndroidVersion.ApiLevel` ou `Pkg.Revision` ;
- `bin/cache/flutter.version.json`, ancien fichier Flutter `version`, ou nom du
  dossier FVM ;
- structure du cache Wrapper et dossiers de version du cache de modules Gradle ;
- fichier JDK `release` avec `JAVA_VERSION`.

Une valeur non sûre, absente ou égale à une version Flutter inconnue reste sans
version. Les gestionnaires et exécutables ne sont jamais lancés. Les ressources
restent en `reference_status: UNKNOWN` jusqu'à la corrélation du Lot 4.

## Mesure de taille

`size_bytes` représente la somme des tailles logiques des fichiers réguliers du
dossier de la ressource. Les répertoires, fichiers spéciaux et cibles de
symlinks ne sont pas comptés. Le nombre de liens ou fichiers spéciaux ignorés est
indiqué en métadonnée.

Une permission refusée, une annulation ou un budget dépassé rend toute la mesure
incomplète : `size_bytes` est alors omis au lieu de présenter un total partiel.
La taille allouée APFS est différée tant qu'une sémantique fiable et explicable
n'est pas validée.

Limites par défaut : 4 096 candidats, 1 024 ressources, 500 000 entrées par
ressource, profondeur 64 et 64 Kio par fichier de métadonnées. Le contexte de
l'appelant borne la durée.

## Références de format

- plateforme Android :
  <https://android.googlesource.com/platform/prebuilts/fullsdk/platforms/+/refs/heads/main/android-36/source.properties>
- génération des métadonnées NDK :
  <https://android.googlesource.com/platform/ndk/+/d7c0ef0df49e73d47dcbc62ee6b0081cd2e7efb1/ndk/checkbuild.py>
- métadonnées de version Flutter observées dans le dépôt amont :
  <https://github.com/flutter/flutter/issues/187907>
- organisation modulaire des images JDK : <https://openjdk.org/jeps/220>

## Conséquences

L'inventaire est déterministe, local et testable sans dépendance externe. Il ne
couvre pas encore les images système Android, AVD, chemins automatiques ou
métriques APFS allouées. Ces éléments ne seront ajoutés que s'ils servent la
carte projet–toolchain sans créer une fausse estimation d'espace récupérable.
