# Dev Environment Auditor

> Statut : MVP CLI 0.1.0 validé ; extensions fonctionnelles en cours
> Plateforme : macOS
> Périmètre : Flutter, Android, Docker et inventaire Xcode/iOS
> Interface cible : CLI `dev-audit`

Dev Environment Auditor doit établir une carte vérifiable entre les projets
Flutter/Android/Docker et les toolchains locales qu'ils exigent, ainsi que les
ressources Xcode/iOS qui occupent le poste. La Phase 0
privilégie la preuve, la traçabilité et l'expression honnête de l'incertitude.

Le projet n'est pas un nettoyeur. Il ne contient aucune fonction de suppression,
d'installation, de réparation, de build, de télémétrie ou de synchronisation.

## Objectif de la Phase 0

La CLI devra :

- détecter automatiquement les racines probables, avec mode explicite disponible ;
- découvrir les projets Flutter, Android et Docker sans suivre les symlinks externes ;
- extraire les exigences Flutter/FVM, Dart, Gradle, AGP, JDK, SDK et NDK ;
- inventorier les installations pertinentes et estimer leur taille ;
- relier projets, exigences et ressources installées ;
- distinguer les correspondances de l'hôte de celles déclarées par Docker ;
- distinguer aussi une déclaration d'image de sa présence dans le daemon local ;
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

Le premier passage terrain a ajouté une auto-détection locale et bornée. Sans
option de chemin, la CLI cherche les projets et les installations Android,
Flutter/FVM, Gradle et JDK dans l'environnement, le `PATH`, les emplacements
macOS usuels et les dossiers de développement courants. Elle ne lance aucun
outil détecté. Une couverture trouvée heuristiquement peut produire un
`MATCHED`, mais jamais un `MISSING` ou `NO_REFERENCE_FOUND`.

Le durcissement suivant sépare maintenant les relations `HOST` et `DOCKER`.
L'analyse statique et bornée des `Dockerfile` reconnaît les images JDK usuelles
et peut prouver qu'une exigence Java est déclarée dans Docker sans prétendre que
l'image est téléchargée ou qu'un conteneur tourne. Une référence d'image
dynamique reste `UNKNOWN`. Les symlinks ignorés sont résumés une seule fois par
racine au lieu de produire un diagnostic par lien.

L'extension Android suivante inventorie aussi les images système installées et
les Android Virtual Devices. Les racines AVD sont recherchées selon les
variables Android documentées, l'emplacement `~/.android/avd` et la recherche
profonde bornée. Une image système expose son API, son tag, son ABI et son
identifiant de paquet `sdkmanager`. Un AVD reste toujours `UNKNOWN` à ce stade :
il est marqué sensible parce que ses disques peuvent contenir des données
mutables et aucune activité récente fiable n'est encore établie.

L'inventaire Docker local est désormais activé par défaut lorsqu'une CLI Docker
est présente. Il vérifie si le daemon répond, puis observe les images,
conteneurs, builders et enregistrements de cache BuildKit par des commandes de
liste bornées. Les images sont dédupliquées par identifiant ; leur taille
virtuelle peut partager des couches. Les conteneurs sont marqués sensibles et
seule la taille de leur couche inscriptible est publiée. Le cache conserve le
signal `Reclaimable` produit par Docker, accompagné d'un avertissement clair :
ce signal n'autorise aucune suppression.

L'analyse Docker couvre maintenant les quatre noms Compose standards et les
`Dockerfile` usuels. Chaque `FROM` externe statique et chaque
`services.*.image` statique devient une exigence `docker/image`, y compris pour
Node, Python, PHP, bases de données et autres images non-Java. Les alias de
stages multi-stage, `scratch`, les valeurs interpolées et les structures YAML
non prouvées ne deviennent pas de fausses exigences. Les projets uniquement
Docker sont découverts comme `DOCKER`; les dossiers de dépendances `vendor` et
`node_modules` restent exclus.

Une exigence d'image est comparée aux références de l'inventaire global du
daemon dans l'environnement `DOCKER_DAEMON`. Cette relation peut pointer vers
une ressource `docker_image`. Elle est différente de `DOCKER`, qui décrit une
toolchain déclarée dans un fichier sans affirmer que l'image existe localement.

