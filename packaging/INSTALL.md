# Installer Dev Environment Auditor sur macOS

Cette archive contient un binaire macOS précompilé. Go n'est pas requis.

1. Vérifier que l'archive correspond à l'architecture du Mac : `arm64` pour un
   Mac Apple Silicon, `amd64` pour un Mac Intel.
2. Télécharger le fichier `.sha256` associé puis vérifier l'archive :

   ```bash
   shasum -a 256 -c dev-audit_VERSION_darwin_ARCH.tar.gz.sha256
   ```

3. Extraire l'archive, entrer dans le dossier créé puis installer :

   ```bash
   tar -xzf dev-audit_VERSION_darwin_ARCH.tar.gz
   cd dev-audit_VERSION_darwin_ARCH
   ./install.sh
   ```

4. Lancer l'audit :

   ```bash
   dev-audit version
   dev-audit scan
   ```

L'installateur privilégie `/opt/homebrew/bin` puis `/usr/local/bin` lorsqu'ils
sont accessibles sans `sudo`. Sinon, il utilise `~/.local/bin` et affiche la
ligne à ajouter au `PATH`. Un dossier cible explicite peut aussi être fourni :

```bash
./install.sh /chemin/vers/bin
```
