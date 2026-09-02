# Plan d'implémentation — Phase 0

## 1. Décision et source de vérité

Le nom technique retenu est **Dev Environment Auditor** et la commande cible est
`dev-audit`. Le nom du dossier local n'est pas une contrainte fonctionnelle ; le
renommer pendant qu'il est ouvert dans l'outil de développement créerait un
risque inutile. Le futur dépôt distant devra idéalement être nommé
`dev-environment-auditor`.

Le document BSF « Prompt maître Codex — Auditeur d'environnements de
développement conscient des projets » définit la Phase 0. La vision produit plus
large reste archivée dans `docs/vision.md`, mais elle n'autorise pas l'ajout de
Docker, d'un nettoyeur ou d'une interface graphique pendant cette phase.

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

## 5. Architecture

```text
cmd/dev-audit             point d'entrée CLI
internal/domain           modèle et invariants
internal/discovery        découverte contrôlée des projets
internal/analyzers        analyse statique Flutter/Android/Gradle
internal/inventory        inventaire local et tailles estimées
internal/correlation      rapprochement exigences/ressources
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
couverture avec le détecteur de concurrence. Le prochain travail à faire valider
est le Lot 3 — Inventory.
