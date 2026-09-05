# Validation Xcode/iOS — Mac 01

- Date : 2026-09-05
- Version : `0.3.0-dev`
- Nature : gate réel après installation complète du runtime iOS 26.5

## Exécution

`dev-audit scan` a été lancé sans aucune option de chemin et avec le timeout de
30 secondes par défaut. Le rapport JSON est resté local et n'est pas versionné,
car il contient des chemins absolus.

Le passage final a terminé sans diagnostic `WARNING` ou `ERROR`. Il contient 27
projets, 102 ressources locales et 84 diagnostics `INFO`. Ces diagnostics
décrivent principalement les racines détectées et les limites honnêtes de
couverture ; ils n'empêchent pas le rapport.

## Inventaire Apple

Les 39 ressources Apple se répartissent ainsi :

- 1 installation Xcode, version 26.6 ;
- 1 installation Command Line Tools ;
- 8 runtimes iOS, dont iOS 26.5 ;
- 11 appareils simulés iOS 26.5 ;
- 17 entrées DerivedData ;
- 1 entrée sous la zone de composants Packages, conservée sensible et inconnue.

Les 11 appareils représentent environ 2,3 Gio de fichiers logiques, dont un
appareil ayant reçu des données de travail. DerivedData représente environ 1,1
Gio. Ces valeurs sont des observations, pas des estimations garanties d'espace
récupérable.

Les runtimes montés dépassent chacun le budget initial de 500 000 entrées. Le
scanner a été corrigé pour ne plus parcourir ces arbres à forte cardinalité :
ils sont toujours inventoriés, mais leur taille porte le statut explicite
`skipped_high_cardinality_runtime_tree`. Le second passage termine ainsi avec
le timeout par défaut et sans avertissement de mesure incomplète.

## Comparaison CoreSimulator

Après le scan, deux commandes officielles de liste ont été exécutées séparément
du produit, uniquement pour établir la vérité terrain :

- `xcrun simctl list runtimes --json` : 8 runtimes, tous disponibles ;
- `xcrun simctl list devices --json` : 11 appareils, tous disponibles, aucun
  dans l'état `Booted`.

Les nombres et versions correspondent exactement à l'inventaire filesystem de
`dev-audit`. Aucune commande de création, démarrage, arrêt ou suppression n'a
été utilisée.

## Conclusion

Le gate Xcode/iOS est validé pour ce Mac. Les appareils restent
`SENSIBLE + INCONNUE`, les DerivedData `RECONSTRUCTIBLE + INCONNUE`, et aucune
ressource Apple ne possède d'action de nettoyage dans le plan simulé.
