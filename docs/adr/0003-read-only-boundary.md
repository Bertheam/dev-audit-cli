# ADR 0003 — Définir la frontière de lecture seule

- Statut : accepté
- Date : 2026-09-02

## Contexte

Le produit doit exporter un rapport tout en garantissant qu'il ne modifie ni les
projets ni les installations auditées.

## Décision

Le moteur ne possède aucune capacité de suppression ou de modification. Les
seules sorties autorisées sont stdout/stderr et, sur demande explicite, le chemin
fourni par `--output`.

Les écritures temporaires persistantes, caches implicites, journaux permanents,
builds, téléchargements et réparations sont interdits.

## Vérification

- interfaces séparées de lecture et de sortie de rapport ;
- recherche statique des primitives d'écriture/destruction ;
- tests avant/après sur le contenu et les mtimes des fixtures ;
- commandes externes derrière une allowlist et sans shell.
