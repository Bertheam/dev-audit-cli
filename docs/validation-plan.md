# Plan de validation terrain

## Objectif

Déterminer si la carte projet–toolchain est plus fiable et utile qu'un simple
inventaire de stockage, sans effectuer de changement sur les machines testées.

## Échantillon

Recruter 10 à 15 développeurs macOS possédant plusieurs projets Flutter/Android,
des versions historiques et des configurations Groovy ou Kotlin DSL variées.

## Préparation

1. Obtenir un consentement explicite et annoncer les zones conventionnelles que
   l'auto-détection locale peut parcourir.
2. Noter versions macOS, architecture, organisation Android SDK et usage de FVM.
3. Constituer avec le testeur une vérité terrain minimale : projets conservés,
   versions explicitement connues et toolchains installées.
4. Expliquer que le rapport ne formule aucune recommandation de suppression.

## Protocole

1. Exécuter d'abord le même binaire sans racines, puis faire confirmer les
   chemins détectés par le testeur ; utiliser les options explicites pour
   corriger ou reproduire le périmètre si nécessaire.
2. Conserver le rapport JSON localement ou le pseudonymiser avant partage.
3. Vérifier manuellement chaque association importante avec le testeur.
4. Qualifier faux positifs, faux négatifs, ambiguïtés et diagnostics incompris.
5. Demander si le rapport apporte plus de confiance qu'une liste de tailles.
6. Confirmer que projets, configurations et toolchains n'ont pas été modifiés.

## Mesures

- rappel des exigences explicitement déclarées correctement détectées ;
- précision des associations présentées comme explicites ;
- part des conclusions importantes dont la preuve est retrouvable ;
- nombre et nature des `UNKNOWN` et diagnostics ;
- durée du scan et volume de fichiers lus ;
- au moins 7 testeurs sur 10 jugeant le rapport plus utile qu'un inventaire ;
- demandes spontanées de carte interactive, snapshot ou étape suivante.

## Gate BSF

Poursuivre uniquement si aucune ressource indispensable n'est présentée comme
sûre à supprimer, si au moins 80 % des exigences explicites sont associées et si
les preuves sont jugées compréhensibles. Sinon : PIVOT ou STOP.

## Confidentialité

Aucun rapport brut ne quitte une machine sans accord. Les chemins peuvent
contenir des noms personnels ou clients ; ils doivent être pseudonymisés avant
toute centralisation.
