# Fixtures Phase 0

- `flutter_explicit` contient des versions littérales attendues comme preuves
  explicites, ainsi qu'un alias de catalogue de versions effectivement utilisé.
- `android_dynamic` contient volontairement des expressions dynamiques qui
  devront produire `PROBABLY_REQUIRED` ou `UNKNOWN`, jamais une valeur inventée.

Les scénarios historiques, malformés, limites, permissions, chemins supprimés
et symlinks externes sont construits dans des répertoires temporaires par les
tests afin de rester portables. Les installations Android, Flutter/FVM et JDK
synthétiques du Lot 3 suivent la même règle pour ne pas alourdir le dépôt.
