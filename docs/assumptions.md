# Hypothèses et décisions ouvertes

## Hypothèses actives

- La Phase 0 cible uniquement macOS sur architecture Apple Silicon ou Intel.
- Les racines de projets sont toujours fournies explicitement.
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
4. Liste exacte des suggestions de racines d'inventaire automatiques et manière de les afficher
   avant le scan.
5. Durée maximale imposée par la future CLI à chaque adaptateur.
6. Sous-ensemble de syntaxe Gradle officiellement pris en charge.
7. Politique de compatibilité macOS du binaire distribué.

## Clarifications appliquées

- `NO_REFERENCE_FOUND` qualifie la couverture d'une ressource et non une preuve
  d'inutilité.
- Une sortie vers `--output` est une écriture explicitement autorisée ; elle ne
  modifie pas l'environnement audité.
- `size_bytes` représente uniquement la taille logique complète des fichiers
  réguliers ; la taille allouée APFS est différée.
- « Go » et « Rust » dans les exclusions du document désignent les écosystèmes à
  auditer, pas nécessairement le langage d'implémentation.
