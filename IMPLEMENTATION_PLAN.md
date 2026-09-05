# Plan d'implémentation — Phase 0

## 1. Décision et source de vérité

Le nom technique retenu est **Dev Environment Auditor** et la commande cible est
`dev-audit`. Le nom du dossier local n'est pas une contrainte fonctionnelle ; le
renommer pendant qu'il est ouvert dans l'outil de développement créerait un
risque inutile. Le futur dépôt distant devra idéalement être nommé
`dev-environment-auditor`.

Le document BSF « Prompt maître Codex — Auditeur d'environnements de
développement conscient des projets » définit la Phase 0. La vision produit plus
large reste archivée dans `docs/vision.md`. Le MVP en lecture seule étant
validé, les fonctionnalités d'inventaire, de classification et de simulation
sont maintenant réintroduites par lots, sans ouvrir encore la frontière
destructive.

## 2. Approche produit

La valeur du projet repose sur une carte bidirectionnelle et explicable :

```text
projet -> exigences déclarées -> ressources installées
ressource installée -> projets détectés qui la requièrent
```

Chaque conclusion doit distinguer :

- le fait directement observé ;
- l'inférence raisonnable ;
- l'absence de référence dans la couverture analysée ;
- l'inconnu ou la contradiction.

L'espace occupé est une donnée d'inventaire. Il n'est pas une autorisation de
suppression et ne doit jamais être présenté comme de l'espace récupérable.

## 3. Pourquoi Go

Go est retenu pour le moteur et la CLI de la Phase 0 :

- compilation en binaire autonome pour une distribution simple sur macOS ;
- bibliothèque standard adaptée aux parcours de fichiers, à JSON et aux tests ;
- `filepath.WalkDir` parcourt dans un ordre lexical et ne suit pas les liens
  symboliques par défaut ;
- `os/exec` n'invoque pas de shell et `CommandContext` permet de borner la durée
  des sous-processus ;
- interfaces légères pour injecter système de fichiers, horloge et commandes
  dans les tests ;
- possibilité de conserver le moteur indépendant d'une future interface macOS.

Références officielles :

- <https://pkg.go.dev/path/filepath#WalkDir>
- <https://pkg.go.dev/os/exec>
- <https://go.dev/doc/modules/layout>

Swift reste une solution de repli si l'intégration native macOS devient une
contrainte immédiate. Rust n'est pas retenu pour la Phase 0 car son coût initial
n'améliore pas directement la preuve produit recherchée.

## 4. Installer Go

La version stable observée au démarrage du Lot 0 est Go 1.27.1. Le module déclare
une compatibilité Go 1.27.

### Option recommandée — Homebrew

```bash
brew install go
go version
```

Homebrew a installé Go 1.27.1 sur le poste de développement. Il n'est pas
nécessaire de mettre à niveau les 75 autres formules obsolètes pour compiler ce
projet, ni de faire confiance aux taps tiers MongoDB, ngrok ou Redis.

### Alternative — installeur officiel

1. Ouvrir <https://go.dev/dl/>.
2. Télécharger l'installeur macOS ARM64 de la version Go 1.27.x.
3. Exécuter le paquet et suivre l'assistant.
4. Fermer puis rouvrir le terminal.
5. Vérifier avec `go version`.

Le binaire produit ne demandera pas à l'utilisateur final d'installer Go.

### Installer la commande du produit

Go n'est requis que pour construire depuis les sources. L'interface utilisateur
est le binaire autonome `dev-audit`, installé depuis la racine du dépôt avec :

```bash
./scripts/install.sh
dev-audit scan
```

Le script privilégie un dossier standard déjà dans le `PATH` et accessible sans
`sudo`, accepte un dossier explicite en premier argument et se replie sur
`~/.local/bin`. Sur le Mac de validation, il est installé dans
`/opt/homebrew/bin`. Le scanner lui-même ne réalise aucune installation et ne
modifie aucun projet audité.

Pour la distribution sans Go, `scripts/build-release.sh` produit deux archives
précompilées `darwin/arm64` et `darwin/amd64`, injecte la version dans le binaire
et génère un checksum individuel ainsi que `SHA256SUMS`. Chaque archive embarque
`install.sh`, qui copie seulement le binaire déjà construit vers un dossier du
`PATH` et n'appelle jamais Go.

## 5. Architecture

