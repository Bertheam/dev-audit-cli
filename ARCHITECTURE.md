# Architecture — Dev Environment Auditor

## Contexte

La Phase 0 est une CLI macOS locale, limitée à Flutter et Android. Elle produit
des observations vérifiables sans modifier les projets ni les toolchains.

## Flux principal

```text
Arguments CLI
  -> validation des racines et exclusions
  -> discovery
  -> analyzers
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

### `internal/domain`

Contient les types `Scan`, `Project`, `Requirement`, `InstalledResource`,
`Relation`, `Evidence` et `Diagnostic`, ainsi que leurs invariants.

### `internal/discovery`

Parcourt uniquement les racines autorisées. Il applique des exclusions, ne suit
pas les symlinks et détecte les frontières de projets sans interpréter leurs
versions.

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

### `internal/inventory`

Recense les installations dans des emplacements explicitement autorisés ou
déduits de variables connues. La mesure de taille possède des limites de temps,
de profondeur et de nombre d'entrées.

### `internal/correlation`

Normalise les versions et relie exigences et ressources. Il ne transforme jamais
une absence de correspondance en recommandation de suppression.

### `internal/evidence`

Centralise les preuves, les règles de confiance, la suppression des valeurs
sensibles et les diagnostics.

### `internal/report`

Trie les collections puis produit une représentation terminal ou JSON. Le JSON
est validé contre `schemas/scan-v1.schema.json`.

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
