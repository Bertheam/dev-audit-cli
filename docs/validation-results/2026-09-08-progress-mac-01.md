# Validation Mac 01 — progression des commandes

- Date : 2026-09-08
- Plateforme : macOS Apple Silicon
- Version : `0.3.0-dev`
- Périmètre : Lot 15

## Commande réelle

```bash
dev-audit scan --progress auto --color never
```

Le terminal de validation a été exposé comme TTY avec un `TERM` fonctionnel
afin de tester la branche interactive. La couleur a été coupée pour isoler le
comportement du spinner.

## Résultats

- la progression commence après 100 ms et reste sur une seule ligne ;
- le temps écoulé passe de `100ms` à `16.3s` ;
- les étapes observées incluent l'auto-détection, la découverte, l'analyse de
  28 projets, les déclarations Docker/Compose, les inventaires Android,
  Flutter/FVM, Gradle et Xcode, puis le daemon Docker ;
- la dernière ligne affiche `✓ Scan complete · 16.3s` ;
- le spinner est arrêté et sa ligne effacée avant le rendu du rapport ;
- le rapport final conserve 28 projets, 201 exigences, 86 ressources et 87
  diagnostics, comme avant l'ajout de la progression.

## Modes non interactifs

Les tests automatisés vérifient aussi que :

- `--progress always` écrit des lignes ordinaires sur `stderr` sans ANSI ;
- `--progress never` n'écrit rien ;
- `scan --format json --progress always` produit toujours un JSON valide et
  sans texte de progression sur `stdout` ;
- `explain` et `plan` utilisent la même infrastructure sans contaminer leur
  sortie principale.

## Sécurité

Le callback de progression est informatif et n'a aucun accès en écriture aux
projets ou ressources. Le scan reste en lecture seule et aucun plan n'est
exécuté.
