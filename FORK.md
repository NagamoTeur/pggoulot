# quellog-plus

Fork non officiel de [quellog](https://github.com/Alain-L/quellog) (Alain Lesage, licence
PostgreSQL), pensé pour l'exploitation : enquêter sur une requête lente sans faire de grep dans les
journaux, et comprendre où est passé son temps. Sans lien avec le projet d'origine.

## Installer

Les [releases](https://github.com/NagamoTeur/quellog-plus/releases) fournissent le binaire
`quellog-plus` (Linux, macOS, Windows ; amd64 et arm64), des paquets `.deb` et `.rpm`, les sommes
SHA-256 et la liste des composants (SBOM). Chaque fichier porte une attestation de provenance
signée par GitHub, qui prouve qu'il a été construit par la CI de ce dépôt à partir du tag :

```bash
gh attestation verify quellog-plus_0.13.0-plus.1_linux_amd64.deb -R NagamoTeur/quellog-plus
sha256sum -c --ignore-missing checksums.txt
sudo apt install ./quellog-plus_0.13.0-plus.1_linux_amd64.deb
```

`quellog-plus` s'installe à côté de `quellog` sans le remplacer.

## Publier une version

Ajouter la section de la version en tête du CHANGELOG (`## [0.13.0-plus.1] - date`), commiter,
puis poser le tag :

```bash
git tag v0.13.0-plus.1 && git push origin v0.13.0-plus.1
```

`.github/workflows/release-plus.yml` relance alors tous les contrôles (tests avec détection de
courses, tests JS, fuzzing, govulncheck, gosec, et la quality gate SonarQube si elle est
configurée) ; s'ils passent, GoReleaser (`.goreleaser-plus.yml`) construit et publie la release,
puis chaque fichier reçoit son attestation. La version suit celle de quellog : `0.13.0-plus.N`.

## Chez un client

Les journaux contiennent souvent des données personnelles : valeurs dans les requêtes, paramètres,
valeurs des erreurs de contrainte. Pour un rapport qui ne les montre pas et qui n'envoie rien hors
de la machine :

```bash
quellog-plus /var/log/postgresql/ --redact --offline --html --lang fr
```

`--redact` remplace les littéraux entre apostrophes, les paramètres et les valeurs des erreurs de
clé par `?`, avant l'analyse, donc dans toutes les sorties (texte, JSON, HTML, traces). Les nombres
hors apostrophes et les chaînes entre `$$` ne sont pas masqués. `--offline` retire du rapport HTML
les boutons « Visualize » qui envoient un plan à explain.dalibo.com. Le rapport reste un fichier
local : le garder là où sont les journaux.

## Ce que le fork ajoute

**`quellog trace`** : voir [docs/trace.md](docs/trace.md). Pour la requête la plus lente (ou celles
choisies par `--pid`, `--query`, `--top`), la requête préparée et ses paramètres, le plan
auto_explain qui lui appartient vraiment, la part du temps passée dans la base et celle passée
à envoyer au client, les fichiers temporaires du backend et de ses workers parallèles, ce qui
s'est passé sur le backend pendant ce temps, et la requête prête à passer dans `EXPLAIN`.

**Les traces dans tous les rapports** : chaque analyse trace les trois requêtes les plus lentes
(`--trace-top N`, 0 pour couper). Bloc compact dans le rapport texte, traces complètes avec
`--traces` ou `--full`, section en Markdown, tableau `traces` en JSON et YAML. Dans le rapport HTML :
une section « Slowest Statements Traced », une fenêtre par trace (où le temps est passé, requête,
rejeu, plan avec Visualize, constats, fichiers temporaires, chronologie) et « Traced Runs » dans le
détail d'une requête. stderr, syslog, jsonlog et csvlog, fichiers, archives ou entrée standard.

**Des constats expliqués, en français** (`--lang fr`, ou `LANG=fr_…`) : chaque constat dit ce qui
s'est passé avec ses chiffres, s'il compte vraiment (sa part du temps, calculée sur l'arbre du
plan), les requêtes en lecture seule qui le confirment et ce qu'il faut proposer au DBA ou aux
développeurs. Débit réel vers le client, tri ou hachage sur disque avec le `work_mem` qu'il
faudrait, filtre qui jette presque tout avec l'index à proposer (et la conversion de type qui
l'empêcherait de servir), estimation fausse, workers manquants, attente de verrou, JIT,
conditions répétées. Tout est calculé à partir des journaux, sans IA : rien ne sort de la machine.
Le reste du rapport quellog reste en anglais.

**Faille XSS corrigée** dans le rapport HTML : `escForJsAttr` n'échappait pas les barres obliques
inverses, si bien qu'un texte de journal contenant `\'` pouvait sortir de la chaîne JavaScript
d'un bouton « Copy » et exécuter du code à l'ouverture du rapport.

**Plans des journaux JSON et CSV** : le plan d'une requête ne commence plus par la fin d'un
`Query Text` sur plusieurs lignes et la ligne `Query Parameters`.

**Plans rattachés à la bonne requête** (`analysis/sql.go`, `planBelongsTo`). Le rapport habituel
collait le plan en attente sur un backend à la requête chronométrée suivante, même quand le
plan venait d'une autre requête (plus lente qu'`auto_explain.log_min_duration` mais plus rapide
que `log_min_duration_statement`). Il compare maintenant le `Query Text` du plan à la requête ;
un `EXECUTE` SQL est reconnu sous son `PREPARE`.

