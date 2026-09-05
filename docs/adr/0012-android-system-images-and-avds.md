# ADR 0012 — Images système Android et AVD sensibles

- Statut : accepté
- Date : 2026-09-05

## Contexte

Le MVP inventorie les plateformes, Build Tools, NDK et CMake du SDK Android,
mais ignore deux postes de stockage importants : les images système et les
Android Virtual Devices. Une image système est un paquet géré par le SDK. Un AVD
est différent : son dossier contient notamment des disques de données, un cache
et éventuellement une carte SD modifiée par l'utilisateur ou les applications.

La taille seule ne permet donc pas d'appliquer la même recommandation aux deux
types de ressources.

## Décision

L'inventaire du SDK parcourt la structure allowlistée
`system-images/android-<api>/<tag>/<abi>`. Une image devient une ressource
`android/android_system_image` avec :

- API, tag et ABI ;
- identifiant de paquet `system-images;<api>;<tag>;<abi>` ;
- gestionnaire officiel `sdkmanager` ;
- taille logique bornée selon les règles existantes.

L'inventaire AVD reçoit une racine typée distincte. L'auto-détection essaie :

1. `ANDROID_AVD_HOME` ;
2. le sous-dossier `avd` de `ANDROID_USER_HOME` ;
3. le sous-dossier `avd` de `ANDROID_EMULATOR_HOME` pour compatibilité ;
4. `~/.android/avd` ;
5. les racines non standards reconnues par la recherche profonde bornée.

Seuls les enfants directs `*.avd` contenant un fichier régulier `config.ini`
sont inventoriés. Les pointeurs `.ini` et les symlinks ne sont pas suivis. Les
valeurs exportées sont limitées à une allowlist non sensible : identifiant AVD,
cible Android, tag, ABI et modèle matériel lorsqu'ils respectent le format sûr.

Chaque AVD devient une ressource `android/android_avd`, porte la métadonnée
`sensitive_mutable_user_data`, conserve `reference_status: UNKNOWN` et avertit
qu'aucun nettoyage n'est impliqué. `avdmanager` est indiqué comme gestionnaire,
mais n'est jamais exécuté.

## Conséquences

Le rapport rend visibles deux postes de stockage Android jusque-là absents sans
les confondre. L'identifiant de paquet rend l'origine d'une image système
explicable. En revanche, aucun signal de dernière utilisation ni aucune
association de projet n'est encore déduit pour un AVD ou une image système.

`size_bytes` reste une taille logique occupée, pas une estimation de l'espace
récupérable. Une future classification devra exclure les AVD des actions
automatiques et exiger une preuve supplémentaire pour toute proposition.

## Références

- Variables d'environnement Android :
  <https://developer.android.com/tools/variables>
- Emplacement et contenu des données AVD :
  <https://developer.android.com/studio/run/emulator-commandline>
- Gestion des AVD :
  <https://developer.android.com/studio/run/managing-avds>
