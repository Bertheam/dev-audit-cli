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
- Un AVD peut contenir des données applicatives mutables ; sa présence, sa
  taille ou son ancienneté apparente ne suffisent jamais à autoriser son
  nettoyage.

## Décisions ouvertes avant publication

1. URL et propriétaire du dépôt distant, nécessaires pour fixer le chemin public
   du module Go.
2. Licence du projet.
3. Politique d'identifiants stables et option de pseudonymisation des chemins.
4. Durée maximale imposée par la future CLI à chaque adaptateur.
5. Sous-ensemble de syntaxe Gradle officiellement pris en charge.
6. Politique de compatibilité macOS du binaire distribué.

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
- Les relations portent `HOST`, `DOCKER` ou `DOCKER_DAEMON`. Un champ absent
  dans un ancien rapport JSON v1 signifie `HOST`.
- `DOCKER:MATCHED` décrit un `FROM` statique compatible ; il ne prouve ni une
  image locale ni un conteneur actif et n'est jamais une ressource installée.
- Les quatre noms Compose standards sont reconnus. Seules les valeurs scalaires
  statiques de `services.*.image` et les images externes statiques de `FROM`
  deviennent des exigences. Les alias de stage et `scratch` sont ignorés.
- `DOCKER_DAEMON` relie une exigence d'image à l'inventaire local global. Une
  absence ne devient `MISSING` que si la liste d'images a été lue complètement.
- Un diagnostic `WARNING` ou `ERROR` rend incomplète la couverture de l'étape ou
  de la famille d'inventaire concernée ; cette règle privilégie les faux
  inconnus aux fausses certitudes.
- Une sortie vers `--output` est une écriture explicitement autorisée ; elle ne
  modifie pas l'environnement audité.
- `size_bytes` représente uniquement la taille logique complète des fichiers
  réguliers ; la taille allouée APFS est différée.
- Les images système Android sont reconstructibles via leur identifiant de
  paquet, mais les AVD restent sensibles et `UNKNOWN` tant qu'une preuve fiable
  d'activité et d'usage n'existe pas.
- Le signal BuildKit `Reclaimable` et la taille virtuelle d'une image sont des
  observations Docker ; ils ne constituent ni une taille uniquement
  récupérable ni une autorisation de nettoyage.
- « Go » et « Rust » dans les exclusions du document désignent les écosystèmes à
  auditer, pas nécessairement le langage d'implémentation.
