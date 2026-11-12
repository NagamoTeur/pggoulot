<div align="center">

# pggoulot

**Trouver le goulot d'étranglement d'une requête PostgreSQL lente, directement dans les journaux.**

🇬🇧 [English](README.md)

[![CI](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml)
[![Sécurité](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml)
[![Paquets](https://github.com/NagamoTeur/pggoulot/actions/workflows/packages.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/packages.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/NagamoTeur/pggoulot/badge)](https://scorecard.dev/viewer/?uri=github.com/NagamoTeur/pggoulot)
[![Release](https://img.shields.io/github/v/release/NagamoTeur/pggoulot?include_prereleases&sort=semver)](https://github.com/NagamoTeur/pggoulot/releases)
[![Licence PostgreSQL](https://img.shields.io/badge/licence-PostgreSQL-blue)](LICENSE)

</div>

Une requête de 7 minutes en production : la base, le réseau ou l'application ? D'habitude, la
réponse demande une dizaine de grep dans les journaux : retrouver le `PREPARE` derrière un
`EXECUTE`, le plan qui lui appartient vraiment, les fichiers temporaires de ses workers parallèles,
la chronologie de la session. pggoulot lit les journaux une fois et rassemble tout, en expliquant.

Il est bâti sur [quellog](https://github.com/Alain-L/quellog), l'analyseur de journaux PostgreSQL
d'Alain Lesage, dont il garde tout le rapport. pggoulot est un projet indépendant, sans lien avec
quellog.

## Ce qu'il vous dit

```text
$ pggoulot trace /var/log/postgresql/ --lang fr

OÙ EST PASSÉ LE TEMPS

  Exécuteur (auto_explain)        2,1 s  0,9 %
  Après l'exécuteur              3m 45s  99 %

CONSTATS

  • L'envoi du résultat a pris 3m 45s (99 % du temps)
    Pourquoi : 919,2 Mo ont été envoyés en 3m 45s, soit 4,1 Mo/s (environ
    34 Mbit/s). La base, elle, avait fini en 2,1 s.
    Impact ici : le goulot est le réseau ou le client, pas la base.
    À vérifier (lecture seule) :
      SELECT pid, client_addr, wait_event FROM pg_stat_activity
      WHERE wait_event = 'ClientWrite';
    À proposer : vérifier le chemin réseau et la façon dont l'application lit le résultat.

  • Sort a débordé de 921,3 Mo sur disque
    Impact ici : cette étape a pris 651 ms, 0,3 % du temps de la requête :
    ce n'est pas la cause ici.
```

- **La vraie requête** : le texte préparé, ses paramètres remplacés, et un `EXPLAIN` prêt à coller.
- **Son propre plan** : le plan auto_explain de cette requête, pas celui qu'une requête précédente
  a laissé en attente sur la connexion.
- **Où est passé le temps** : l'exécuteur face au reste, et pour un `EXECUTE` en SQL le débit réel
  vers le client.
- **Des constats classés par leur part du temps**, chacun expliqué : ce qui s'est passé, si ça
  compte ici, les requêtes en lecture seule qui le confirment, ce qu'il faut proposer (index,
  `work_mem`, `ANALYZE`…). En français ou en anglais.
- **Les fichiers temporaires** du backend et de ses workers, et la **chronologie** de la session.
- **Dans tous les rapports** : texte, Markdown, JSON, YAML et le rapport HTML autonome.
- **Les journaux tels quels** : stderr, syslog, jsonlog, csvlog, formats cloud, archives, entrée
  standard.
- **Sûr chez un client** : lecture seule, aucun port réseau, aucune connexion à une base.
  `--redact` masque les valeurs et les paramètres (données personnelles) dans toutes les sorties ;
  `--offline` retire du rapport le seul bouton qui envoie quelque chose à l'extérieur.

## Installer

Télécharger un paquet dans les [releases](https://github.com/NagamoTeur/pggoulot/releases) (Linux,
macOS et Windows ; amd64 et arm64), le vérifier, puis l'installer :

```bash
gh attestation verify pggoulot_0.1.0_linux_amd64.deb -R NagamoTeur/pggoulot
sha256sum -c --ignore-missing checksums.txt
sudo apt install ./pggoulot_0.1.0_linux_amd64.deb
```

L'attestation prouve que le fichier a été construit par la CI de ce dépôt à partir du tag de la
release. Chaque release fournit aussi une SBOM (la liste de tous les composants). `pggoulot`
s'installe à côté de `quellog` sans le remplacer. Depuis les sources :
`go generate ./web/... && go build -o pggoulot .`

## Utiliser

```bash
pggoulot /var/log/postgresql/ --lang fr                   # le rapport de quellog, avec les requêtes les plus lentes tracées
pggoulot trace /var/log/postgresql/ -n 3 --lang fr        # la trace complète des 3 plus lentes
pggoulot /var/log/postgresql/ --redact --offline --html   # un rapport HTML à garder sur place
```

Voir [docs/trace.md](docs/trace.md) pour les traces, [docs/quellog.md](docs/quellog.md) pour le
reste (toutes les commandes de quellog marchent avec `pggoulot`), et [FORK.md](FORK.md).

## Sécurité

Chaque push et chaque pull request passent les contrôles ci-dessous ; une release n'est construite
que si **tous** passent, puis chaque fichier publié reçoit une attestation. Les rapports sont dans
l'[onglet Security](https://github.com/NagamoTeur/pggoulot/security) du dépôt.

| Contrôle | Ce qu'il cherche | État |
|---|---|---|
| [CodeQL](https://codeql.github.com/) | Failles dans le code Go et JavaScript (requêtes étendues) | [![CodeQL Go](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=CodeQL%20(go)&label=Go)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) [![CodeQL JS](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=CodeQL%20(javascript-typescript)&label=JS)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [Semgrep](https://semgrep.dev/) | Règles de sécurité Go et JavaScript, XSS, secrets | [![Semgrep](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=Semgrep&label=Semgrep)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [gosec](https://github.com/securego/gosec) | Règles de sécurité Go | [![gosec](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=gosec&label=gosec)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [govulncheck](https://go.dev/doc/security/vuln/) | Failles Go connues dans le code réellement appelé | [![govulncheck](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=govulncheck&label=govulncheck)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [OSV-Scanner](https://google.github.io/osv-scanner/) | Failles connues dans toutes les dépendances (osv.dev) | [![OSV-Scanner](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=OSV-Scanner&label=OSV)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [Trivy](https://trivy.dev/) | Failles hautes et critiques corrigeables, secrets, mauvaises configurations | [![Trivy](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=Trivy&label=Trivy)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [gitleaks](https://github.com/gitleaks/gitleaks) | Secrets dans tout l'historique git | [![gitleaks](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=gitleaks&label=gitleaks)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [zizmor](https://docs.zizmor.sh/) | Faiblesses des workflows de CI eux-mêmes | [![zizmor](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=zizmor&label=zizmor)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [OpenSSF Scorecard](https://scorecard.dev/) | Bonnes pratiques de chaîne d'approvisionnement du dépôt | [![Scorecard](https://api.scorecard.dev/projects/github.com/NagamoTeur/pggoulot/badge)](https://scorecard.dev/viewer/?uri=github.com/NagamoTeur/pggoulot) |
| Tests | Détection de courses, corpus de régression, tests web, fuzzing des parseurs | [![CI](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml) [![Fuzz](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=fuzz&label=fuzz)](https://github.com/NagamoTeur/pggoulot/actions/workflows/fork-checks.yml) |

Les workflows épinglent chaque action à un commit, tournent en lecture seule par défaut, et les
releases se construisent sans cache. Dependabot propose les mises à jour de sécurité, et le
scan de secrets de GitHub bloque un push qui en contiendrait. Pour signaler une faille :
[SECURITY.md](SECURITY.md).

## Bâti sur quellog

pggoulot suit quellog : chaque semaine, une issue s'ouvre si quellog a des changements à fusionner,
et le corpus de régression vérifie que, traces mises à part, les rapports de pggoulot sont
identiques à ceux de quellog. Les correctifs trouvés en route lui sont proposés (par exemple
[Alain-L/quellog#10](https://github.com/Alain-L/quellog/pull/10)).

## Licence

[Licence PostgreSQL](LICENSE). Copyright (c) 2025-2026 Alain Lesage pour quellog ; les ajouts de
pggoulot sont sous la même licence.
