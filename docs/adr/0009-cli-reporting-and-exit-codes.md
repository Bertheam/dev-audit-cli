# ADR 0009 — CLI, rapports validés et codes de sortie

- Statut : accepté
- Date : 2026-09-02

## Contexte

Les moteurs des Lots 1 à 4 étaient testables séparément mais non accessibles à
un utilisateur. Leur assemblage doit conserver les erreurs partielles, éviter
les inférences de couverture et produire un contrat JSON réellement vérifié.
Les chemins des projets et SDK sont des données locales potentiellement
sensibles ; toute écriture doit rester explicite.

## Décision

La CLI expose trois commandes :

- `dev-audit version` ;
- `dev-audit scan --root PATH [options]` ;
- `dev-audit explain --report FILE ID`.

`--root` est obligatoire et répétable. Les inventaires Android, Flutter, FVM,
Gradle et JDK possèdent chacun une option répétable. Aucun emplacement n'est
déduit automatiquement du dossier personnel ou d'une variable d'environnement.
L'absence de racine d'inventaire est autorisée, accompagnée d'un diagnostic
informatif et sans conclusion `MISSING`.

Un timeout de 30 secondes borne par défaut tout le pipeline ; l'utilisateur peut
le modifier. L'orchestrateur exécute chaque famille d'inventaire séparément afin
qu'une erreur Android n'annule pas la couverture prouvée de Gradle ou JDK. Un
`WARNING` ou `ERROR` rend conservativement incomplète l'étape concernée.

La sortie par défaut est le terminal. `--format json` sérialise le document
normalisé puis le valide contre `schemas/scan-v1.schema.json`, embarqué dans le
binaire. La bibliothèque `github.com/santhosh-tekuri/jsonschema/v6` 6.0.3 est
retenue pour JSON Schema 2020-12 et la validation des formats. Elle ne charge
aucune ressource distante à l'exécution.

`explain` accepte uniquement un rapport JSON v1 valide, limité à 64 Mio. Il
refuse les champs inconnus, les documents concaténés et un fichier de sortie
identique à sa source. Les chaînes affichées dans le terminal sont échappées
pour empêcher l'injection de séquences de contrôle.

Les codes de sortie sont :

| Code | Signification |
|---|---|
| `0` | succès sans diagnostic `ERROR` |
| `1` | rapport partiel, ou identifiant demandé absent |
| `2` | arguments ou usage invalides |
| `3` | erreur de lecture, validation, rendu ou écriture |

`--output` est l'unique autorisation d'écriture. La destination doit être un
fichier régulier et non un symlink ; elle est créée ou remplacée avec les
permissions `0600`.

## Conséquences

La CLI est utilisable sans installation Android ou Flutter globale, mais ne
prétend alors rien sur les ressources manquantes. Un rapport partiel reste
exploitable et son code de sortie est automatisable. La dépendance de validation
ajoute un petit coût de compilation, en échange d'une vérification exacte du
contrat public et de tests golden reproductibles.

La Phase 0 ne propose toujours aucune suppression, installation, réparation,
construction, télémétrie, interface graphique ou découverte automatique de
racines.

## Références

- JSON Schema 2020-12 : <https://json-schema.org/draft/2020-12>
- validateur Go retenu : <https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6>