```text
cmd/dev-audit             point d'entrée CLI
internal/domain           modèle et invariants
internal/discovery        découverte contrôlée des projets
internal/analyzers        analyse statique Flutter/Android/Gradle
internal/inventory        inventaire local et tailles estimées
internal/dockerinventory  inventaire borné du daemon Docker
internal/correlation      rapprochement exigences/ressources
internal/classification   catégories, ancienneté et espace potentiel expliqués
internal/evidence         preuves, confiance et diagnostics
internal/report           terminal et JSON v1
internal/platform         adaptateurs filesystem/command/clock
schemas                   contrats JSON versionnés
examples                  rapports d'exemple
testdata                  projets synthétiques
```

La logique métier dépend d'interfaces et non du terminal ou du système de
fichiers réel. Les reporters dépendent du domaine ; le domaine ne dépend d'aucun
reporter ni adaptateur système.

## 6. Modèle de confiance

Les états du document sont conservés, mais placés sur le bon concept :

- `Requirement.confidence` : `REQUIRED_EXPLICITLY`, `PROBABLY_REQUIRED`,
  `UNKNOWN` ;
- `InstalledResource.reference_status` : `REFERENCED`,
  `NO_REFERENCE_FOUND`, `UNKNOWN` ;
- `Relation.match_status` : `MATCHED`, `MISSING`, `AMBIGUOUS`, `UNKNOWN`.

Cette séparation évite de créer une fausse relation projet–ressource pour
représenter une ressource qui n'a aucune référence connue.

## 7. Lots et gates

| Lot | Résultat attendu | Gate |
|---|---|---|
| 0 — Fondation | Documentation, Git, architecture, ADR, domaine, JSON v1, backlog et fixtures. | Aucun scanner ni primitive destructive ; modèle et schéma vérifiables. |
| 1 — Discovery | Racines explicites, exclusions, Flutter/Android, limites et symlinks. | Fixtures chemins/permissions/symlinks ; aucun fichier modifié. |
| 2 — Analyseurs | Flutter/FVM, Dart, Gradle, AGP, SDK, NDK et JDK avec preuves. | Cas explicites, dynamiques et malformés couverts. |
| 3 — Inventory | Installations locales et tailles bornées. | Lecture seule prouvée ; erreurs partielles diagnostiquées. |
| 4 — Correlation | Graphe bidirectionnel, correspondances et inconnues. | Aucune inférence cachée ni faux « inutilisé ». |
| 5 — Reporting | `scan`, `explain`, terminal, JSON v1 et golden tests. | Schéma stable, sorties déterministes et codes de sortie documentés. |
| 6 — Validation | Paquet de test sur 10 à 15 machines. | Mesures BSF puis décision GO/PIVOT/STOP. |
| 7 — Android avancé | Images système et AVD, avec sensibilité explicite. | Détection réelle, lecture seule et aucune fausse recommandation. |
| 8 — Docker local | Compose, builders, cache, images et conteneurs en lecture seule. | Daemon optionnel, sorties bornées et séparation déclaration/état. |
| 9 — Classification | Catégories et ancienneté explicables. | Toute catégorie est prouvée ; l'inconnu reste bloquant. |
| 10 — Simulation | Plan de nettoyage sans exécution. | Impact et estimation visibles ; sensible/inconnu exclus par défaut. |

Les tâches restantes du Lot 6 liées à la publication publique, la signature, la
notarisation, Homebrew et la validation multi-machines sont des tâches de
finalisation. Elles seront reprises après les Lots 7 à 10.

## 8. Invariants transversaux

- aucune suppression, installation, réparation ou compilation ;
- aucun `sudo`, réseau, compte ou télémétrie ;
- aucune concaténation de commande dans un shell ;
- aucun suivi de symlink hors des racines autorisées ;
- sous-processus optionnels bornés en temps et en volume de sortie ;
- diagnostics sans secrets ni contenu de configuration privée ;
- erreurs partielles conservées dans le rapport ;
- ordre des collections stabilisé avant sérialisation ;
- écriture autorisée uniquement vers un rapport explicitement demandé ;
- tests vérifiant que contenu et dates de modification des fixtures restent
  inchangés.

## 9. Fin du Lot 0

Le Lot 0 est terminé lorsque la documentation ne se contredit plus, que le
modèle et le schéma JSON sont cohérents, que les fixtures minimales existent et
que les tests Go passent après installation de la toolchain. Aucun scanner réel
n'est inclus dans ce lot.

**État au 2 septembre 2026 : gate validé.** Go 1.27.1 a été installé avec
Homebrew ; `gofmt`, `go mod tidy`, `go test ./...`, `go vet ./...` et
`go run ./cmd/dev-audit version` ont réussi.

## 10. Fin du Lot 1

