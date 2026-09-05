# Architecture — Dev Environment Auditor

## Contexte

La Phase 0 est une CLI macOS locale pour Flutter, Android, Docker et les
ressources Xcode/iOS. Elle produit des observations vérifiables sans modifier
les projets ni les toolchains.

## Flux principal

```text
Arguments CLI
  -> auto-détection ou validation des racines et exclusions
  -> discovery
  -> analyzers
  -> environment analyzers (Dockerfile et Compose statiques)
  -> inventory adapters (hôte et daemon Docker)
  -> correlation
  -> classification explicable
  -> terminal ou JSON v1

Rapport JSON v1 validé
  -> planning (sélection automatique conservatrice ou IDs explicites)
  -> plan SIMULATION_ONLY lié au SHA-256 source
  -> terminal ou cleanup-plan JSON v1
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
écrire un rapport explicitement demandé. `plan` relit le même contrat, calcule
son empreinte et produit un artefact simulé ; une sortie fichier utilise une
création exclusive afin de ne jamais remplacer un plan déjà revu.

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
Home, JDK, applications Xcode et racines Apple Developer. La première passe
utilise les variables connues, le `PATH` et les emplacements conventionnels.
Pour Apple, elle normalise `DEVELOPER_DIR`, inspecte les applications
`Xcode*.app` et valide `~/Library/Developer` et `/Library/Developer`. Une seconde
passe optionnelle parcourt des dossiers
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

Orchestre Discovery, analyse statique, inventaires séparés par famille,
corrélation et classification. La couverture des projets et analyseurs est
complète uniquement en l'absence de diagnostic `WARNING` ou `ERROR`. Chaque
famille d'inventaire ne déclare ses types complets que si son propre passage
respecte la même règle.
Une erreur partielle conserve les résultats des autres étapes.

Une racine de projets auto-détectée rend volontairement incomplète la couverture
de découverte pour la corrélation : aucun `NO_REFERENCE_FOUND` n'en découle. De
même, une famille d'inventaire automatique n'autorise jamais `MISSING`. Une
correspondance positive exacte reste possible dans les deux cas. La corrélation
des ressources hôte produit des relations `HOST`; l'analyse déclarative des
toolchains produit `DOCKER`; la présence d'une image déclarée dans le daemon
produit `DOCKER_DAEMON`.

### `internal/domain`

Contient les types `Scan`, `Project`, `Requirement`, `InstalledResource`,
`ResourceClassification`, `SpaceEstimate`, `Relation`, `Evidence` et
`Diagnostic`, ainsi que leurs invariants. Une relation
porte l'environnement `HOST`, `DOCKER` ou `DOCKER_DAEMON`. Pour compatibilité
avec les premiers rapports JSON v1, l'absence du champ `environment` se lit
comme `HOST`. Les champs additifs de classification restent optionnels à la
lecture pour conserver la compatibilité avec les rapports v1 antérieurs.

Le domaine contient aussi le contrat indépendant `CleanupPlan`. Ses invariants
imposent `SIMULATION_ONLY`, une confirmation explicite par élément, la cohérence
du résumé et un `plan_id` SHA-256 recalculé sur tout le contenu hors identifiant.
Une sélection `DEFAULT_SAFE` ne peut contenir ni `SENSIBLE`, ni `INCONNUE`, ni
`UTILISEE` et exige `ORPHELINE_PROBABLE` avec une estimation conservatrice.

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
- Docker : `Dockerfile`, `Dockerfile.*` ou un des quatre noms Compose standards ;
- `<projet-flutter>/android` est replié dans un projet `FLUTTER_ANDROID` plutôt
  que signalé deux fois.

Un marqueur Docker inclus dans un projet Flutter/Android enrichit ce projet sans
remplacer son type. Un `Dockerfile` sous un projet Compose n'est pas publié
comme projet séparé. Les dépendances sous `vendor` et `node_modules` sont
exclues.

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
premier adaptateur parcourt les `Dockerfile` et fichiers Compose standards sous
chaque projet avec des budgets de 50 000 entrées, 32 fichiers, une profondeur de
6 et 1 Mio par fichier. Il ne lance ni Docker ni une image.

Les images Temurin, OpenJDK, Corretto, Gradle et Maven permettent actuellement
d'extraire un niveau de fonctionnalité JDK. Une image `jre` n'est jamais traitée
comme un JDK ; une variable d'image ou une version non statique reste inconnue.
Une relation `DOCKER:MATCHED` prouve uniquement qu'un `FROM` déclaratif satisfait
l'exigence. Elle n'a aucun `resource_id` et n'entre pas dans l'inventaire HOST.
Le moteur ne produit pas encore de conclusion `DOCKER:MISSING`.
Les symlinks restent ignorés silencieusement par cet adaptateur, car Discovery
les a déjà comptés pour la même racine et possède le diagnostic de synthèse.

Chaque image externe statique d'un `FROM` et chaque valeur scalaire statique de
`services.*.image` devient aussi une exigence `docker/image`. Les alias de stage
et `scratch` sont exclus ; les interpolations et structures Compose non prises
en charge sont diagnostiquées sans interprétation. Cette représentation couvre
les images de toolchains non-Java sans déduire abusivement une installation
Node, Python ou PHP sur l'hôte.

### `internal/inventory`

Recense les installations dans des emplacements transmis par la CLI, qu'ils
soient explicites ou détectés. La mesure de taille possède des limites de temps,
de profondeur et de nombre d'entrées.

Le moteur d'inventaire exige toujours des racines typées ; leur déduction est la
responsabilité de `internal/autodetect` et de la CLI. Il reconnaît les paquets
Android structurés, les SDK Flutter directs ou sous FVM, les distributions
Wrapper et plugins AGP/Kotlin du cache Gradle, les JDK via leurs métadonnées
statiques, ainsi que Xcode, DerivedData, Device Support, CoreSimulator et les
Command Line Tools. Aucun gestionnaire ni exécutable inventorié n'est lancé.

Les appareils CoreSimulator portent une sensibilité explicite et un état
d'activité `unknown`; les téléchargements de composants visibles portent
`unknown_may_be_in_progress`. Les plist XML bornées servent uniquement à lire
des clés allowlistées. Un plist binaire ou malformé ne déclenche aucune
interprétation de remplacement. Ce lot n'appelle ni Xcode, ni `xcodebuild`, ni
`simctl`, et n'ajoute aucune action Apple au plan simulé.

Un bundle de runtime monté est détecté par son enveloppe et son nom, mais son
arbre n'est pas mesuré. Sur le Mac de validation, chacun dépassait 500 000
entrées ; répéter ce parcours pour chaque version pénalisait fortement la
commande par défaut et ne décrivait pas fidèlement l'espace physique APFS. La
métadonnée `size_status=skipped_high_cardinality_runtime_tree` rend cette
absence explicite.

La métrique `size_bytes` est la somme logique des fichiers réguliers. Les
symlinks ne sont pas suivis. Dès qu'une permission, une limite ou l'annulation
rend le parcours incomplet, la taille est absente et un diagnostic explique la
couverture manquante. La taille allouée APFS n'est pas estimée dans cette phase.

### `internal/dockerinventory`

Interroge facultativement le daemon avec cinq commandes Docker strictement
allowlistées et bornées. Il inventorie images, conteneurs, builders et cache
BuildKit sans demander commandes, labels, variables, montages ou contenus. Le
type `docker_image` est déclaré complètement couvert uniquement si la commande
de liste réussit sans timeout, troncature ni ligne malformée.

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

Les exigences `docker/image` sont normalisées selon les formes usuelles de
référence : registre Docker Hub implicite, namespace `library` et tag `latest`.
Elles sont comparées aux métadonnées `reference` et `digest` des images locales.
Une relation `DOCKER_DAEMON:MISSING` exige un inventaire d'images complet ; les
images locales non reliées restent `UNKNOWN` si la découverte de projets est
heuristique.

### `internal/classification`

Applique après corrélation des catégories multiples, déterministes et
accompagnées de preuves. `UTILISEE` exige une relation positive ou un état de
conteneur actif observé. `RECONSTRUCTIBLE` exige un chemin de reconstruction
identifiable : paquet `sdkmanager`, cache FVM versionné, cache Gradle, DerivedData
Xcode ou cache BuildKit. `SENSIBLE` dérive uniquement d'une métadonnée de
sensibilité explicite.

`ANCIENNE` compare une preuve fiable de dernière utilisation au seuil
`--old-after-days`, enregistré dans le rapport. Le moteur n'accepte actuellement
que `LastUsedAt` issu de `docker buildx du`; il ignore les dates de création et
les `mtime`. Les durées relatives d'anciens plugins Buildx utilisent une borne
basse conservatrice plutôt qu'une date reconstruite. `ORPHELINE_PROBABLE` exige
à la fois `NO_REFERENCE_FOUND`, `RECONSTRUCTIBLE` et `ANCIENNE`, et exclut toute
ressource sensible. En l'absence de preuve suffisante, `INCONNUE` reste explicite.

Un `SpaceEstimate` n'est produit que pour un cache BuildKit dont Docker affirme
qu'il est récupérable, non partagé et non mutable. Sa justification et ses
preuves restent attachées à la ressource. Les tailles observées et les espaces
potentiels ne sont pas additionnés comme s'ils étaient des octets uniques.

### `internal/planning`

Transforme un rapport validé en plan déterministe sans accéder au système de
fichiers, au réseau ni aux gestionnaires locaux. Par défaut, chaque ressource
est soit sélectionnée selon les invariants conservateurs, soit conservée dans
`excluded` avec des motifs structurés. Une liste `--resource` remplace cette
politique par une sélection manuelle limitée aux identifiants demandés.

Les actions prises en charge produisent uniquement des tableaux d'arguments
ciblés : filtre d'identifiant BuildKit, identifiant d'image ou de conteneur,
identifiant de paquet Android, ou nom d'AVD. Les localisateurs et métadonnées
sont validés avant d'entrer dans une commande. Aucune action générale, option
forcée, commande shell ou suppression directe de chemin n'est construite.

Les projets affectés sont recopiés depuis les relations du rapport. Les impacts,
catégories de risque, preuves, avertissements, tailles observées et estimations
restent attachés à chaque élément. Le total porte volontairement le nom
`known_potentially_reclaimable_bytes_sum` : il s'agit d'une somme d'estimations
connues, jamais d'un gain garanti.

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
refusent les champs inconnus et les valeurs JSON supplémentaires. Le rapport
terminal affiche séparément `observed_size` et `potentially_reclaimable`, puis
`explain` expose la justification et les preuves de chaque classification ou
estimation. Le second schéma embarqué `cleanup-plan-v1.schema.json` valide les
plans simulés. Leur décodage recalcule aussi l'identifiant de contenu afin de
rejeter toute altération après création.

Les rapports terminal de scan et de plan partagent la signature `◆ dev-audit`.
Elle n'utilise aucune séquence ANSI : le rendu reste déterministe, testable et
adapté à la redirection. Le JSON n'hérite d'aucun élément de présentation.

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
n'est modifié. Trois écritures explicites restent permises :

1. la sortie standard du terminal ;
2. un rapport vers le chemin fourni par `scan --output` ;
3. un plan simulé vers un nouveau chemin fourni par `plan --output`.

Le troisième cas utilise une création exclusive : un plan existant n'est
jamais tronqué ni remplacé. `plan` ne lance aucune commande qu'il décrit et la
CLI n'expose aucune primitive d'application du plan.

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
