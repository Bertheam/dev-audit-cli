# Dev Storage Manager - Dossier produit BSF

> **Statut :** cadrage initial, avant validation utilisateur et choix technique
> **Version :** 0.1
> **Date :** 2 septembre 2026
> **Nom du produit :** ouvert (`Dev Storage Manager` est un nom de travail)
> **Cadre :** Berthe Startup Framework (BSF)

---

## 0. Objet du document

Ce document est la source de vérité initiale du projet. Il décrit le problème,
la proposition de valeur, le premier périmètre, les exigences de sécurité et
les hypothèses à vérifier avant d'écrire une application complète.

### Règles de lecture

- **VALIDÉ** : décision actuelle à respecter.
- **RECOMMANDÉ** : meilleure direction actuelle, modifiable avec justification.
- **HYPOTHÈSE** : proposition à tester avant de la considérer comme vraie.
- **OUVERT** : décision qui reste à prendre.
- Une fonctionnalité V2 ne doit pas être intégrée prématurément au MVP.

---

# Résumé exécutif

Les environnements de développement accumulent des dizaines de gigaoctets de
builds, caches, SDK, NDK, simulateurs, images Docker et versions d'outils. Les
solutions existantes savent généralement montrer de gros dossiers ou exécuter
des commandes de nettoyage. Elles expliquent rarement quels projets utilisent
encore chaque ressource et quel sera l'impact réel d'une suppression.

Dev Storage Manager doit devenir un **gestionnaire de stockage conscient des
projets**. Il analyse les projets locaux, relie leurs configurations aux
ressources installées, puis classe chaque élément comme utilisé, reconstructible,
ancien, orphelin ou sensible. Il propose un plan vérifiable avant toute action.

Le produit ne doit pas être un nouveau nettoyeur agressif. Sa valeur principale
est la réponse fiable à trois questions :

1. Qu'est-ce qui occupe réellement l'espace ?
2. Quel projet utilise encore cette ressource ?
3. Que se passera-t-il précisément si elle est supprimée ?

---

# Registre des décisions

| Sujet | Décision | Statut |
|---|---|---|
| Positionnement | Gestionnaire intelligent du stockage développeur, conscient des projets | VALIDÉ |
| Plateforme initiale | macOS | RECOMMANDÉ |
| Écosystèmes MVP | Flutter, Android, Gradle et Docker | VALIDÉ |
| Mode initial | Audit et simulation avant nettoyage | VALIDÉ |
| Suppression automatique globale | Interdite dans le MVP | VALIDÉ |
| Données sensibles Docker | Volumes, bases et données métier protégés par défaut | VALIDÉ |
| Xcode et simulateurs | Audit initial, nettoyage complet dans une étape ultérieure | RECOMMANDÉ |
| Architecture | Application locale monolithique avec moteur réutilisable et CLI | RECOMMANDÉ |
| Langage du moteur | À choisir après prototype comparatif | OUVERT |
| Interface desktop | Après validation du moteur d'audit | VALIDÉ |
| Nom commercial | À étudier | OUVERT |
| Modèle économique | Cœur d'audit gratuit, fonctions avancées éventuellement payantes | HYPOTHÈSE |

---

# BSF 1 - Problème à résoudre

## 1.1 Situation observée

Un poste Flutter et Android peut conserver simultanément :

- plusieurs SDK Flutter ;
- plusieurs distributions Gradle et caches associés ;
- plusieurs versions de SDK, NDK, CMake et images système Android ;
- des AVD et données d'émulateurs inutilisés ;
- les DerivedData, runtimes et simulateurs Xcode ;
- des builds Flutter propres à chaque projet ;
- des images, couches, builders et caches Docker ;
- des dépendances téléchargées par plusieurs gestionnaires de paquets.

Chaque outil gère une partie de ce stockage avec ses propres commandes et ses
propres règles de rétention. Le développeur ne dispose pas d'une vue fiable et
unifiée de leurs dépendances.

## 1.2 Problèmes des approches actuelles

- Le stockage macOS classe souvent ces éléments dans « Données système ».
- Un outil d'analyse de disque montre la taille, mais pas l'usage métier.
- Un nettoyeur générique reconnaît des chemins connus, mais pas les versions
  encore référencées par les projets.
- Une suppression manuelle peut provoquer de longs retéléchargements ou casser
  un build urgent.
- Les commandes Docker les plus larges peuvent supprimer des données durables.
- Les caches sont supprimés en urgence lorsque le disque est déjà presque plein.
- Les équipes et agents automatisés reconstruisent des artefacts sans politique
  commune de rétention.

