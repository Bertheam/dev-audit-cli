# ADR 0017 — Inventorier Xcode/iOS depuis le système de fichiers

- Statut : accepté
- Date : 2026-09-05

## Contexte

Xcode, DerivedData, Device Support, les runtimes et les appareils simulés
peuvent représenter une part importante du stockage macOS. Leur gestion est
cependant liée à l'état de Xcode et de CoreSimulator. Le premier Mac de
validation installe actuellement un runtime iOS 26.5 : lancer des commandes de
gestion ou proposer un nettoyage pendant cette opération serait une mauvaise
frontière de sécurité.

Apple expose les runtimes installés et l'espace récupérable dans Xcode Settings
> Components, et les simulateurs dans le Device Hub. Apple précise aussi qu'une
plateforme en téléchargement ou en installation ne peut pas encore servir à
construire ou exécuter une application.

## Décision

Le Lot 11 utilise uniquement l'interface de système de fichiers déjà bornée. Il
détecte `DEVELOPER_DIR`, les bundles `Xcode*.app`, `~/Library/Developer` et
`/Library/Developer`, puis inventorie des chemins structurels allowlistés.

Il n'appelle ni Xcode, ni `xcodebuild`, ni `simctl`. Les plist ne sont lues que
si elles sont régulières, internes à la ressource et sous la limite de taille ;
seules des clés XML connues sont conservées. Un format binaire ou malformé
laisse la valeur inconnue.

Les appareils simulés reçoivent `sensitive_mutable_user_data` et
`activity_state=unknown`. Les fichiers ou dossiers observés sous `Packages`
reçoivent `sensitive_mutable_download` et
`activity_state=unknown_may_be_in_progress`. Aucun de ces signaux ne prétend
connaître l'état réel de Xcode. Aucun type Apple ne dispose d'une action de
planification.

DerivedData est classé reconstructible à partir de sa nature de sortie de build
et d'index, mais reste `INCONNUE` faute de dernière utilisation fiable. Les
dates de fichiers ne servent pas à conclure qu'une ressource est ancienne.

## Conséquences

Le scan peut présenter les principaux postes de stockage Xcode sans demander à
l'utilisateur leurs chemins et sans réveiller de simulateur. Les arbres de
runtimes montés ne sont pas parcourus : leur cardinalité élevée ralentirait le
scan et leur somme logique ne représente pas nécessairement l'espace physique
libérable. Leur taille reste explicitement non mesurée.

La validation terrain a attendu la fin du téléchargement. La comparaison
ponctuelle avec `simctl list` a confirmé les 8 runtimes et 11 appareils trouvés
sur le Mac 01, sans appareil booté. Le scanner lui-même reste filesystem-first :
l'état booté et la progression exacte des composants ne sont pas couverts par
le rapport. Une future interrogation dynamique devra rester en lecture seule,
allowlistée, bornée, testée séparément et ne pourra pas autoriser une action.

## Références

- Apple, installation et suppression de composants Xcode :
  <https://developer.apple.com/documentation/xcode/downloading-and-installing-additional-xcode-components>
- Apple, exécution sur appareils simulés ou physiques :
  <https://developer.apple.com/documentation/xcode/running-your-app-on-simulated-or-physical-devices>
- Apple, Device Hub : <https://developer.apple.com/documentation/xcode/device-hub>
