# Architecture — Dev Environment Auditor

## Contexte

La Phase 0 est une CLI macOS locale, limitée à Flutter et Android. Elle produit
des observations vérifiables sans modifier les projets ni les toolchains.

## Flux principal

```text
Arguments CLI
  -> auto-détection ou validation des racines et exclusions
  -> discovery
  -> analyzers
  -> environment analyzers (Dockerfile statique)
  -> inventory adapters
  -> correlation
  -> evidence/confidence
  -> terminal ou JSON v1
```

Chaque étape retourne ses résultats et ses diagnostics partiels. Une erreur sur
un projet ne doit pas invalider les résultats démontrables des autres projets.

## Modules

### `cmd/dev-audit`

Parse les arguments, construit les adaptateurs et sélectionne un reporter. Il ne
contient aucune règle métier.

`scan` peut fonctionner sans chemin fourni. La CLI demande alors au détecteur
local des racines de projets et d'inventaire, puis transmet au service
applicatif la provenance heuristique de leur couverture. Les racines typées
restent répétables et permettent un mode entièrement explicite. Un contexte
borne la détection et le pipeline. `explain` relit un rapport JSON v1 validé et
affiche les preuves d'un identifiant. La CLI est la seule couche autorisée à
écrire un rapport explicitement demandé.

### `scripts` et `packaging`

`install.sh` construit depuis les sources et requiert Go. Le script mainteneur
`build-release.sh` effectue une cross-compilation sans CGO pour `darwin/arm64` et
`darwin/amd64`, injecte la version, crée les archives et calcule leurs sommes
SHA-256. Chaque archive reçoit `install-binary.sh` sous le nom `install.sh` ainsi
que les instructions de `packaging/INSTALL.md`.

L'installateur d'archive n'utilise pas Go : il copie uniquement le binaire déjà
compilé dans un dossier du `PATH`. La signature Developer ID et la notarisation
restent un gate obligatoire avant une diffusion publique.

### `internal/autodetect`

Détecte sans sous-processus les projets, Android SDK, Flutter/FVM, Gradle User
Home et JDK. La première passe utilise les variables connues, le `PATH` et les
emplacements conventionnels. Une seconde passe optionnelle parcourt des dossiers
de développement bornés à 250 000 entrées par emplacement, 750 000 au total et
une profondeur de 10.

Les signatures exigent des marqueurs structurels : un SDK Android doit présenter
au moins deux répertoires caractéristiques ; Flutter exige `bin/flutter` et
`packages/flutter` ; un JDK exige `release`, `bin/java` et `bin/javac`. Le
parcours profond ne suit pas les symlinks ; les chemins d'exécutables du
`PATH` sont résolus puis validés par leurs marqueurs. Les caches de build,
environnements virtuels et `testdata` sont ignorés. Les chemins trouvés et leur
source sont exposés par diagnostics.

### `internal/application`

Orchestre Discovery, analyse statique, inventaires séparés par famille et
corrélation. La couverture des projets et analyseurs est complète uniquement en
l'absence de diagnostic `WARNING` ou `ERROR`. Chaque famille d'inventaire ne
déclare ses types complets que si son propre passage respecte la même règle.
Une erreur partielle conserve les résultats des autres étapes.

Une racine de projets auto-détectée rend volontairement incomplète la couverture
de découverte pour la corrélation : aucun `NO_REFERENCE_FOUND` n'en découle. De
même, une famille d'inventaire automatique n'autorise jamais `MISSING`. Une
correspondance positive exacte reste possible dans les deux cas. La corrélation
des ressources installées produit uniquement des relations `HOST` ; les
relations `DOCKER` proviennent de l'analyse déclarative séparée.

### `internal/domain`

Contient les types `Scan`, `Project`, `Requirement`, `InstalledResource`,
`Relation`, `Evidence` et `Diagnostic`, ainsi que leurs invariants. Une relation
porte l'environnement `HOST` ou `DOCKER`. Pour compatibilité avec les premiers
rapports JSON v1, l'absence du champ `environment` se lit comme `HOST`.

### `internal/discovery`

Parcourt uniquement les racines autorisées. Il applique des exclusions, ne suit
pas les symlinks et détecte les frontières de projets sans interpréter leurs
versions. Les liens ignorés produisent un résumé par racine, pas un diagnostic
par entrée.

Le Lot 1 utilise une interface de système de fichiers limitée à `Lstat` et
`WalkDir`. Les valeurs par défaut bornent chaque racine à 250 000 entrées et une
profondeur de 64. Les collections et diagnostics sont triés avant retour.

La reconnaissance reste volontairement conservatrice :

- Flutter : `pubspec.yaml` accompagné de `.metadata`, `.fvmrc` ou d'un dossier
  `android` ;
- Android : `settings.gradle(.kts)` accompagné d'un Gradle Wrapper ou d'un
  `AndroidManifest.xml` sous un module ;
- `<projet-flutter>/android` est replié dans un projet `FLUTTER_ANDROID` plutôt
  que signalé deux fois.

Ces marqueurs établissent uniquement une frontière de projet. Leur contenu et
les versions déclarées relèvent du Lot 2.

### `internal/analyzers`

Lit un sous-ensemble documenté des fichiers Flutter et Android. Chaque règle
d'analyse possède un identifiant stable et produit des preuves. Une expression
dynamique non résolue reste une inconnue.

Le Lot 2 limite chaque projet à 50 000 entrées, 256 fichiers, une profondeur de
12 et 1 Mio par fichier. Les seules entrées sont `.fvmrc`, l'ancien fichier FVM,
le pubspec, les scripts Gradle, le Wrapper et le catalogue standard. Les fichiers
sont ouverts via une interface qui n'expose aucune écriture et les symlinks sont
refusés.