## 1.3 Problème central

> Les développeurs peuvent trouver les fichiers volumineux, mais ils ne savent
> pas facilement lesquels sont encore nécessaires, pourquoi ils existent et
> quelles suppressions sont réellement sûres.

## 1.4 Opportunité

Construire une couche de compréhension entre les projets, leurs outils et les
ressources locales. Le produit doit prévenir l'accumulation, pas seulement
intervenir après saturation du disque.

---

# BSF 2 - Utilisateurs

## 2.1 Cible initiale

Développeurs macOS travaillant avec Flutter, Android, iOS et Docker sur plusieurs
projets ou pour plusieurs clients.

## 2.2 Segments

### Développeur mobile indépendant

- Passe régulièrement d'un ancien projet à un projet récent.
- Conserve plusieurs toolchains par prudence.
- Veut récupérer de l'espace sans perdre une journée à réparer son environnement.

### Petite équipe produit

- Veut une politique commune de versions et de nettoyage.
- Utilise des agents ou scripts qui peuvent multiplier les builds.
- A besoin d'un diagnostic reproductible entre les postes.

### Mainteneur ou consultant

- Possède de nombreux projets rarement ouverts.
- Ne sait pas quelles anciennes versions restent réellement nécessaires.
- Veut archiver proprement un environnement avant nettoyage.

---

# BSF 3 - Proposition de valeur

## 3.1 Promesse

> Récupérer de l'espace sans deviner et sans casser ses projets.

## 3.2 Différenciation

Le produit ne se contente pas de scanner des dossiers. Il construit une carte :

```text
Projet -> configuration déclarée -> outil/version -> fichiers locaux -> taille
```

Exemples attendus :

- `NDK 28.2` : utilisé par trois projets, conserver.
- `NDK 27.0` : aucune référence trouvée, suppression proposée.
- `Gradle 8.12` : utilisé par un projet archivé, afficher l'impact.
- `Docker build cache` : reconstructible, 18 Go, limiter à 8 Go.
- `Volume PostgreSQL` : données persistantes, ne jamais sélectionner par défaut.

## 3.3 Principes produit

- La preuve avant la suppression.
- La prévention avant le nettoyage d'urgence.
- L'impact utilisateur avant le nombre de gigaoctets récupérables.
- Les commandes officielles avant les suppressions directes de dossiers.
- Le mode lecture seule comme première expérience.
- Aucun jargon non expliqué dans l'interface principale.

---

# BSF 4 - MVP

## 4.1 Objectif du MVP

Prouver que l'outil peut analyser une machine de développement réelle et
produire un plan plus fiable qu'un nettoyeur basé uniquement sur les chemins et
dates de fichiers.

## 4.2 Périmètre obligatoire

### Découverte des projets

- Sélection d'un ou plusieurs dossiers racines autorisés par l'utilisateur.
- Détection des projets Flutter, Android et Docker.
- Respect des exclusions configurables : archives, sauvegardes, volumes montés,
  dossiers système et chemins sensibles.
- Aucun envoi des chemins, noms de projets ou configurations vers un serveur.

### Détection des versions

- Flutter et Dart déclarés par FVM ou par les métadonnées du projet.
- Gradle Wrapper, Android Gradle Plugin et Kotlin.
- `compileSdk`, `targetSdk`, `minSdk`, NDK et CMake.
- Images système Android et AVD installés.
- Builders Docker, cache BuildKit, images et conteneurs.

### Inventaire du stockage

- Taille réelle et taille potentiellement récupérable.
- Dernière utilisation lorsqu'elle est déterminable de manière fiable.
- Origine de la ressource et commande officielle de gestion.
- Liste des projets qui référencent explicitement la ressource.
- Signalement clair lorsque l'usage ne peut pas être déterminé.

### Classification

Chaque ressource reçoit une catégorie explicable :

- **Utilisée** : au moins un projet détecté la référence.
- **Reconstructible** : build ou cache pouvant être régénéré.
- **Ancienne** : inactive selon une politique configurable.
- **Orpheline probable** : aucune référence détectée, avec niveau de confiance.
- **Sensible** : peut contenir des données, secrets ou artefacts irremplaçables.
- **Inconnue** : l'outil ne possède pas assez de preuves pour recommander une action.

### Plan de nettoyage

- Simulation obligatoire avec détail élément par élément.
- Estimation de l'espace récupérable avant validation.
- Conséquence affichée : retéléchargement, reconstruction, perte potentielle ou
  absence d'impact connu.
- Sélection manuelle des actions.
- Résumé final et seconde confirmation pour toute opération sensible autorisée.
- Journal local des commandes, résultats et tailles récupérées.

