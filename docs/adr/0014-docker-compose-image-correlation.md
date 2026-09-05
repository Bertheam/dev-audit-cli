# ADR 0014 — Corréler les images Docker déclarées et locales

- Statut : accepté
- Date : 2026-09-05

## Contexte

L'inventaire du daemon sait observer les images locales, mais ne sait pas
les rattacher aux projets. Inversement, l'analyse JDK des `Dockerfile` décrit un
environnement de build sans dire si l'image correspondante est présente. Les
projets uniquement Docker et les images déclarées dans Compose n'étaient pas
encore découverts.

Une même relation ne peut pas représenter honnêtement ces deux faits :

- `DOCKER` signifie qu'une toolchain est déclarée statiquement dans un fichier ;
- la présence d'une image est un état global du daemon courant.

## Décision

Discovery reconnaît `Dockerfile`, `Dockerfile.*`, `compose.yaml`, `compose.yml`,
`docker-compose.yaml` et `docker-compose.yml`. Un marqueur Docker sous un projet
Flutter ou Android enrichit ce projet. Un répertoire extérieur devient un
projet `DOCKER`; les Dockerfiles imbriqués sous une racine Compose sont repliés
dans cette racine. Les arbres `vendor` et `node_modules` sont exclus.

L'analyse statique bornée produit une exigence explicite `docker/image` pour :

- chaque image externe littérale d'une instruction `FROM` ;
- chaque valeur scalaire littérale de `services.*.image` dans le sous-ensemble
  Compose pris en charge.

Les preuves conservent le fichier, la ligne, la clé et la valeur observée. Les
déclarations identiques d'un projet sont dédupliquées tout en agrégeant leurs
preuves. `scratch`, les alias de stages multi-stage, les interpolations, les
alias YAML, les blocs et les mappings inline ne produisent pas d'exigence
inventée. Les formes non statiques ou non prises en charge sont diagnostiquées
en `INFO`.

La corrélation utilise un troisième environnement : `DOCKER_DAEMON`. Elle
normalise le registre Docker Hub implicite, le namespace `library` et le tag
`latest`, puis compare l'exigence aux métadonnées `reference` et `digest` des
ressources `docker_image`.

- `DOCKER_DAEMON:MATCHED` pointe vers l'unique image locale correspondante.
- `DOCKER_DAEMON:AMBIGUOUS` protège plusieurs candidats possibles.
- `DOCKER_DAEMON:MISSING` n'est autorisé que si `docker image ls` a terminé sans
  timeout, troncature, budget atteint ou ligne malformée.
- Sinon, la relation reste `UNKNOWN`.

Le contrat JSON v1 reçoit les valeurs additives `DOCKER` pour `Project.kind` et
`DOCKER_DAEMON` pour `Relation.environment`. L'interdiction de `resource_id`
reste propre à `DOCKER`; une relation `DOCKER_DAEMON` peut référencer une
ressource locale Docker.

## Conséquences

Les images Node, Python, PHP, bases de données et autres toolchains non-Java
sont visibles sans les transformer en installations hôte. La CLI peut expliquer
qu'une image déclarée est présente ou absente du daemon sans effectuer de
`pull`, de build ou de lancement.

Le parseur Compose est volontairement un sous-ensemble statique et non un
moteur YAML complet. Il privilégie une absence d'interprétation explicable à
une résolution implicite d'ancres, de variables ou de fichiers fusionnés.

## Références

- Modèle et noms de fichiers Compose :
  <https://docs.docker.com/compose/intro/compose-application-model/>
- Attribut `services.*.image` :
  <https://docs.docker.com/reference/compose-file/services/#image>
- Instruction `FROM` :
  <https://docs.docker.com/reference/dockerfile/#from>
