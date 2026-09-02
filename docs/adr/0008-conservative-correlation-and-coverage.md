# ADR 0008 — Corrélation conservatrice et couverture explicite

- Statut : accepté
- Date : 2026-09-02

## Contexte

Une version déclarée par un projet et une version trouvée dans un cache ne
suffisent pas toujours à prouver une association unique. Plusieurs installations
peuvent satisfaire la même contrainte, une ressource peut manquer de métadonnées
et un parcours peut être incomplet. Confondre « non observé » avec « absent » ou
« inutilisé » créerait précisément le risque que la Phase 0 doit éviter.

## Décision

La corrélation est une fonction pure : elle reçoit projets, ressources et état
de couverture, puis retourne des copies ordonnées, des relations et ses propres
diagnostics. Elle ne lit pas le système de fichiers, ne lance aucune commande et
ne modifie pas ses entrées.

La couverture possède deux axes indépendants :

- `ProjectDiscoveryComplete` et `RequirementAnalysisComplete` doivent être vrais
  pour attribuer `NO_REFERENCE_FOUND` à une ressource corrélable ;
- `CompleteInventoryScopes` énumère les couples normalisés
  écosystème/composant dont l'inventaire est complet et autorise `MISSING` pour
  une exigence explicite correspondante.

Une portée absente est incomplète. L'absence d'un candidat sans cette preuve
produit `UNKNOWN`. Une ressource possible reste également `UNKNOWN` lorsqu'une
exigence est probable, sa contrainte n'est pas comprise ou sa version manque.
Plusieurs candidats possibles produisent `AMBIGUOUS` sans choisir arbitrairement
un chemin.

Les correspondances de Phase 0 sont :

| Exigence | Ressource | Comparaison |
|---|---|---|
| Android `compile_sdk` | `android_sdk_platform` | exacte |
| Android NDK, CMake et AGP | composant homonyme | exacte |
| Flutter SDK | `flutter_sdk` | version stable exacte ou canal connu |
| Dart SDK | `flutter_sdk.dart_sdk_version` | contrainte Dart stable |
| Gradle et Kotlin Gradle Plugin | composant homonyme | exacte |
| JDK | `jdk` | niveau de fonctionnalité Java |

`target_sdk` et `min_sdk` décrivent la compatibilité applicative, pas une
installation locale distincte. Android Build Tools est inventorié mais aucune
exigence correspondante n'est encore extraite. Ces éléments ne reçoivent donc
aucune association implicite.

Le sous-ensemble de contraintes Dart suit les formes documentées par pub :
`any`, version stable exacte, intersections de comparateurs et notation caret.
Les préversions et métadonnées de build restent hors périmètre initial ; elles
sont signalées comme non prises en charge au lieu d'être approximées.

## Conséquences

Les résultats sont moins affirmatifs, mais auditables : `MISSING` prouve une
absence dans un inventaire déclaré complet, tandis que `NO_REFERENCE_FOUND`
signifie seulement qu'aucune exigence couverte ne vise la ressource. Aucun de ces
états n'est une recommandation de suppression.

Une future extension pourra élargir le parseur Dart ou résoudre des contraintes
entre plusieurs exigences du même projet. Elle devra ajouter des fixtures et ne
pourra pas diminuer les exigences de couverture.

## Références

- contraintes de dépendances Dart : <https://dart.dev/tools/pub/dependencies>
- politique de versions de pub : <https://dart.dev/tools/pub/versioning>
- implémentation de référence : <https://pub.dev/packages/pub_semver>