**Plans complets en stderr** : `Gather` et `Gather Merge` manquaient à la liste des nœuds, si bien
que le haut du plan disparaissait.

**Le parseur stderr garde les lignes des plans auto_explain**, comme jsonlog et csvlog : la trace
montre le plan tel que PostgreSQL l'a écrit. L'analyse SQL d'origine le relit à plat, si bien que
le reste des rapports ne change pas. `parser.PreserveContinuationNewlines` étend cela à tous les
messages sur plusieurs lignes ; `trace` l'active. jsonlog et csvlog gardent aussi `leader_pid`.

**Le corpus de régression** tourne sans traces (`QUELLOG_TRACE_TOP=0`) : il vérifie que tout le
reste reste identique à quellog d'origine. Les traces ont leurs propres tests (`trace/`,
`output/trace_test.go`, fixtures stderr, JSON et CSV tirées d'une instance de test, sans aucune donnée réelle).

## Construire et vérifier

```bash
go generate ./web/...
go test ./...
go build .
```

La CI (`.github/workflows/`) lance les tests, le détecteur de courses, le fuzzing des traces,
golangci-lint, govulncheck, gosec, CodeQL et gitleaks, et vérifie que le module WASM compile.

## Sécurité

Vérifié avant publication (novembre 2026) :

| Outil | Résultat |
|---|---|
| SonarQube (Community) | Quality Gate passée, note sécurité A, 0 vulnérabilité, 0 point chaud ; 0 alerte sur le code du fork (couverture Go 78 à 90 %) |
| govulncheck | aucune faille dans le code appelé (après mise à jour de brotli, klauspost/compress, x/text, x/sys et de Go 1.27.4) |
| gosec | aucune alerte sur le code du fork ; celles du code d'origine sont du bruit (erreurs d'écriture non lues, conversions numériques, MD5 servant d'identifiant) |
| semgrep, Trivy, gitleaks | pas de faille, de dépendance vulnérable corrigeable ni de secret |
| fuzzing, `-race` | 4,7 millions d'entrées sans plantage, aucune course |

Corrigé au passage dans le code d'origine : la faille XSS d'`escForJsAttr` (décrite plus haut) et
l'ouverture du rapport sous Windows (`--open` passait par `cmd /C start`, qui interprète `&` et `|`
dans le nom du fichier ; c'est maintenant `rundll32`, sans shell). Voir aussi `SECURITY.md`.

## Suivre l'original

Chaque lundi, `.github/workflows/upstream-watch.yml` ouvre (ou met à jour) une issue
« Merge the original quellog » quand quellog a des changements pas encore fusionnés. Pour fusionner :

```bash
git fetch upstream && git merge upstream/main
go generate ./web/... && go test ./...
```

Le corpus de régression vérifie que tout ce qui n'est pas au fork donne la même sortie que l'original.

Les changements faits hors de `trace/`, `cmd/trace.go`, `cmd/traces_flags.go`, `output/trace.go`,
`web/js/sections/traces.js` et ce fichier sont petits et commentés, pour que les fusions restent
simples, et pourraient être proposés à l'auteur.
