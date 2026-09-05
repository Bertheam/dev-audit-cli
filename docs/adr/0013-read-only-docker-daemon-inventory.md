# ADR 0013 — Inventaire borné du daemon Docker

- Statut : accepté
- Date : 2026-09-05

## Contexte

La lecture statique des `Dockerfile` indique ce qu'un projet déclare, mais ne
dit pas quelles images, quels conteneurs ou quels caches occupent réellement le
stockage Docker local. Le daemon est une source d'état différente du système de
fichiers hôte et peut être absent, arrêté ou inaccessible.

Les commandes Docker peuvent aussi exposer des champs sensibles et certaines
tailles se recouvrent à cause du partage de couches.

## Décision

Un module `internal/dockerinventory` séparé utilise uniquement la CLI `docker`
et une allowlist de cinq lectures :

- `docker version` pour distinguer client et daemon ;
- `docker image ls` pour les identifiants, références, dates et tailles ;
- `docker container ls --all --size` pour l'état et la couche inscriptible ;
- `docker buildx ls` pour les builders et leurs nœuds ;
- `docker buildx du` pour les enregistrements de cache.

Aucun shell n'est invoqué. Chaque processus reçoit un contexte limité à cinq
secondes par défaut, la sortie combinée est bornée à 8 Mio et le résultat à
2 000 ressources. Le timeout global de `scan` reste prioritaire. Le module ne
possède aucune commande de mutation.

Les templates de sortie demandent une allowlist de champs. Les commandes,
labels, variables d'environnement, montages, descriptions BuildKit et contenus
des conteneurs ne sont pas collectés. Les erreurs brutes de Docker ne sont pas
recopiées dans le rapport.

Les ressources utilisent les composants `docker_image`, `docker_container`,
`docker_builder` et `docker_build_cache`, avec un localisateur `docker://` dans
le champ historique `path`. Les images peuvent devenir `REFERENCED` lorsqu'une
exigence statique de projet leur correspond. Les autres ressources Docker
restent `UNKNOWN`; une présence locale ne prouve pas à elle seule qu'un projet
les utilise.

La taille d'une image est sa taille virtuelle et peut partager des couches. La
taille d'un conteneur ne couvre que sa couche inscriptible. BuildKit fournit un
signal `Reclaimable`, conservé comme métadonnée accompagnée d'un avertissement
qui interdit de le transformer en autorisation de suppression.

Docker absent ou daemon indisponible produit un diagnostic `INFO` et ne bloque
pas le reste du scan. L'option `--docker-inventory=false` désactive entièrement
ces appels.

La corrélation ajoutée par l'ADR 0014 utilise l'environnement
`DOCKER_DAEMON`, distinct de la déclaration de toolchain `DOCKER`. La commande
`docker image ls` ne déclare son type complètement couvert que si elle réussit
sans timeout, limite de sortie, limite de ressources ou donnée malformée.

## Compatibilité

La documentation Docker courante expose une option `--timeout` sur certaines
commandes Buildx, mais le plugin présent sur le Mac 01 ne la connaît pas. Le
module n'utilise donc pas cette option et impose sa borne avec
`exec.CommandContext`, ce qui fonctionne aussi avec les versions plus anciennes.

## Références

- Espace utilisé par le daemon :
  <https://docs.docker.com/reference/cli/docker/system/df/>
- Liste et sémantique de taille des images :
  <https://docs.docker.com/reference/cli/docker/image/ls/>
- Liste et taille des couches inscriptibles des conteneurs :
  <https://docs.docker.com/reference/cli/docker/container/ls/>
- Builders Buildx :
  <https://docs.docker.com/reference/cli/docker/buildx/ls/>
- Cache BuildKit et signal reclaimable :
  <https://docs.docker.com/reference/cli/docker/buildx/du/>
