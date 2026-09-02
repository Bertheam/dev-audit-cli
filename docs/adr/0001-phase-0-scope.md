# ADR 0001 — Limiter la Phase 0 à l'audit Flutter/Android

- Statut : accepté
- Date : 2026-09-02

## Contexte

La vision initiale inclut Docker, une interface macOS et l'exécution contrôlée de
nettoyages. La question produit prioritaire est toutefois de vérifier si une
carte projet–toolchain apporte davantage de confiance qu'un inventaire de disque.

## Décision

La Phase 0 couvre uniquement macOS, Flutter, Android, une CLI locale, l'analyse
statique, l'inventaire, la corrélation, les preuves et les rapports terminal/JSON.

Docker, Xcode, la GUI, le cloud, la télémétrie et toute action destructive sont
hors périmètre.

## Conséquences

Le moteur peut être validé rapidement. La vision plus large est conservée dans
`docs/vision.md`, mais ne constitue pas un backlog actif.
