# Validation des actions ciblées — Mac 01

- Date : 2026-09-08
- Version : `0.3.0-dev`
- Mode : scan réel puis plan `SIMULATION_ONLY`

## Inventaire

Le scan zéro-configuration termine sans diagnostic `WARNING` ou `ERROR` :

- 28 projets ;
- 86 ressources installées ;
- 15 paquets Android associés au chemin absolu de la nouvelle CLI `android` ;
- 4 distributions Gradle Wrapper portant `targeted_directory_removal` ;
- 4 entrées Xcode DerivedData portant `targeted_directory_removal`.

## Plan mixte

Une plateforme Android référencée, une distribution Gradle référencée et une
entrée DerivedData d'usage inconnu ont été sélectionnées manuellement. Le plan a
produit :

```json
["/Users/bertham/Library/Android/sdk/cmdline-tools/latest/bin/android", "sdk", "remove", "platforms;android-36"]
["/bin/rm", "-R", "/Users/bertham/.gradle/wrapper/dists/gradle-8.12-all/ejduaidbjup3bmmkhw3rie4zb"]
["/bin/rm", "-R", "/Users/bertham/Library/Developer/Xcode/DerivedData/SDKStatCaches.noindex"]
```

Le rendu signale correctement `UTILISEE` pour Android et Gradle, `INCONNUE`
pour DerivedData, les projets affectés, les impacts, les avertissements et la
confirmation obligatoire. Aucune commande du plan n'a été exécutée.

## Gates

- `go test -race ./...` : réussi ;
- `go vet ./...` : réussi ;
- installation dans `/opt/homebrew/bin` : réussie ;
- reproduction du même `plan_id` par le binaire installé : réussie.