### Exécution sûre

- `flutter clean` exécuté projet par projet après validation.
- `sdkmanager --uninstall` pour les composants Android gérés.
- API ou commandes Docker ciblées, jamais `docker system prune` par défaut.
- `docker buildx prune` borné par une politique de stockage.
- Arrêt si un build, émulateur ou outil concerné utilise activement la ressource.
- Échec fermé : en cas de doute ou de commande partiellement réussie, aucune
  action suivante n'est enchaînée silencieusement.

## 4.3 Hors périmètre du MVP

- Nettoyage automatique sans validation.
- Suppression des volumes Docker et bases locales.
- Modification automatique des versions de projets.
- Migration automatique Gradle, AGP, Kotlin, Flutter ou NDK.
- Nettoyage général des documents personnels.
- Optimisation de macOS ou promesse d'accélération magique.
- Windows et Linux.
- Tableau de bord cloud ou compte obligatoire.
- Agents autonomes exécutant des suppressions en arrière-plan.

---

# BSF 5 - Expérience utilisateur

## 5.1 Parcours principal

1. L'utilisateur choisit les dossiers de projets à analyser.
2. L'application présente l'espace par écosystème.
3. Elle indique les versions utilisées et les ressources sans référence connue.
4. L'utilisateur ouvre une recommandation et consulte ses preuves.
5. Il ajoute des éléments à un plan de nettoyage.
6. Une simulation affiche les commandes et conséquences.
7. L'utilisateur confirme.
8. L'application exécute les actions une par une et produit un rapport.

## 5.2 Écrans du futur client desktop

- **Vue d'ensemble** : espace utilisé, récupérable et protégé.
- **Projets** : toolchains et ressources associées à chaque projet.
- **Android** : SDK, NDK, images système, AVD et Gradle.
- **Docker** : cache de build, images, conteneurs et volumes protégés.
- **Flutter** : SDK installés, projets associés et builds locaux.
- **Plan** : actions proposées, risques, commandes et estimation.
- **Historique** : opérations exécutées et espace réellement récupéré.

## 5.3 Langage d'interface

Préférer :

- « Utilisé par 3 projets » plutôt que « dépendance active détectée ».
- « Sera téléchargé au prochain build » plutôt que « cache reconstructible ».
- « Peut contenir une base de données » plutôt que « volume persistant ».
- « Nous ne pouvons pas confirmer son usage » plutôt qu'une fausse certitude.

---

# BSF 6 - Modèle de sécurité

## 6.1 Invariants

- Une analyse est en lecture seule.
- Aucune suppression n'est présélectionnée lors de la première utilisation.
- Une absence de référence ne prouve jamais seule qu'une ressource est inutile.
- Les symlinks ne sont jamais suivis hors des racines autorisées sans validation.
- Les sources, documents personnels, secrets et fichiers non générés ne sont
  jamais considérés comme des caches.
- Les volumes Docker, archives publiées, certificats et sauvegardes sont protégés.
- Les commandes exécutées utilisent des arguments structurés, sans concaténation
  de chaînes provenant des noms de fichiers.
- Le rapport ne contient aucun secret ni contenu de fichier utilisateur.

## 6.2 Niveaux d'action

| Niveau | Exemple | Comportement |
|---|---|---|
| Sûr | Build Flutter local | Suppression proposée avec validation simple |
| Reconstructible coûteux | Cache de dépendances | Afficher le coût de téléchargement estimé |
| Géré par un outil | NDK Android | Utiliser le gestionnaire officiel |
| Sensible | Volume Docker | Bloqué par défaut, parcours séparé |
| Inconnu | Dossier non reconnu | Rapport uniquement |

## 6.3 Traçabilité

Chaque opération enregistre localement :

- la ressource ciblée ;
- la preuve ayant conduit à la recommandation ;
- la commande ou API utilisée ;
- l'heure, le résultat et le code de sortie ;
- l'espace estimé puis réellement récupéré.

Le journal ne doit jamais contenir de jeton, variable d'environnement sensible
ou contenu de configuration privée.

---

# BSF 7 - Architecture initiale

## 7.1 Direction recommandée

Commencer par un **monolithe local** composé de quatre responsabilités :

```text
Scanner -> Analyseur de références -> Planificateur -> Exécuteur
```

- **Scanner** : trouve les projets et ressources dans les racines autorisées.
- **Analyseur** : lit les configurations et relie projets, versions et artefacts.
- **Planificateur** : calcule les recommandations, risques et espace récupérable.
- **Exécuteur** : applique uniquement un plan confirmé et journalisé.

