# VPS Admin

Panneau d'administration auto-hébergé pour n'importe quel VPS Linux : un agent en un seul binaire Go sur le serveur, un panneau web Material 3 embarqué, et une app Android Flutter (Material You) qui se connecte à plusieurs serveurs.

> Statut :
>
> - Fonctionnel : l'agent (API, appairage, passkeys, métriques, services, Docker, audit de sécurité, mise à jour automatique) et le panneau web.
> - À venir : les déploiements depuis GitHub, l'assistant et l'app.

## Fonctionnalités

- **Tableau de bord** : CPU, mémoire, swap, disques, réseau et charge, en direct (SSE) avec historique sur 1 h et 24 h.
- **Services systemd** : liste, démarrer, arrêter, redémarrer, journaux. Arrêter `ssh` ou l'agent lui-même est refusé, pour ne jamais perdre l'accès.
- **Docker** : conteneurs groupés par projet compose, actions, logs, CPU et mémoire.
- **Audit de sécurité** :
  - SSH : port, mots de passe, root, X11, nombre d'essais ;
  - pare-feu, avec détection de ufw et d'une table nftables native qui filtrent en même temps ;
  - fail2ban ou CrowdSec, mises à jour en attente, redémarrage requis, mises à jour automatiques ;
  - LLMNR, ports ouverts, ports Docker publiés qui contournent ufw.
- **Journal d'audit** de chaque action, avec l'appareil et l'IP.
- **Mise à jour automatique** signée, avec retour arrière tout seul si la nouvelle version ne démarre pas.

## Installation

Sur le serveur, en root, avec un nom de domaine qui pointe dessus :

```sh
curl -fsSL https://github.com/beedywool/vps-admin/releases/latest/download/install.sh | sudo bash -s -- --domain admin.example.com
```

L'installeur :

1. Télécharge le binaire pour l'architecture (amd64 ou arm64), puis vérifie la signature ed25519 de `SHA256SUMS` et le checksum.
2. Crée l'utilisateur système `vps-admin`. Il est membre de `docker`, `systemd-journal` et `adm`.
3. Écrit la liste blanche sudo, avec les chemins réels des commandes.
4. Installe le service systemd.
5. Ajoute le domaine à Caddy si Caddy est présent. Sinon, fais pointer ton reverse proxy vers `127.0.0.1:7171`.
6. Affiche un **code d'appairage**.

Si le repo est **privé**, ajoute `--token <jeton>` : un jeton GitHub fine-grained, en lecture seule sur « Contents » de ce seul repo. Il est gardé dans la config pour que la mise à jour automatique continue de fonctionner.

On peut le relancer pour réparer ou mettre à jour : les données sont gardées. Pour un nouveau code plus tard : `sudo vps-admin pair`.

## Sécurité

- **Appairage** : le code est généré sur le serveur par root.
  - Il est à usage unique et valable 10 minutes, et seul le dernier généré fonctionne.
  - Les essais sont limités par IP et au total.
- **Web** :
  - après l'appairage, le navigateur doit enregistrer une **passkey** ;
  - chaque connexion suivante demande cette passkey ;
  - la session est un cookie `__Host-` en `SameSite=Strict`, et un en-tête dédié est exigé contre le CSRF.
- **App** : jeton d'appareil révocable depuis la liste des appareils.
- **Privilèges** :
  - l'agent tourne sous un utilisateur dédié et n'obtient root que pour les commandes de `/etc/sudoers.d/vps-admin` ;
  - l'accès Docker reste équivalent à root, comme pour tout outil d'administration Docker.
- **Exposition** : l'agent n'écoute que sur `127.0.0.1`. Le TLS est assuré par le reverse proxy.

## Mises à jour

Pousser un tag `vX.Y.Z` lance le workflow `release` :

1. Il compile les binaires.
2. Il signe `SHA256SUMS` avec le secret `RELEASE_SIGNING_KEY`.
3. Il publie la release.

Les agents vérifient GitHub toutes les 6 heures. Un binaire n'est installé que si la signature correspond à la clé publique compilée dans l'agent (`internal/update/pubkey.go`).

Après le remplacement, `preflight.sh` (lancé par systemd avant chaque démarrage) remet l'ancienne version si la nouvelle échoue trois fois. Cette version n'est alors plus retentée automatiquement.

Générer une paire de clés pour un fork :

```sh
go run ./tools/release-sign -keygen
```

## Développement

```sh
cd web && npm ci && npm run build && cd ..   # le panneau est compilé dans internal/web/dist
go test ./...
go run ./cmd/vps-admin serve -config dev.json   # sans fichier : 127.0.0.1:7171, données dans /var/lib/vps-admin
go run ./cmd/vps-admin pair -config dev.json
```

| Dossier | Contenu |
| --- | --- |
| `cmd/vps-admin` | point d'entrée : `serve`, `pair`, `version` |
| `internal/api` | routes HTTP, flux SSE, en-têtes de sécurité |
| `internal/auth` | appairage, jetons, sessions, passkeys |
| `internal/system` | métriques `/proc`, services, audit de sécurité |
| `internal/docker` | client minimal de l'API Docker |
| `internal/update` | releases signées, installation, retour arrière |
| `internal/store` | SQLite (pur Go, binaire statique) |
| `deploy/` | installeur, unité systemd, `preflight.sh` |
| `web/` | panneau Vue 3, Material 3 Expressive, couleurs Material You générées depuis une couleur de base |
| `app/` | app Flutter (à venir) |
