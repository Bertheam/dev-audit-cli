# ADR 0011 — Séparer les relations HOST et DOCKER

- Statut : accepté
- Date : 2026-09-03

## Contexte

Le premier test terrain a trouvé une API qui exige Java 21 et fonctionne dans
une image Temurin 21, alors que le Mac ne possède pas de JDK 21 local observé.
Une relation unique ferait croire soit que l'application est cassée, soit que
l'image Docker est une installation locale. Les diagnostics individuels émis
pour chaque symlink rendaient aussi les rapports inutilement longs.

## Décision

Chaque relation porte désormais un environnement `HOST` ou `DOCKER`. Le champ
JSON reste optionnel afin que les premiers rapports v1, où il était absent,
continuent à se lire comme des relations `HOST`.

- `HOST` est produit par la corrélation entre exigences et ressources réellement
  inventoriées sur la machine.
- `DOCKER` est produit par une lecture statique et bornée des instructions
  `FROM` des `Dockerfile` appartenant aux projets détectés.
- Une relation Docker ne possède jamais de `resource_id`.
- Un match Docker ne prouve pas que l'image est présente ou qu'un conteneur est
  actif.
- Les images dynamiques, les versions non reconnues et les incompatibilités ne
  produisent aucune fausse conclusion `DOCKER:MISSING`.
- Le premier sous-ensemble reconnaît les niveaux JDK des images Temurin,
  OpenJDK, Corretto, Gradle et Maven ; les images JRE sont exclues.

Les symlinks restent ignorés. Discovery publie un seul diagnostic de comptage
par racine au lieu d'une entrée par lien. L'analyse Docker ne duplique pas ce
diagnostic pour les mêmes projets.

## Conséquences

L'API de validation peut être décrite honnêtement comme `HOST:UNKNOWN` et
`DOCKER:MATCHED`. L'inventaire, le cache, le daemon et l'exécution Docker restent
hors périmètre. Le contrat JSON v1 gagne un champ additif `environment`, et les
reporters affichent séparément les totaux HOST et DOCKER.