Les parseurs couvrent les formes statiques usuelles sur une ligne. Ils
n'interprètent pas Groovy ou Kotlin et ne résolvent pas une propriété arbitraire.
Un alias de catalogue n'est retenu que lorsqu'un script l'utilise. Les preuves
dynamiques sont minimisées pour éviter de recopier une variable d'environnement,
une URL ou une expression potentiellement sensible.

### `internal/environments`

Analyse statiquement les environnements déclaratifs séparés de l'hôte. Le
premier adaptateur parcourt les `Dockerfile` sous chaque projet avec des budgets
de 50 000 entrées, 32 fichiers, une profondeur de 6 et 1 Mio par fichier. Il ne
lance ni Docker ni une image.

Les images Temurin, OpenJDK, Corretto, Gradle et Maven permettent actuellement
d'extraire un niveau de fonctionnalité JDK. Une image `jre` n'est jamais traitée
comme un JDK ; une variable d'image ou une version non statique reste inconnue.
Une relation `DOCKER:MATCHED` prouve uniquement qu'un `FROM` déclaratif satisfait
l'exigence. Elle n'a aucun `resource_id` et n'entre pas dans l'inventaire HOST.
Le moteur ne produit pas encore de conclusion `DOCKER:MISSING`.
Les symlinks restent ignorés silencieusement par cet adaptateur, car Discovery
les a déjà comptés pour la même racine et possède le diagnostic de synthèse.

### `internal/inventory`

Recense les installations dans des emplacements transmis par la CLI, qu'ils
soient explicites ou détectés. La mesure de taille possède des limites de temps,
de profondeur et de nombre d'entrées.

Le moteur d'inventaire exige toujours des racines typées ; leur déduction est la
responsabilité de `internal/autodetect` et de la CLI. Il reconnaît les
paquets Android structurés, les SDK Flutter directs ou sous FVM, les
distributions Wrapper et plugins AGP/Kotlin du cache Gradle, et les JDK via leurs
métadonnées statiques. Aucun gestionnaire ni exécutable inventorié n'est lancé.

La métrique `size_bytes` est la somme logique des fichiers réguliers. Les
symlinks ne sont pas suivis. Dès qu'une permission, une limite ou l'annulation
rend le parcours incomplet, la taille est absente et un diagnostic explique la
couverture manquante. La taille allouée APFS n'est pas estimée dans cette phase.

### `internal/correlation`

Normalise les versions et relie exigences et ressources. Il ne transforme jamais
une absence de correspondance en recommandation de suppression.

Le Lot 4 reçoit un contrat de couverture explicite. `MISSING` n'est produit que
pour une exigence statique explicite lorsque le type de ressource normalisé a
été entièrement inventorié. `NO_REFERENCE_FOUND` n'est produit que si la
découverte des projets et leur analyse sont toutes deux complètes. Une exigence
probable, une contrainte non prise en charge, une version installée absente ou
une correspondance multiple protège les candidats concernés en `UNKNOWN`.

Les versions Android, Flutter, Gradle et plugins sont associées exactement ; un
canal Flutter peut aussi correspondre à la métadonnée `channel`. Un JDK est
comparé par niveau de fonctionnalité (`17` avec `17.0.12`, ou `8` avec
`1.8.0_442`). Les contraintes Dart stables acceptées sont `any`, une version
exacte, les comparateurs intersectés et la notation caret. Elles sont évaluées
sur `dart_sdk_version` du SDK Flutter inventorié.

### `internal/evidence`

Centralise les preuves, les règles de confiance, la suppression des valeurs
sensibles et les diagnostics.

### `internal/report`

Trie les collections puis produit une représentation terminal ou JSON. Le JSON
est validé contre `schemas/scan-v1.schema.json`.

Le schéma JSON 2020-12 est embarqué dans le binaire et compilé localement avec
les assertions de format activées. Le terminal échappe les caractères de
contrôle et rappelle systématiquement que `NO_REFERENCE_FOUND` n'autorise pas
une suppression. Les rapports d'entrée de `explain` sont limités à 64 Mio,
refusent les champs inconnus et les valeurs JSON supplémentaires.

### `internal/platform`

Expose les frontières injectables : système de fichiers, horloge, environnement
et exécution optionnelle de commandes locales non destructives.

## Direction des dépendances

```text
CLI/reporters/adapters -> services applicatifs -> domain
```

Le domaine n'importe aucun module de plateforme. Les analyseurs reçoivent un
projet déjà découvert et ne construisent que des chemins allowlistés sous sa
racine.

## Frontière de lecture seule

« Lecture seule » signifie qu'aucun projet, cache, SDK ou fichier de configuration
n'est modifié. Deux écritures explicites restent permises :

1. la sortie standard du terminal ;
2. un rapport vers le chemin fourni par `--output`.

Les fichiers temporaires persistants, journaux implicites et caches applicatifs
sont interdits en Phase 0.

Un rapport explicitement demandé est écrit en `0600`. Les symlinks et fichiers
non réguliers sont refusés comme destinations ; `explain` ne peut pas remplacer
le rapport qu'il est en train de lire.

## Déterminisme

- parcours et résultats triés lexicalement ;
- identifiants dérivés d'une forme canonique documentée ;
- horloge injectée dans les tests ;
- JSON fondé sur des structures typées, pas sur des maps libres ;
- diagnostics ordonnés par portée, code puis chemin.

## Sous-processus

L'analyse statique est prioritaire. Une commande locale éventuelle doit être
allowlistée, lancée sans shell, bornée par un contexte et accompagnée d'une
limite de sortie. Aucun build ou téléchargement implicite n'est autorisé.