**État au 2 septembre 2026 : gate validé.** Le moteur Discovery contrôle les
racines, exclusions, symlinks, budgets et erreurs partielles. Il détecte les
frontières Flutter/Android sans lire le contenu des configurations. Les tests
standards et avec détecteur de concurrence passent ; `internal/discovery`
atteint 89,1 % de couverture. Ce gate a autorisé l'ouverture du Lot 2.

## 11. Fin du Lot 2

**État au 2 septembre 2026 : gate validé.** Les analyseurs internes extraient
les exigences Flutter/FVM, Dart, Gradle, AGP/Kotlin, SDK, NDK/CMake et JDK depuis
une allowlist de fichiers. Les expressions dynamiques ne sont jamais exécutées :
elles restent probables ou inconnues et ne portent aucune version fabriquée.

Les tests couvrent valeurs explicites, formats historiques, catalogues, valeurs
dynamiques, fichiers malformés, limites, annulation, symlinks et invariance du
contenu et des dates de modification. `internal/analyzers` atteint 83,0 % de
couverture avec le détecteur de concurrence. Ce gate a autorisé l'ouverture du
Lot 3 — Inventory.

## 12. Fin du Lot 3

**État au 2 septembre 2026 : gate validé.** L'inventaire reçoit des racines
typées et explicites pour Android SDK, Flutter SDK, cache FVM, Gradle User Home
et JDK. Il extrait les versions depuis des métadonnées allowlistées sans lancer
de gestionnaire de paquets ou d'exécutable installé. La version Dart embarquée
et les caches Wrapper/AGP/Kotlin restent ainsi corrélables au Lot 4.

La taille publiée est la somme logique complète des fichiers réguliers. Les
symlinks et fichiers spéciaux ne sont pas suivis ; toute erreur, annulation ou
limite dépassée supprime la taille au lieu de conserver un total partiel. Les
tests couvrent versions, fallbacks, données malformées, valeurs sensibles,
budgets, erreurs simulées, déterminisme et invariance des fichiers.
`internal/inventory` atteint 80,8 % de couverture avec le détecteur de
concurrence. Ce gate a autorisé l'ouverture du Lot 4 — Correlation.

## 13. Fin du Lot 4

**État au 2 septembre 2026 : gate validé.** Le moteur de corrélation produit une
relation pour chaque exigence sans modifier les résultats des analyseurs ou de
l'inventaire. Il normalise `compile_sdk` vers une plateforme Android, relie Dart
au SDK embarqué dans Flutter, compare les versions exactes pour Android,
Flutter, Gradle et les plugins, et compare un JDK par niveau de fonctionnalité.

Le sous-ensemble Dart couvre les versions stables `x.y.z`, `any`, les bornes
`>=`, `>`, `<=`, `<`, l'égalité et la notation caret. Les préversions, suffixes
de build et syntaxes composées non prises en charge restent inconnus avec un
diagnostic, sans lancer `dart pub`.

Deux preuves de couverture sont séparées : une liste de types d'inventaire
complets autorise `MISSING`, tandis que la combinaison découverte complète et
analyse complète autorise `NO_REFERENCE_FOUND`. Toute exigence non explicite,
version candidate absente ou correspondance multiple protège les ressources
possibles contre un faux statut « non référencé ». Les composants inventoriés
mais non analysés, comme Android Build Tools, restent également `UNKNOWN`.

Les tests couvrent correspondances, absences, ambiguïtés, contraintes Dart,
canaux Flutter, JDK historiques et modernes, copies profondes et déterminisme.
`internal/correlation` atteint 87,7 % de couverture avec le détecteur de
concurrence. Ce gate a autorisé l'ouverture du Lot 5 — Reporting.

## 14. Fin du Lot 5

**État au 2 septembre 2026 : gate initial validé.** `dev-audit scan` assemble
Discovery, les analyseurs, quatre passages d'inventaire indépendants et la
corrélation sous un timeout global. À ce gate initial, les racines de projets
étaient obligatoires et les racines Android, Flutter/FVM, Gradle et JDK étaient
explicites, typées et répétables. Une famille sans `WARNING` ni `ERROR` pouvait
seule autoriser une conclusion `MISSING`.

Le rapport terminal résume projets, exigences, relations, ressources et
diagnostics en échappant les caractères de contrôle. Le rapport JSON est trié,
encodé puis validé contre le schéma JSON 2020-12 embarqué avec validation des
formats. `dev-audit explain --report FILE ID` valide à nouveau le document avant
d'exposer preuves, justification et statut d'un projet, d'une exigence ou d'une
ressource.