Le Lot 9 ajoute enfin une classification multiple et expliquée des ressources.
`UTILISEE`, `RECONSTRUCTIBLE`, `ANCIENNE`, `ORPHELINE_PROBABLE`, `SENSIBLE` et
`INCONNUE` sont des observations accompagnées d'une justification et de leurs
preuves. Une ressource peut cumuler des catégories compatibles, par exemple
`RECONSTRUCTIBLE,ANCIENNE,INCONNUE`. `ORPHELINE_PROBABLE` exige simultanément
une absence de référence dans une couverture complète, une reconstruction
démontrable et une dernière utilisation fiable plus ancienne que la politique ;
`NO_REFERENCE_FOUND` seul ne suffit jamais.

Le seuil d'ancienneté vaut 180 jours par défaut. À ce stade, seule la date
`LastUsedAt` fournie par `docker buildx du` est acceptée comme preuve de dernière
utilisation ; les dates de création et les `mtime` de dossiers ne le sont pas.
Les plugins récents la fournissent en RFC 3339 ; les plugins plus anciens
peuvent fournir une durée relative, conservée telle quelle et évaluée avec une
borne basse prudente sans inventer de date exacte.

Le rapport sépare `observed_size` de `potentially_reclaimable`. Cette dernière
est publiée uniquement pour un enregistrement BuildKit marqué récupérable par
Docker, non partagé et non mutable. Elle reste une estimation par ressource,
jamais une recommandation ou un total garanti.

Le Lot 10 ajoute `dev-audit plan`, qui transforme un rapport JSON validé en un
plan de nettoyage strictement simulé. Le mode automatique ne sélectionne que
les ressources `ORPHELINE_PROBABLE` disposant d'une estimation conservatrice et
d'une commande officielle ciblée ; toute ressource `SENSIBLE`, `INCONNUE` ou
`UTILISEE` est exclue. `--resource` permet d'examiner manuellement un identifiant
précis, mais ne lance toujours rien et expose les risques au lieu de les masquer.

Un plan JSON est lié au SHA-256 exact du rapport source et possède lui-même un
identifiant calculé sur son contenu. Sa sortie fichier est créée en `0600` et
n'est jamais écrasée. Le binaire ne contient aucune commande `apply`, `execute`
ou équivalent : les commandes documentées restent des tableaux d'arguments
destinés à la revue.

Le Lot 11 étend l'auto-détection aux installations `Xcode*.app`, à
`~/Library/Developer` et à `/Library/Developer`, sans appeler `xcodebuild`,
`simctl` ou Xcode. L'inventaire distingue l'application Xcode, DerivedData,
iOS Device Support, les runtimes CoreSimulator, les appareils simulés, les
téléchargements de composants observables et les Command Line Tools. Les
simulateurs sont `SENSIBLE + INCONNUE`; un téléchargement reste potentiellement
actif et aucune ressource Apple ne reçoit d'action de planification.

La sortie terminal porte une identité compacte — `◆ dev-audit` — commune au
scan et à la revue de plan. Elle reste volontairement sans séquence de couleur
ANSI afin de demeurer propre dans un pipe, un fichier ou un journal. Les
contrats JSON et la sortie scriptable de `dev-audit version` ne changent pas.

Go 1.27.1 est installé via Homebrew sur le poste de développement. Les tests,
les tests avec détecteur de concurrence, `go vet` et la commande
`dev-audit version` passent.

