# Dev Environment Auditor

> Statut : Lots 0 à 5 terminés ; validation terrain non encore réalisée
> Plateforme : macOS
> Périmètre : Flutter et Android
> Interface cible : CLI `dev-audit`

Dev Environment Auditor doit établir une carte vérifiable entre les projets
Flutter/Android et les toolchains locales qu'ils exigent. La Phase 0 privilégie
la preuve, la traçabilité et l'expression honnête de l'incertitude.

Le projet n'est pas un nettoyeur. Il ne contient aucune fonction de suppression,
d'installation, de réparation, de build, de télémétrie ou de synchronisation.

## Objectif de la Phase 0

La CLI devra :

- analyser uniquement les racines fournies explicitement ;
- découvrir les projets Flutter et Android sans suivre les symlinks externes ;
- extraire les exigences Flutter/FVM, Dart, Gradle, AGP, JDK, SDK et NDK ;
- inventorier les installations pertinentes et estimer leur taille ;
- relier projets, exigences et ressources installées ;
- rattacher chaque conclusion à des preuves et à un niveau de confiance ;
- produire un rapport terminal et un export JSON v1 déterministes.

## État actuel

Le Lot 0 contient la structure du projet, le modèle de domaine, le contrat JSON,
les décisions d'architecture, le backlog et des fixtures initiales.

Le Lot 1 ajoute un moteur interne de découverte capable de valider les racines,
d'appliquer des exclusions, de détecter les projets Flutter/Android, de replier
le sous-projet `android` d'un projet Flutter et d'arrêter proprement un parcours
qui dépasse ses budgets. Aucun symlink n'est suivi.

Le Lot 2 ajoute des analyseurs statiques bornés pour FVM, le pubspec Dart,
Gradle Wrapper, AGP/Kotlin, les catalogues de versions utilisés, les niveaux SDK,
NDK/CMake et les toolchains Java/Kotlin. Une expression non résolue reste
`PROBABLY_REQUIRED` ou `UNKNOWN` sans version inventée. Chaque résultat indique
son fichier, sa clé, sa ligne et la règle appliquée.

Le Lot 3 inventorie, depuis des racines explicitement typées, les plateformes et
Build Tools Android, NDK/CMake, SDK Flutter/FVM, caches Gradle/AGP/Kotlin et JDK.
La version Dart embarquée est conservée avec le SDK Flutter. Chaque ressource
reçoit un chemin, une version statique lorsque disponible et une taille logique
bornée. Une mesure interrompue ou incomplète ne publie aucune taille partielle.

Le Lot 4 relie les exigences explicites aux ressources inventoriées. Il prend en
charge les versions exactes, les plages Dart stables usuelles, les canaux
Flutter documentés et le niveau majeur d'un JDK. `MISSING` exige une couverture
d'inventaire complète du type concerné ; `NO_REFERENCE_FOUND` exige une
découverte et une analyse complètes. Toute ambiguïté, métadonnée absente ou
syntaxe non prise en charge reste `UNKNOWN` ou `AMBIGUOUS`.

Le Lot 5 assemble le pipeline derrière `dev-audit scan`, produit un rapport
terminal ou JSON v1 validé par le schéma embarqué, et permet d'expliquer un
projet, une exigence ou une ressource depuis un rapport JSON. Les diagnostics
partiels restent dans le rapport et déterminent le code de sortie sans supprimer
les observations valides.

Go 1.27.1 est installé via Homebrew sur le poste de développement. Les tests,
les tests avec détecteur de concurrence, `go vet` et la commande
`dev-audit version` passent.

La vision plus large d'origine est conservée dans
[`docs/vision.md`](docs/vision.md). Docker, le nettoyage, la GUI et le cloud
restent explicitement hors Phase 0.

## Installer Go sur macOS avec Homebrew

Le projet cible Go 1.27. La méthode retenue sur ce Mac Apple Silicon est
Homebrew :

```bash
brew install go
```

Vérifier ensuite :

```bash
go version
go env GOPATH GOMODCACHE
```

L'installeur ARM64 de <https://go.dev/dl/> reste une alternative si Homebrew
n'est pas utilisé. Le binaire final ne demandera pas à l'utilisateur d'installer
Go.

## Vérifications locales

Après installation de Go :

```bash
go test ./...
go test -race -cover ./...
go vet ./...
go run ./cmd/dev-audit version
```

Résultat attendu à la fin du Lot 5 : `0.0.0-lot5`.

Validation structurelle du JSON sans Go :

```bash
python3 -m json.tool examples/scan-v1.minimal.json
```

## Utiliser la CLI

Rapport terminal avec une racine de projets explicite :

```bash
go run ./cmd/dev-audit scan \
  --root /Users/alice/Projects \
  --timeout 30s
```

Ajouter uniquement les racines d'inventaire que l'on souhaite auditer :

```bash
go run ./cmd/dev-audit scan \
  --root /Users/alice/Projects \
  --android-sdk-root /Users/alice/Library/Android/sdk \
  --flutter-sdk-root /Users/alice/Developer/flutter \
  --fvm-cache-root /Users/alice/fvm/versions \
  --gradle-user-home /Users/alice/.gradle \
  --jdk-root /Library/Java/JavaVirtualMachines
```

Les options de racine et `--exclude` sont répétables. Aucune racine
d'inventaire n'est devinée depuis le dossier personnel ou l'environnement. Sans
racine d'inventaire, le scan reste valide, mais les conclusions `MISSING` sont
désactivées.

Créer un rapport JSON local puis expliquer un identifiant affiché :

```bash
go run ./cmd/dev-audit scan \
  --root /Users/alice/Projects \
  --format json \
  --output audit.json

go run ./cmd/dev-audit explain --report audit.json resource-0123456789abcdef
```

Un fichier de sortie est créé avec les permissions `0600`. La CLI refuse une
sortie qui est un symlink et `explain` refuse d'écraser son rapport source.

Codes de sortie :

- `0` : commande terminée sans diagnostic `ERROR` ;
- `1` : rapport partiel avec diagnostic `ERROR`, ou identifiant absent ;
- `2` : arguments invalides ;
- `3` : erreur de lecture, validation, rendu ou écriture.

## Documentation

- [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) : approche, choix de Go et lots.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) : modules, flux et frontières techniques.
- [`BACKLOG.md`](BACKLOG.md) : lots, résultats attendus et gates.
- [`docs/assumptions.md`](docs/assumptions.md) : hypothèses et décisions ouvertes.
- [`docs/risks.md`](docs/risks.md) : registre initial des risques.
- [`docs/validation-plan.md`](docs/validation-plan.md) : protocole terrain.
- [`docs/adr/`](docs/adr/) : décisions structurantes.
- [`schemas/scan-v1.schema.json`](schemas/scan-v1.schema.json) : contrat JSON v1.

## Règle de sécurité centrale

Une absence de référence signifie uniquement qu'aucune référence n'a été trouvée
dans le périmètre effectivement analysé. Elle ne signifie jamais qu'une ressource
est inutilisée ou sûre à supprimer.
