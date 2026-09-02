# ADR 0004 — Versionner le contrat JSON dès le Lot 0

- Statut : accepté
- Date : 2026-09-02

## Contexte

Le rapport JSON servira aux comparaisons, aux tests terrain et à une éventuelle
interface future. Les termes de confiance ne doivent pas être surchargés.

## Décision

Le rapport expose `schema_version: "1.0"`, `read_only: true` et des collections
triées. Le contrat sépare :

- confiance d'une exigence ;
- statut de référence d'une ressource ;
- statut de correspondance d'une relation ;
- diagnostics partiels.

Le schéma se trouve dans `schemas/scan-v1.schema.json`.

## Conséquences

Tout changement incompatible exigera une nouvelle version de schéma. Les ajouts
compatibles resteront optionnels jusqu'à la version suivante.