Seul `--output` autorise une écriture. Les fichiers sont créés ou remplacés en
`0600`, les symlinks et fichiers non réguliers sont refusés, et `explain` ne peut
pas écraser sa source. Les rapports d'entrée sont bornés à 64 Mio. Les codes de
sortie distinguent succès (`0`), rapport partiel ou identifiant absent (`1`),
usage invalide (`2`) et erreur opérationnelle (`3`).

Les tests d'intégration vérifient le pipeline réel sur les fixtures, l'invariance
des métadonnées inventoriées, les formats et les codes de sortie. Les golden
tests figent les sorties terminal et JSON ; le JSON est contrôlé par le schéma
embarqué. Avec `go test -race -cover ./...`, la couverture atteint 95,1 % pour
`internal/application`, 78,2 % pour `internal/report` et 79,3 % pour la CLI. Le
prochain travail à faire valider est le Lot 6 — Validation terrain.

**Durcissement issu du premier test terrain, 3 septembre 2026.** La CLI détecte
désormais les racines omises depuis l'environnement, le `PATH`, les emplacements
conventionnels et une recherche profonde bornée. Les options explicites restent
disponibles et désactivent la détection pour leur famille. Pour préserver les
invariants de corrélation, une couverture heuristique peut établir `MATCHED`,
mais ne peut produire ni `MISSING` ni `NO_REFERENCE_FOUND`.

La commande autonome s'installe via `scripts/install.sh`. Les diagnostics de
symlinks sont regroupés par racine. Les relations distinguent `HOST` et
`DOCKER` ; une lecture statique et bornée des `Dockerfile` peut établir qu'une
image JDK déclarée correspond à l'exigence sans la présenter comme une
installation locale ni lancer Docker. Sur le projet de validation Chambrage,
Java 21 ressort ainsi `HOST:UNKNOWN` et `DOCKER:MATCHED`.

Le paquet ARM64 `0.1.0-mvp` a aussi été installé et exécuté avec un `PATH`
volontairement privé de Go. La version et un scan JSON ont réussi. Le binaire
Intel a été vérifié comme Mach-O `x86_64`, mais doit encore être exécuté sur une
machine Intel réelle. Ces contrôles de paquet sur le Mac 01 ne remplacent pas la
validation prévue sur 10 à 15 machines.

## 15. Fin du Lot 7

**État au 5 septembre 2026 : gate validé.** L'inventaire Android couvre
maintenant les images système par API, tag et ABI ainsi que les dossiers AVD.
Une racine AVD distincte est détectée depuis les variables Android documentées,
`~/.android/avd` et la recherche profonde bornée. Les pointeurs et symlinks ne
sont pas suivis.

Les images système exposent leur identifiant de paquet `sdkmanager`. Les AVD
sont marqués sensibles, restent `UNKNOWN` et ne donnent lieu à aucune action.
Sur le Mac 01, le scan zéro-configuration a trouvé 2 images système et 1 AVD,
sans diagnostic `WARNING` ou `ERROR`. Le prochain lot fonctionnel est le Lot 8 —
Inventaire Docker en lecture seule ; la publication et la validation
multi-machines restent reportées à la finalisation.

## 16. Lot 8 terminé — état Docker local et déclarations Compose

Le scanner détecte maintenant la CLI Docker et traite un daemon absent comme
une capacité optionnelle. Lorsqu'il répond, cinq commandes de lecture au maximum
collectent la version, les images, les conteneurs, les builders et le cache
BuildKit. Chaque commande possède un timeout de processus et une limite de
sortie ; l'ensemble reste borné par le timeout global du scan.

Les formats CLI demandent uniquement des champs allowlistés. Les commandes,
labels, variables, montages et contenus des conteneurs ne sont jamais demandés.
Les ressources Docker utilisent un localisateur `docker://` pour ne pas les
confondre avec un chemin de l'hôte.

Le test réel du Mac 01 a trouvé 51 images, 19 conteneurs, 2 builders et 44
enregistrements BuildKit sans diagnostic `WARNING` ou `ERROR`. Il a également
permis de rendre le scanner compatible avec un plugin Buildx plus ancien que la
documentation courante : les timeouts sont imposés par Go plutôt que par une
option CLI facultative.

La seconde étape détecte les projets Docker seuls, les quatre noms Compose
standards et `Dockerfile.*`. Les `FROM` externes et `services.*.image` statiques
deviennent des exigences `docker/image`; cela couvre les toolchains non-Java
sans prétendre qu'elles sont installées sur l'hôte. Les dépendances `vendor` et
`node_modules`, les alias multi-stage, `scratch` et les valeurs dynamiques sont
exclus ou laissés inconnus.

