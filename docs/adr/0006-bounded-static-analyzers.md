# ADR 0006 — Analyseurs statiques bornés et conservateurs

- Statut : accepté
- Date : 2026-09-02

## Contexte

Les fichiers Flutter et Gradle peuvent combiner valeurs littérales, alias,
propriétés, variables d'environnement et code arbitraire. Exécuter Gradle,
Flutter ou un gestionnaire de paquets pour résoudre ces expressions pourrait
déclencher un build, un téléchargement ou du code appartenant au projet.

## Décision

Le Lot 2 repose exclusivement sur une lecture statique et bornée :

- fichiers Flutter autorisés : `.fvmrc`, `.fvm/fvm_config.json` et
  `pubspec.yaml` à la racine du projet ;
- fichiers Android autorisés : `settings.gradle(.kts)`, `build.gradle(.kts)`,
  `gradle/wrapper/gradle-wrapper.properties` et
  `gradle/libs.versions.toml` ;
- aucun autre YAML, JSON, TOML, fichier de propriétés ou manifeste n'est lu ;
- aucun sous-processus, shell, build, plugin ou script Gradle n'est exécuté ;
- aucun symlink n'est suivi, y compris le dossier historique `.fvm` ;
- limites par défaut : 50 000 entrées, 256 fichiers, profondeur 12 et 1 Mio
  par fichier ;
- parseurs volontairement limités aux déclarations usuelles tenant sur une
  ligne ; les constructions non reconnues restent absentes ou diagnostiquées.

Chaque exigence possède un identifiant stable et au moins une preuve avec
fichier, clé, ligne, règle et valeur observée minimisée. L'URL complète du
Wrapper n'est jamais exportée : seule la version extraite du nom de
distribution est conservée. Les autres versions doivent aussi respecter une
forme bornée et sans URL avant d'être recopiées dans une preuve.

## Règles de confiance

- une valeur littérale ou un alias de catalogue effectivement utilisé et
  résolu produit `REQUIRED_EXPLICITLY` ;
- une indirection connue comme `flutter.compileSdkVersion` ou
  `libs.versions.*` produit `PROBABLY_REQUIRED` sans version ;
- une propriété, variable d'environnement ou expression arbitraire produit
  `UNKNOWN` sans version ;
- `sourceCompatibility` produit `PROBABLY_REQUIRED`, car ce niveau de langage
  ne prouve pas le JDK utilisé pour exécuter Gradle ;
- plusieurs contraintes distinctes sont signalées sans conclure qu'elles sont
  incompatibles.

## Formats couverts

- FVM actuel, versions par flavor et ancien `flutterSdkVersion` ;
- contraintes Dart et Flutter sous le bloc `environment` du pubspec ;
- distribution Gradle Wrapper ;
- versions AGP/Kotlin via plugins DSL, `buildscript` et catalogues utilisés ;
- `compileSdk`, `targetSdk`, `minSdk`, NDK et CMake ;
- toolchains Java et Kotlin explicitement déclarées.

Les règles sont alignées sur les documentations de référence :

- <https://dart.dev/tools/pub/pubspec>
- <https://github.com/leoafarias/fvm/blob/main/docs/pages/documentation/getting-started/configuration.mdx>
- <https://docs.gradle.org/current/userguide/gradle_wrapper.html>
- <https://docs.gradle.org/current/userguide/version_catalogs.html>
- <https://developer.android.com/build/releases/about-agp>
- <https://developer.android.com/build/jdks>

## Conséquences

L'analyse reste sûre, déterministe et testable sans dépendance Go externe. Elle
ne résout pas tout le langage Gradle et peut donc produire davantage
d'inconnues qu'une exécution réelle. Ce choix est intentionnel : étendre une
règle demandera un exemple réel, une preuve non ambiguë et un test de
non-régression.
