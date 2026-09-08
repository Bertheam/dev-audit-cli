# ADR 0020 — Afficher la progression sans polluer les rapports

- Statut : accepté
- Date : 2026-09-08

## Contexte

Un scan réel dure environ 15 secondes sur le Mac de validation. Sans retour
visuel, l'utilisateur ne peut pas distinguer un inventaire actif d'une commande
bloquée. Un pourcentage serait pourtant trompeur : le nombre de projets,
fichiers, SDK et caches n'est connu qu'après leur parcours.

La sortie standard peut contenir le contrat JSON v1. Toute animation ou phrase
ajoutée à ce flux casserait les pipes et les intégrations.

## Décision

La progression décrit des étapes réelles et un temps écoulé, sans pourcentage.
L'orchestrateur accepte un callback facultatif et publie les transitions entre
découverte, analyse, inventaires, Docker, corrélation, classification et rendu.
Le callback ne modifie aucune donnée et n'appartient pas au contrat de domaine.

La CLI affiche la progression exclusivement sur `stderr`. Les trois commandes
humaines acceptent `--progress auto|always|never` :

- `auto` anime seulement lorsque `stderr` est un TTY et que `TERM` n'est pas
  `dumb` ;
- `always` force la progression, mais utilise des lignes sans ANSI lorsque la
  destination n'est pas interactive ;
- `never` ne produit aucune progression.

Le spinner attend 100 ms avant son premier rendu afin que les commandes rapides
ne clignotent pas. Son arrêt rejoint la goroutine avant d'afficher le rapport.
Les couleurs suivent la politique `--color` et `NO_COLOR`, mais la progression
elle-même reste disponible sans couleur.

## Conséquences

L'utilisateur voit quelle famille consomme du temps sans recevoir une fausse
estimation. Un JSON redirigé depuis `stdout` reste strictement valide. Les logs
peuvent demander explicitement une trace lisible de chaque étape.

Le callback ajoute une préoccupation de présentation optionnelle à la
configuration applicative. Il reste synchrone, minimal et sans dépendance pour
éviter de coupler le moteur au terminal.