La corrélation `DOCKER_DAEMON` compare ces exigences aux références d'images du
daemon. Sur le Mac 01, le scan complet a trouvé 8 projets Docker seuls, 34
exigences d'images, 10 correspondances et 24 images déclarées absentes. Le même
passage a inventorié 52 images, 19 conteneurs, 2 builders et 48 enregistrements
BuildKit, sans diagnostic `WARNING` ou `ERROR`. Le prochain lot fonctionnel est
le Lot 9 — classification explicable.

## 17. Lot 9 terminé — classification explicable

Le moteur applique après corrélation six catégories non exclusives :
`UTILISEE`, `RECONSTRUCTIBLE`, `ANCIENNE`, `ORPHELINE_PROBABLE`, `SENSIBLE` et
`INCONNUE`. Chaque catégorie contient une justification et des preuves
structurées. Le contrat interdit les combinaisons contradictoires et maintient
les anciens rapports JSON v1 lisibles grâce à des champs additifs optionnels.

La politique `--old-after-days` vaut 180 jours par défaut et est enregistrée
avec le scan. Une catégorie `ANCIENNE` exige une vraie preuve de dernière
utilisation ; seule la valeur `LastUsedAt` de `docker buildx du` est reconnue
dans ce lot. Une date de création ou un `mtime` ne sont jamais substitués à une
preuve d'usage. Un ancien plugin Buildx peut fournir une durée relative : elle
est alors évaluée par borne basse conservatrice sans fabriquer de timestamp.

`ORPHELINE_PROBABLE` ne peut apparaître que si une ressource est simultanément
sans référence dans une couverture complète, reconstructible et ancienne selon
une preuve fiable, tout en n'étant pas sensible. La taille observée est séparée
d'un objet `potentially_reclaimable` justifié. Ce dernier est limité aux caches
BuildKit déclarés récupérables par Docker, non partagés et non mutables ; il
n'autorise aucune action.

Le gate a été validé par les tests avec détecteur de concurrence, `go vet` et
deux scans réels du Mac 01. Avec le seuil par défaut de 180 jours, aucune preuve
n'a dépassé la politique et aucune ressource n'a été artificiellement vieillie.
Avec un seuil de 1 jour, 41 caches BuildKit observés à 2 ou 3 jours ont reçu
`ANCIENNE`, tout en conservant `INCONNUE` et sans devenir
`ORPHELINE_PROBABLE`. Le prochain lot fonctionnel est le Lot 10 — plan de
nettoyage simulé sans exécution.

## 18. Lot 10 terminé — plan de nettoyage simulé et immuable

`dev-audit plan --report FILE` transforme désormais un rapport JSON v1 validé
en un artefact `SIMULATION_ONLY`. Chaque élément contient l'impact, les projets
affectés, les catégories de risque, les preuves, la taille observée,
l'estimation éventuelle et une commande officielle sous forme de tableau
d'arguments. La commande n'est jamais exécutée et la CLI ne possède aucune
primitive `apply` ou `execute`.

La politique `DEFAULT_SAFE` exige `ORPHELINE_PROBABLE`, une estimation
conservatrice et un adaptateur ciblé, tout en interdisant `SENSIBLE`, `INCONNUE`
et `UTILISEE`. `--resource ID` permet une simulation manuelle limitée aux
identifiants choisis ; les risques restent visibles et chaque élément exige une
confirmation explicite. Une ressource absente ou sans action prise en charge
reste dans `excluded`.

Le contrat `cleanup-plan-v1.schema.json` est embarqué. Le plan contient le
SHA-256 exact du rapport source et son `plan_id` couvre l'intégralité de son
contenu hors identifiant. Le décodage rejette toute altération. Une sortie
fichier est créée en `0600` avec création exclusive et ne peut pas écraser un
plan existant.

Les adaptateurs actuels décrivent des actions ciblées pour les caches BuildKit,
images et conteneurs Docker, les paquets `sdkmanager` et les AVD `avdmanager`.
Ils ne produisent ni shell, ni `docker system prune`, ni `--force`, ni
suppression directe de chemin.

Le gate réel du Mac 01 a utilisé le rapport du Lot 9 afin de ne pas perturber le
téléchargement Xcode du simulateur iOS 26.5. La sélection automatique a produit
0 élément et 189 exclusions, conformément à l'absence d'orpheline probable. La
simulation manuelle d'un cache BuildKit `INCONNUE` a exposé son estimation de
48 670 000 octets et le filtre ciblé attendu. Les empreintes Docker normalisées
sont restées inchangées. Tests `-race`, couverture, `go vet`, installation dans
`/opt/homebrew/bin` et exécution par `dev-audit plan` ont réussi.
