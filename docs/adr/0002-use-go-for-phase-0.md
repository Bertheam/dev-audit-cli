# ADR 0002 — Utiliser Go pour le moteur et la CLI

- Statut : accepté
- Date : 2026-09-02

## Contexte

Le dépôt était vide de code. Le moteur doit fournir un parcours déterministe,
des frontières système injectables, des sous-processus bornés, du JSON stable et
un binaire macOS simple à distribuer.

## Décision

Utiliser Go 1.27 pour la Phase 0, avec la bibliothèque standard par défaut et un
minimum de dépendances externes.

## Raisons

- `filepath.WalkDir` est adapté au parcours déterministe sans suivi automatique
  des symlinks ;
- `os/exec.CommandContext` lance sans shell et permet les délais d'expiration ;
- `encoding/json` et `testing` couvrent le contrat initial ;
- le moteur reste indépendant d'une future interface utilisateur.

## Conséquences

Go doit être installé sur le poste de développement. Les utilisateurs du futur
binaire n'auront pas besoin de la toolchain. Le chemin du module est local tant
qu'aucun dépôt distant n'est défini ; il sera remplacé avant publication.
