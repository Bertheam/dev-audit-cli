# ADR 0005 — Découverte contrôlée des projets

- Statut : accepté
- Date : 2026-09-02

## Contexte

La découverte doit trouver des projets Flutter et Android dans des racines
choisies par l'utilisateur, sans parcourir aveuglément la machine ni sortir du
périmètre autorisé.

## Décision

- exiger au moins une racine explicite ;
- convertir les racines en chemins absolus nettoyés ;
- éliminer les doublons et racines déjà couvertes ;
- rejeter une racine qui est elle-même un symlink ;
- ne suivre aucun symlink rencontré pendant le parcours ;
- appliquer des exclusions par nom, chemin, glob ou préfixe `/**` ;
- exclure par défaut les caches et sorties connus comme `.git`, `.gradle`,
  `.dart_tool`, `.fvm`, `build`, `Pods` et `node_modules` ;
- limiter par défaut chaque racine à 250 000 entrées et une profondeur de 64 ;
- convertir permissions refusées, disparitions et limites atteintes en
  diagnostics sans faire échouer les autres racines.

Un projet Flutter exige `pubspec.yaml` et un marqueur Flutter complémentaire.
Un projet Android exige des settings Gradle et un wrapper ou un manifeste. Le
dossier `android` d'un projet Flutter est absorbé dans le projet parent.

## Conséquences

La découverte est déterministe, testable par injection d'un système de fichiers
et strictement en lecture seule. Certaines structures non conventionnelles
resteront non détectées ; elles seront ajoutées seulement à partir de cas réels.
