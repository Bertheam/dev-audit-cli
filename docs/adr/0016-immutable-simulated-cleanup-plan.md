# ADR 0016 — Figer un plan de nettoyage simulé avant toute exécution

- Statut : accepté
- Date : 2026-09-05

## Contexte

La classification explique l'usage connu, la reconstruction, l'ancienneté, la
sensibilité et l'incertitude. Elle ne constitue pas encore une décision de
nettoyage. Montrer directement une commande à partir d'une taille ou d'une
absence de référence créerait un raccourci dangereux, notamment pour les AVD,
conteneurs et ressources dont la couverture reste heuristique.

Une future primitive d'exécution doit aussi être incapable de recalculer une
sélection différente de celle relue par l'utilisateur. Le Lot 10 doit donc
produire un artefact autonome, déterministe et vérifiable, tout en restant
strictement sans exécution.

## Décision

La commande `dev-audit plan --report FILE` lit exclusivement un rapport JSON v1
validé. Elle émet un `CleanupPlan` au mode constant `SIMULATION_ONLY` et ne
possède aucun chemin de code lançant les commandes décrites.

La sélection `DEFAULT_SAFE` exige simultanément :

- la catégorie `ORPHELINE_PROBABLE` ;
- une estimation `potentially_reclaimable` ;
- l'absence de `SENSIBLE`, `INCONNUE` et `UTILISEE` ;
- une action officielle ciblée prise en charge.

Toute ressource non retenue apparaît dans `excluded` avec des raisons
structurées. `--resource ID`, répétable, active `EXPLICIT_RESOURCE_IDS` : une
ressource risquée peut alors être incluse pour examen, avec ses catégories et
des avertissements supplémentaires. Cette sélection manuelle n'est jamais une
confirmation d'exécution. Une ressource absente ou sans action prise en charge
reste exclue.

Chaque élément contient son impact, sa commande sous forme de tableau
d'arguments, les projets affectés, les risques, les preuves, les avertissements,
la taille observée et l'estimation éventuelle. Toutes les actions exigent une
confirmation explicite dans le contrat. Aucun shell, `docker system prune`,
`--force` ou effacement direct de chemin n'est produit.

Le plan contient le SHA-256 des octets exacts du rapport source. Son `plan_id`
est le SHA-256 de tous ses champs, identifiant exclu. Toute modification rend le
plan invalide au décodage. Une sortie fichier est créée avec `O_EXCL` et le mode
`0600` ; un chemin existant n'est jamais écrasé.

Les actions ciblées du Lot 10 sont :

- `docker buildx prune --filter id=<ID>` pour un enregistrement BuildKit ;
- `docker image rm <ID>` pour une image ;
- `docker container rm <ID>` pour un conteneur, sans suppression de volume ni
  forçage ;
- `sdkmanager --uninstall <package>` pour un paquet Android ;
- `avdmanager delete avd -n <name>` pour un AVD, avec avertissement de
  sensibilité et de dépréciation actuelle de l'outil.

## Conséquences

Un plan automatique vide est un résultat normal lorsque rien n'est prouvé
orphelin probable. Le mode manuel rend néanmoins les actions et impacts
inspectables sans abaisser la politique automatique.

La somme `known_potentially_reclaimable_bytes_sum` porte uniquement sur les
estimations connues des éléments sélectionnés. Elle est affichée comme une
estimation non garantie, car des couches ou blocs peuvent être partagés.

Une future fonctionnalité d'exécution devra relire et valider exactement cet
artefact, demander les confirmations prévues et refuser toute dérive du rapport
ou du plan. Cette fonctionnalité n'est pas autorisée ni implémentée ici.

## Références

- Filtre ciblé BuildKit : <https://docs.docker.com/reference/cli/docker/buildx/prune/>
- Désinstallation `sdkmanager` : <https://developer.android.com/tools/sdkmanager>
- Suppression d'un AVD : <https://developer.android.com/tools/avdmanager>
- Classification conservatrice :
  [`0015-explainable-resource-classification.md`](0015-explainable-resource-classification.md)