La vision plus large d'origine est conservée dans
[`docs/vision.md`](docs/vision.md). Le MVP Phase 0 reste figé ; l'inventaire du
daemon et des caches Docker, la simulation de plan et l'inventaire Xcode/iOS ont
été ajoutés comme extensions post-MVP. Le lancement de conteneurs, l'exécution
d'un nettoyage, la GUI et le cloud restent hors périmètre actuel.

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
go test ./cmd/dev-audit
```

Version de développement actuelle : `0.3.0-dev`. La baseline MVP distribuée
localement reste `0.1.0-mvp`.

Validation structurelle du JSON sans Go :

```bash
python3 -m json.tool examples/scan-v1.minimal.json
```

## Installer depuis les sources

L'utilisateur final n'a pas besoin de connaître `go run`. Depuis la racine du
dépôt, l'installateur construit un binaire autonome et choisit un dossier déjà
présent dans le `PATH` et accessible sans `sudo` :

```bash
./scripts/install.sh
dev-audit version
```

Sur un Mac Apple Silicon avec Homebrew, le choix automatique est généralement
`/opt/homebrew/bin`. Un autre dossier peut être donné explicitement :

```bash
./scripts/install.sh /opt/homebrew/bin
dev-audit version
```

Si aucun dossier standard accessible n'existe, le repli est `~/.local/bin` et
le script affiche la ligne `export PATH=...` à appliquer.

L'installation du binaire est une opération de distribution distincte du scan.
La commande `dev-audit scan` reste intégralement en lecture seule.

Cette méthode construit le binaire et nécessite donc Go. Pour un utilisateur qui
ne possède pas Go, utiliser une archive précompilée.

## Installer une archive précompilée sans Go

Choisir `darwin_arm64` pour un Mac Apple Silicon ou `darwin_amd64` pour un Mac
Intel. Après téléchargement de l'archive et de son fichier `.sha256` :

```bash
shasum -a 256 -c dev-audit_0.1.0-mvp_darwin_arm64.tar.gz.sha256
tar -xzf dev-audit_0.1.0-mvp_darwin_arm64.tar.gz
cd dev-audit_0.1.0-mvp_darwin_arm64
./install.sh
dev-audit scan
```

L'archive contient déjà l'exécutable. Son `install.sh` n'appelle ni `go` ni un
gestionnaire de paquets.

Pour fabriquer les archives en tant que mainteneur :

```bash
./scripts/build-release.sh 0.1.0-mvp
cd dist
shasum -a 256 -c SHA256SUMS
```

Le dossier `dist/` reçoit les archives ARM64 et Intel, un checksum individuel
pour chacune et le fichier récapitulatif `SHA256SUMS` ; il n'est pas versionné
dans Git.

## Utiliser la CLI

Usage recommandé sans connaissance préalable des chemins :

```bash
dev-audit scan
```

La CLI effectue d'abord les détections directes : variables d'environnement,
exécutables présents dans le `PATH`, emplacements macOS et gestionnaires usuels.
Elle complète par défaut avec une recherche profonde bornée dans les dossiers
de développement courants et les sous-dossiers de premier niveau de
`Documents`. `testdata`, les caches générés et les symlinks ne sont pas
parcourus.

Pour limiter la recherche à une racine de projets connue tout en laissant les
toolchains être détectées automatiquement :

```bash
dev-audit scan \
  --root /Users/alice/Projects \
  --timeout 30s
```

Le mode entièrement explicite reste disponible pour un audit reproductible et
pour autoriser les conclusions d'absence dans le périmètre déclaré :

```bash
dev-audit scan \
  --auto-detect=false \
  --root /Users/alice/Projects \
  --android-sdk-root /Users/alice/Library/Android/sdk \
  --android-avd-root /Users/alice/.android/avd \
  --flutter-sdk-root /Users/alice/Developer/flutter \
  --fvm-cache-root /Users/alice/fvm/versions \
  --gradle-user-home /Users/alice/.gradle \
  --jdk-root /Library/Java/JavaVirtualMachines \
  --xcode-root /Applications/Xcode.app \
  --apple-developer-root /Users/alice/Library/Developer \
  --apple-developer-root /Library/Developer
