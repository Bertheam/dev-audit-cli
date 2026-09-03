# Hypothèses et décisions ouvertes

## Hypothèses actives

- La Phase 0 cible uniquement macOS sur architecture Apple Silicon ou Intel.
- Les utilisateurs non experts ne connaissent pas nécessairement leurs racines
  de projets, SDK, caches ou JDK ; la CLI doit les rechercher localement.
- L'analyse statique suffit pour prouver les versions déclarées littéralement.
- Une configuration dynamique peut rester `PROBABLY_REQUIRED` ou `UNKNOWN`.
- L'inventaire peut lire les emplacements Android/Flutter/JDK autorisés sans les
  modifier ni exécuter leurs gestionnaires.
- Le rapport par défaut est écrit sur stdout.
- Un chemin absolu peut être affiché localement, mais ne doit jamais être envoyé
  vers un service distant.

## Décisions ouvertes avant publication

1. URL et propriétaire du dépôt distant, nécessaires pour fixer le chemin public
   du module Go.
2. Licence du projet.
3. Politique d'identifiants stables et option de pseudonymisation des chemins.
4. Durée maximale imposée par la future CLI à chaque adaptateur.
5. Sous-ensemble de syntaxe Gradle officiellement pris en charge.
6. Politique de compatibilité macOS du binaire distribué.
7. Étendue future de la détection Docker au-delà des images JDK reconnues.

## Clarifications appliquées

- `NO_REFERENCE_FOUND` qualifie la couverture d'une ressource et non une preuve
  d'inutilité.
- `MISSING` exige une couverture d'inventaire complète déclarée pour le type de
  ressource normalisé ; une racine omise vaut couverture incomplète.
- Les versions Dart avec préversion ou métadonnée de build restent hors du
  sous-ensemble de corrélation initial et produisent `UNKNOWN`.
- La CLI détecte par défaut les racines omises depuis l'environnement, le
  `PATH`, les emplacements usuels et une recherche profonde bornée.
- Une couverture automatique reste heuristique : elle autorise les
  correspondances positives, jamais `MISSING` ni `NO_REFERENCE_FOUND`.
- Les relations portent `HOST` ou `DOCKER`. Un champ absent dans un ancien
  rapport JSON v1 signifie `HOST`.
- `DOCKER:MATCHED` décrit un `FROM` statique compatible ; il ne prouve ni une
  image locale ni un conteneur actif et n'est jamais une ressource installée.
- Un diagnostic `WARNING` ou `ERROR` rend incomplète la couverture de l'étape ou
  de la famille d'inventaire concernée ; cette règle privilégie les faux
  inconnus aux fausses certitudes.
- Une sortie vers `--output` est une écriture explicitement autorisée ; elle ne
  modifie pas l'environnement audité.
- `size_bytes` représente uniquement la taille logique complète des fichiers
  réguliers ; la taille allouée APFS est différée.
- « Go » et « Rust » dans les exclusions du document désignent les écosystèmes à
  auditer, pas nécessairement le langage d'implémentation.
