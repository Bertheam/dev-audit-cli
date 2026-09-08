# ADR 0019 — Rendre la sortie terminal human-first et accessible

- Statut : accepté
- Date : 2026-09-08

## Contexte

Le premier rendu terminal exposait presque chaque champ du domaine sous forme
de clés et valeurs. Il était déterministe et complet, mais demandait trop
d'effort pour répondre aux questions immédiates : quels problèmes exigent mon
attention, quelles ressources occupent le plus d'espace et quelle commande
dois-je utiliser ensuite ? `explain` et `plan` possédaient en outre des
hiérarchies différentes.

Le rendu humain n'est pas le contrat d'automatisation. Le JSON v1 remplit ce
rôle et doit rester exhaustif, stable et exempt de décoration terminal.

## Décision

`scan`, `explain` et `plan` partagent une petite couche de style interne et le
même vocabulaire visuel. Chaque vue commence par `◆ dev-audit`, sa commande et
son mode de sécurité. Les sections, symboles et libellés textuels portent la
hiérarchie ; la couleur n'est jamais nécessaire pour comprendre un statut.

Le scan est concis par défaut : synthèse, environnements, 12 projets, 12 plus
grosses ressources, diagnostics `WARNING`/`ERROR` et prochaines commandes. Le
mode `--verbose` affiche les racines, chaque projet et exigence, toutes les
ressources et les diagnostics informatifs. Il ne relance pas le scan et ne
modifie aucune donnée.

Les commandes humaines acceptent `--color auto|always|never`. Le mode `auto`
n'active les séquences ANSI que si stdout est un terminal interactif. Il les
désactive pour une sortie fichier, un pipe, `NO_COLOR` ou `TERM=dumb`.
`always` et `never` sont des choix explicites. Le JSON n'est jamais coloré,
même avec `--color always`.

Les chaînes observées restent échappées avant stylisation. Les tris restent
déterministes et le rendu compact possède un golden test. Les tests vérifient
également le mode détaillé, la présence explicite d'ANSI et son absence dans
les sorties plain-text et JSON.

## Conséquences

La première lecture devient plus courte et orientée vers la décision, tandis
que les détails restent disponibles sans modifier le scan. Le rendu terminal
peut continuer à évoluer comme interface humaine ; les intégrations doivent
consommer le JSON v1 plutôt que parser son texte.

Le comptage de ressources affiché dans l'en-tête peut être supérieur au nombre
de lignes visibles. Le rapport l'indique et propose `--verbose`, ce qui évite
de cacher silencieusement des observations.

## Références

- Command Line Interface Guidelines : <https://clig.dev/>
- convention `NO_COLOR` : <https://no-color.org/>
- variables d'environnement de GitHub CLI :
  <https://cli.github.com/manual/gh_help_environment>