Le moteur doit d'abord être utilisable par une CLI. L'interface macOS consommera
ensuite la même API locale. Aucun backend, compte ou base distante n'est requis.

## 7.2 Modèle conceptuel minimal

- `Project` : chemin, type, état Git et dernière activité observable.
- `Requirement` : outil, version et source de la déclaration.
- `Artifact` : ressource locale, taille, date et niveau de sensibilité.
- `Reference` : lien prouvé ou probable entre projet et ressource.
- `Recommendation` : action proposée, confiance, impact et justification.
- `CleanupPlan` : sélection immuable validée par l'utilisateur.
- `Operation` : résultat journalisé d'une action.

## 7.3 Choix de technologie

**OUVERT.** Un prototype court doit comparer :

- Swift pour une intégration macOS directe ;
- Go pour une CLI simple, portable et distribuée en binaire unique ;
- Rust pour la sûreté mémoire et un moteur portable, au prix d'une complexité
  initiale plus élevée.

Le choix doit être fondé sur la sécurité des opérations fichiers, la qualité des
bibliothèques système, la distribution signée et la vitesse de développement.
Le futur client desktop pourra être natif ou utiliser une couche légère autour
du moteur. Il ne faut pas choisir un framework d'interface avant de valider le
moteur d'analyse.

## 7.4 Détecteurs du MVP

- Flutter/Dart : `.fvmrc`, métadonnées Flutter, `pubspec.yaml` et fichiers Android.
- Gradle : Wrapper, paramètres et scripts Groovy/Kotlin.
- Android : packages installés, NDK, SDK, images système et AVD.
- Docker : `docker compose`, Dockerfiles, builders et inventaire du daemon.

Une interface interne commune est acceptable, mais aucun système de plugins
public ne doit être construit avant d'avoir plusieurs détecteurs stables.

---

# BSF 8 - Validation

## 8.1 Définition de terminé du MVP d'audit

Le MVP est validé lorsqu'il peut, sur une machine de test représentative :

- détecter au moins 95 % des projets Flutter/Android présents dans les racines
  explicitement choisies ;
- associer correctement leurs versions Gradle et NDK ;
- distinguer une version utilisée d'une version réellement sans référence ;
- mesurer le cache Docker sans supprimer images, conteneurs ou volumes ;
- produire un rapport lisible avec preuves et niveau de confiance ;
- fonctionner entièrement hors ligne, hors commandes locales interrogées ;
- terminer un audit sans modifier les projets ni leurs dates de fichiers ;
- passer les scénarios de test avec symlinks, permissions refusées, projets
  incomplets, configurations invalides et processus de build actifs.

## 8.2 Définition de terminé du MVP de nettoyage

- La simulation et l'exécution utilisent exactement le même plan immuable.
- Toute commande destructrice exige une confirmation explicite.
- Les opérations sûres sont idempotentes.
- Une erreur arrête proprement le plan ou suit une stratégie annoncée.
- Le rapport distingue estimation et espace réellement récupéré.
- Aucun scénario automatisé ne supprime un volume Docker ou un fichier source.
- Les tests d'intégration utilisent des environnements temporaires et des
  fixtures, jamais les vraies données de la machine de développement.

## 8.3 Jeu d'évaluation initial

Créer des fixtures couvrant :

- deux projets partageant le même NDK ;
- un projet archivé utilisant une ancienne version Gradle ;
- un NDK installé sans référence ;
- un projet dont la version est définie par variable ou fichier externe ;
- un cache Docker volumineux avec images et volumes à préserver ;
- un projet Flutter propre et un projet avec modifications non commitées ;
- un AVD démarré pendant l'analyse ;
- un chemin avec espaces, accents et symlink externe.

---

# BSF 9 - Modèle économique

## 9.1 Hypothèse initiale

### Gratuit

- Audit local.
- Vue par écosystème.
- Simulation et nettoyage manuel sûr.
- Export d'un rapport local.

### Payant potentiel

- Surveillance continue et alertes avant saturation.
- Politiques automatiques avec limites et fenêtres de rétention.
- Historique avancé et comparaison avant/après.
- Règles partagées entre membres d'une équipe.
- Audit CI et parc de machines.

## 9.2 Risque économique

Les développeurs disposent déjà de scripts gratuits et peuvent considérer le
nettoyage comme une tâche ponctuelle. La monétisation dépendra donc de la
confiance, de la prévention et du gain de temps récurrent, pas du simple bouton
« Nettoyer ».

---

# BSF 10 - Paysage existant

Les solutions actuelles confirment le besoin, mais couvrent surtout une partie
du problème :

