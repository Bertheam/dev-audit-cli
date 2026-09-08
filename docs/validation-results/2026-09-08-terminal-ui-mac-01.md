# Validation Mac 01 — expérience terminal human-first

- Date : 2026-09-08
- Plateforme : macOS Apple Silicon
- Version : `0.3.0-dev`
- Périmètre : Lot 14, présentation terminal uniquement

## Commandes de validation

```bash
go test ./...
go test -race ./...
go vet ./...

dev-audit help
dev-audit scan --color never
dev-audit explain --report internal/report/testdata/scan.golden.json --color never resource-one
dev-audit plan --report internal/report/testdata/scan.golden.json --color never
dev-audit plan --report internal/report/testdata/scan.golden.json --color always
./scripts/install.sh /opt/homebrew/bin
```

## Résultats

- tous les tests, tests de courses et contrôles `go vet` réussissent ;
- le scan réel termine en 15 secondes avec 28 projets, 201 exigences, 86
  ressources et 87 diagnostics informatifs ;
- la vue compacte affiche 12 projets et les 12 plus grosses ressources, puis
  annonce respectivement 16 et 74 entrées masquées ;
- les 87 diagnostics `INFO` sont résumés, sans masquer de `WARNING` ou `ERROR` ;
- `explain` sépare correctement détails, classifications, relations, preuves,
  métadonnées et avertissements ;
- `plan` affiche immédiatement `SIMULATION ONLY` et `NOT EXECUTED` ;
- l'environnement de validation contient `NO_COLOR=1` et `TERM=dumb` : le mode
  `auto` reste donc sans ANSI comme attendu ;
- `--color always` produit les séquences ANSI et `--color never` les supprime ;
- `--format json --color always` reste un JSON valide sans séquence ANSI ;
- le binaire installé répond avec la nouvelle aide et la version attendue.

## Sécurité

Le test réel exécute uniquement les opérations de lecture du scan et des
rapports. Le plan reste une simulation ; aucune commande de nettoyage n'est
exécutée et aucune ressource de développement n'est modifiée.
