# ADR 0015 — Classifier les ressources sans recommander leur suppression

- Statut : accepté
- Date : 2026-09-05

## Contexte

La corrélation sait dire qu'une ressource satisfait une exigence ou qu'aucune
référence n'a été trouvée dans une couverture donnée. Elle ne prouve pas à elle
seule qu'une ressource est utilisée aujourd'hui, ancienne, reconstructible ou
supprimable. Les tailles de fichiers, d'images et de caches utilisent aussi des
métriques différentes et parfois partagées.

Une classification utile doit donc rester explicable, accepter plusieurs
dimensions simultanées et préserver l'inconnu lorsqu'une preuve manque.

## Décision

Chaque ressource d'un nouveau scan reçoit une ou plusieurs classifications
parmi :

- `UTILISEE` : une exigence projet est corrélée à la ressource, ou Docker
  observe un conteneur dans un état actif ;
- `RECONSTRUCTIBLE` : une source allowlistée expose un moyen de reconstruire la
  ressource, actuellement `sdkmanager`, FVM, Gradle ou BuildKit ;
- `ANCIENNE` : une preuve de dernière utilisation dépasse le seuil configuré ;
- `ORPHELINE_PROBABLE` : la ressource est sans référence dans une couverture
  complète, reconstructible, ancienne et non sensible ;
- `SENSIBLE` : l'inventaire a identifié des données mutables ou utilisateur ;
- `INCONNUE` : aucune preuve suffisante n'établit l'usage ni le statut
  d'orpheline probable.

Chaque classification contient un texte de justification et au moins une
preuve structurée. Les invariants du domaine rejettent les combinaisons
contradictoires, notamment `UTILISEE` avec `INCONNUE`, ou
`ORPHELINE_PROBABLE` avec `SENSIBLE`.

Le seuil par défaut est 180 jours et l'option `--old-after-days` permet de le
modifier. La valeur est conservée dans `scan.classification_policy`. Une date
de création ou le `mtime` d'un chemin ne sont jamais traités comme une dernière
utilisation. Dans ce lot, seule la valeur `LastUsedAt` issue de la commande
allowlistée `docker buildx du --format=json` est acceptée. Les versions récentes
la rendent en RFC 3339 ; certains plugins plus anciens renvoient une durée
relative. Dans ce second cas, le moteur conserve la valeur et utilise une borne
basse prudente, sans reconstruire une fausse date exacte.

`size_bytes` reste la mesure observée propre à l'adaptateur. Un objet séparé
`potentially_reclaimable` contient des octets, une justification et ses preuves.
Il n'est produit que pour un cache BuildKit marqué `Reclaimable=true`,
`Shared=false` et `Mutable=false`, avec une taille connue. Aucun total garanti
n'est calculé.

Les nouveaux champs du JSON v1 sont additifs et optionnels à la lecture afin de
conserver les rapports antérieurs. Tous les nouveaux scans les produisent.

## Conséquences

`NO_REFERENCE_FOUND` seul reste insuffisant et se combine avec `INCONNUE` tant
que l'ancienneté ou la reconstruction n'est pas prouvée. Une ressource peut être
à la fois reconstructible, ancienne et d'usage inconnu sans être présentée comme
orpheline.

Le rapport terminal sépare `observed_size` et `potentially_reclaimable`.
`dev-audit explain` affiche les raisons et les preuves de chaque catégorie et de
l'estimation éventuelle. Aucune commande de nettoyage n'est créée ou exécutée.

## Références

- Cache BuildKit et champs `LastUsedAt`, `Reclaimable`, `Shared`, `Mutable` :
  <https://docs.docker.com/reference/cli/docker/buildx/du/>
- Contrat de corrélation prudente :
  [`0008-conservative-correlation-and-coverage.md`](0008-conservative-correlation-and-coverage.md)
- Inventaire Docker en lecture seule :
  [`0013-read-only-docker-daemon-inventory.md`](0013-read-only-docker-daemon-inventory.md)