- Flutter fournit `flutter clean`, limité au projet courant.
- Gradle possède son propre nettoyage et des durées de rétention configurables.
- Android Studio et `sdkmanager` installent ou désinstallent SDK et NDK.
- Docker BuildKit possède un garbage collector et des limites de stockage.
- FVM gère les versions Flutter par projet.
- Des outils comme Mac Dev Cleaner et DevCleaner for Xcode regroupent plusieurs
  emplacements de caches.

La différenciation doit rester la **preuve d'utilisation inter-projets** et le
**plan d'impact**, pas le nombre de dossiers connus.

### Références initiales

- [Flutter CLI](https://docs.flutter.dev/reference/flutter-cli)
- [Caches Gradle](https://docs.gradle.org/current/userguide/directory_layout.html)
- [Android sdkmanager](https://developer.android.com/tools/sdkmanager)
- [Garbage collection Docker BuildKit](https://docs.docker.com/build/cache/garbage-collection/)
- [FVM](https://github.com/conceptadev/fvm)
- [Mac Dev Cleaner](https://github.com/thanhdevapp/mac-dev-cleaner-cli)

---

# BSF 11 - Risques

## 11.1 Risques produit

- Être perçu comme un nettoyeur Mac supplémentaire.
- Afficher trop d'informations techniques et perdre l'utilisateur.
- Surévaluer l'espace récupérable ou sous-estimer le coût de reconstruction.
- Ne pas apporter assez de valeur récurrente pour justifier un achat.

## 11.2 Risques techniques

- Configurations Gradle dynamiques difficiles à interpréter statiquement.
- Projets situés hors des racines analysées et donc références invisibles.
- Évolutions fréquentes des formats et commandes des outils détectés.
- Permissions macOS et sandboxing de l'application.
- Mesures de taille différentes entre fichiers logiques, snapshots et stockage
  réellement libéré.
- Processus concurrents qui recréent ou utilisent une ressource pendant l'action.

## 11.3 Réponses prévues

- Afficher un niveau de confiance et les limites de chaque conclusion.
- Ne jamais présenter « aucune référence détectée » comme une certitude absolue.
- Utiliser les outils officiels et tester chaque version supportée.
- Ajouter des adaptateurs versionnés seulement lorsqu'un cas réel le nécessite.
- Commencer en lecture seule avec des utilisateurs pilotes.

---

# BSF 12 - Feuille de route

## Phase 0 - Recherche et preuve

- Auditer plusieurs machines et mesurer les catégories dominantes.
- Recenser les outils existants et leurs erreurs fréquentes.
- Prototyper uniquement la détection Flutter, Gradle et NDK.
- Vérifier qu'une relation projet-version fiable est techniquement possible.

## Phase 1 - CLI d'audit

- Scanner des racines choisies.
- Construire le graphe projets-ressources.
- Produire un rapport texte et JSON.
- N'exécuter aucune suppression.

## Phase 2 - Nettoyage contrôlé

- Plans immuables et simulations.
- Actions Flutter, Android, Gradle et cache BuildKit ciblées.
- Journal local et vérification de l'espace récupéré.

## Phase 3 - Application macOS

- Interface claire par projets et écosystèmes.
- Permissions et signature.
- Alertes locales et politiques manuelles.

## Phase 4 - Prévention et équipes

- Seuils de stockage.
- Politiques partagées.
- Intégration CI et autres systèmes selon la demande réelle.

---

# BSF 13 - Questions ouvertes

- Quel nom exprime la sécurité et la compréhension sans évoquer un antivirus ?
- Quel niveau de rétention par défaut équilibre espace disque et vitesse de build ?
- Faut-il mettre en quarantaine certains dossiers avant suppression définitive ?
- Comment déterminer la dernière utilisation réelle d'une toolchain ?
- Jusqu'où analyser les expressions dynamiques Gradle sans exécuter le build ?
- Le produit doit-il rester macOS-only ou préparer un moteur portable ?
- Quelle partie apporte assez de valeur récurrente pour être payante ?
- Une application distribuée hors Mac App Store est-elle nécessaire pour obtenir
  les permissions et exécuter les outils développeur correctement ?

---

# Prochaines étapes

1. Valider ce cadrage et le nom de travail.
2. Inventorier deux à cinq machines ou environnements représentatifs.
3. Définir le format du rapport d'audit et les niveaux de confiance.
4. Construire un prototype en lecture seule pour Flutter, Gradle et NDK.
5. Tester le prototype sur des fixtures avant toute machine réelle.
6. Choisir la technologie du moteur à partir des résultats du prototype.