```

Les options de racine et `--exclude` sont répétables. Un type de racine fourni
explicitement remplace l'auto-détection de cette famille. `--deep-search=false`
conserve les détections directes sans le parcours approfondi.

Pour les AVD, la détection directe consulte `ANDROID_AVD_HOME`, puis les
sous-dossiers `avd` de `ANDROID_USER_HOME` et `ANDROID_EMULATOR_HOME`, ainsi que
`~/.android/avd`. Les pointeurs `.ini` externes ne sont pas suivis : seuls les
dossiers `*.avd` réellement contenus dans une racine validée sont inventoriés.

Les racines automatiques sont toujours considérées comme heuristiques. Elles
permettent d'établir une correspondance positive, mais ne prouvent pas que le
reste du disque a été couvert. Les statuts `MISSING` exigent donc une racine
d'inventaire explicite et `NO_REFERENCE_FOUND` une racine de projets explicite.

Pour Xcode, `DEVELOPER_DIR` est normalisé vers l'application correspondante,
les applications `Xcode*.app` sont recherchées dans `/Applications` et
`~/Applications`, puis les deux racines Developer standards sont validées par
leurs marqueurs structurels. Une application Xcode non standard placée sous un
dossier de développement peut aussi être retrouvée par la recherche profonde
bornée. La CLI lit uniquement le système de fichiers : l'état « booted » d'un
simulateur et la progression exacte d'un téléchargement restent inconnus.
Les arbres montés des runtimes, qui contiennent plusieurs centaines de milliers
de fichiers, ne sont pas parcourus pour calculer une taille : le runtime est
inventorié, mais sa taille reste inconnue plutôt que lente ou partielle.

L'inventaire du daemon Docker est optionnel et activé par défaut :

```bash
dev-audit scan --docker-inventory=false
```

Docker absent ou arrêté n'empêche pas le reste du scan et produit seulement un
diagnostic `INFO`. Le scanner n'exécute ni `prune`, ni `rm`, ni `pull`, ni `run`,
ni commande de build. Il ne collecte pas les commandes, labels, variables,
montages ou contenus des conteneurs. `plan` peut documenter une commande ciblée
mais ne l'exécute pas.

La politique d'ancienneté peut être ajustée sans modifier les autres règles :

```bash
dev-audit scan --old-after-days 90
```

La valeur choisie est enregistrée dans `scan.classification_policy` du rapport
JSON. Réduire le seuil peut produire davantage de catégories `ANCIENNE`, mais
ne transforme toujours aucune catégorie en ordre de suppression.

Une exigence peut avoir simultanément deux lectures dans le rapport :

```text
[HOST:UNKNOWN DOCKER:MATCHED] "java"/"jdk" constraint="21"
```

`HOST` compare l'exigence aux installations inventoriées sur le Mac. `DOCKER`
compare cette même exigence aux images JDK déclarées statiquement dans les
`Dockerfile`. Un match Docker n'est jamais ajouté aux ressources installées de
l'hôte, n'exécute pas Docker et ne prouve ni le téléchargement de l'image ni
l'état d'un conteneur.

Une image déclarée possède une lecture distincte de l'état local :

```text
[DOCKER_DAEMON:MATCHED] "docker"/"image" constraint="postgres:17-alpine"
```

`DOCKER_DAEMON:MATCHED` signifie que la référence est présente dans la liste
d'images du daemon courant. `DOCKER_DAEMON:MISSING` signifie uniquement qu'elle
n'est pas présente dans cet inventaire complet ; la CLI ne la télécharge pas et
ne lance aucun conteneur.

Créer un rapport JSON local puis expliquer un identifiant affiché :

```bash
dev-audit scan \
  --format json \
  --output audit.json

dev-audit explain --report audit.json resource-0123456789abcdef
```

Créer ensuite un plan conservateur en lecture seule :

```bash
dev-audit plan \
  --report audit.json \
  --format json \
  --output cleanup-plan.json
```

Sans `--resource`, la sélection est `DEFAULT_SAFE`. Il est normal qu'elle soit
vide si le scan n'a établi aucune `ORPHELINE_PROBABLE` : l'absence de certitude
ne devient pas une proposition de nettoyage.

Pour simuler uniquement des ressources choisies par leur identifiant :

```bash
dev-audit plan \
  --report audit.json \
  --resource resource-0123456789abcdef \
  --resource resource-fedcba9876543210
```

Ce mode `EXPLICIT_RESOURCE_IDS` peut montrer un élément `SENSIBLE`, `INCONNUE`
ou `UTILISEE`, accompagné d'avertissements. Il s'agit d'une sélection pour la
revue, pas d'une confirmation d'exécution. Une ressource absente ou sans action
ciblée prise en charge est placée dans `excluded` et donne le code de sortie
`1`. Les actions décrites actuellement couvrent les caches BuildKit, images et
conteneurs Docker, les paquets Android `sdkmanager` et les AVD gérés par
`avdmanager`. Aucun `docker system prune`, `--force` ou suppression directe de
chemin n'est produit.

Un fichier de sortie est créé avec les permissions `0600`. Les rapports de scan
refusent une sortie qui est un symlink et `explain` refuse d'écraser son rapport
source. Un plan refuse en plus tout chemin de sortie déjà existant, afin que le
contenu validé ne soit pas remplacé silencieusement.

Codes de sortie :

- `0` : commande terminée sans diagnostic `ERROR` ;
- `1` : rapport partiel avec diagnostic `ERROR`, identifiant absent, ou
  sélection manuelle partiellement exclue du plan ;
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
- [`schemas/cleanup-plan-v1.schema.json`](schemas/cleanup-plan-v1.schema.json) :
  contrat du plan simulé immuable.

## Règle de sécurité centrale

Une absence de référence signifie uniquement qu'aucune référence n'a été trouvée
dans le périmètre effectivement analysé. Elle ne signifie jamais qu'une ressource
est inutilisée ou sûre à supprimer. Les classifications et l'espace
potentiellement récupérable restent des observations à examiner, pas des
recommandations de nettoyage.
