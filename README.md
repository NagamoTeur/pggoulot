<div align="center">

# pggoulot

**Find the bottleneck of a slow PostgreSQL statement, straight from the logs.**

*Goulot d'étranglement* is French for bottleneck. 🇫🇷 [Lire en français](README.fr.md)

[![CI](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml)
[![Security](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml)
[![Packages](https://github.com/NagamoTeur/pggoulot/actions/workflows/packages.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/packages.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/NagamoTeur/pggoulot/badge)](https://scorecard.dev/viewer/?uri=github.com/NagamoTeur/pggoulot)
[![Release](https://img.shields.io/github/v/release/NagamoTeur/pggoulot?include_prereleases&sort=semver)](https://github.com/NagamoTeur/pggoulot/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/NagamoTeur/pggoulot)](go.mod)
[![License: PostgreSQL](https://img.shields.io/badge/license-PostgreSQL-blue)](LICENSE)

</div>

A 7-minute query in production: was it the database, the network or the application? Answering
usually means a dozen greps through the logs: find the `PREPARE` behind an `EXECUTE`, the plan that
really belongs to it, the temp files of its parallel workers, the timeline of the session.
pggoulot reads the PostgreSQL logs once and puts it all in one place, with explanations.

It is built on [quellog](https://github.com/Alain-L/quellog), the PostgreSQL log analyzer by Alain
Lesage, and keeps everything quellog reports. pggoulot is an independent project, not affiliated
with quellog.

## What it tells you

```text
$ pggoulot trace /var/log/postgresql/

TRACE  3m 48s  pid 83

WHERE THE TIME WENT

  Executor (auto_explain)         2.1 s  0.9 %
  After the executor             3m 45s  99 %

FINDINGS

  • Sending the result took 3m 45s (99 % of the time)
    Why: 919.2 MB were sent in 3m 45s: 4.1 MB/s (about 34 Mbit/s). The
    database itself had finished in 2.1 s.
    Impact here: the bottleneck is the network or the client, not the database.
    To check (read-only):
      SELECT pid, client_addr, wait_event FROM pg_stat_activity
      WHERE wait_event = 'ClientWrite';
    To propose: check the network path and how the application reads the result.

  • Sort spilled 921.3 MB to disk
    Impact here: this step took 651 ms, 0.3 % of the statement's time: not the
    cause here.
```

- **The real query**: the prepared text, its parameters substituted, and an `EXPLAIN` ready to paste.
- **Its own plan**: the auto_explain plan that belongs to this statement, not the one a previous
  query left pending on the connection.
- **Where the time went**: executor time against the rest, and for an SQL `EXECUTE` the actual
  throughput to the client.
- **Findings ranked by their share of the time**, each explained: what happened, whether it
  matters here, the read-only queries that confirm it, what to propose (index, `work_mem`,
  `ANALYZE`…). In English or French (`--lang fr`).
- **Temp files** of the backend and of its parallel workers, and the **timeline** of the session.
- **In every report**: text, Markdown, JSON, YAML and the standalone HTML report.
- **Logs as they are**: stderr, syslog, jsonlog, csvlog, cloud formats, archives, standard input.
- **Safe on a customer's server**: read-only, no network port, no database connection.
  `--redact` masks literals and parameter values (personal data) in every output; `--offline`
  removes the report's only outbound button.

## Install

Download a package from the [releases](https://github.com/NagamoTeur/pggoulot/releases) (Linux,
macOS and Windows; amd64 and arm64), check it, then install it:

```bash
gh attestation verify pggoulot_0.1.0_linux_amd64.deb -R NagamoTeur/pggoulot
sha256sum -c --ignore-missing checksums.txt
sudo apt install ./pggoulot_0.1.0_linux_amd64.deb
```

The attestation proves the file was built by this repository's CI from the release tag. Each
release also ships an SBOM (the list of every component). `pggoulot` installs next to `quellog`
without replacing it. From source: `go generate ./web/... && go build -o pggoulot .`

## Use

```bash
pggoulot /var/log/postgresql/                      # quellog's report, with the slowest statements traced
pggoulot trace /var/log/postgresql/ -n 3           # the full trace of the 3 slowest statements
pggoulot trace /var/log/postgresql/ --query orders --lang fr
pggoulot /var/log/postgresql/ --redact --offline --html   # an HTML report safe to keep on site
```

See [docs/trace.md](docs/trace.md) for traces and [docs/quellog.md](docs/quellog.md) for everything
else (all quellog commands work with `pggoulot`).

## Security

Every push and pull request runs the checks below; a release is only built when **all** of them
pass, then every file it publishes is attested. Reports land in the repository's
[Security tab](https://github.com/NagamoTeur/pggoulot/security).

| Check | What it looks for | Status |
|---|---|---|
| [CodeQL](https://codeql.github.com/) | Security flaws in the Go and JavaScript code (extended queries) | [![CodeQL Go](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=CodeQL%20(go)&label=Go)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) [![CodeQL JS](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=CodeQL%20(javascript-typescript)&label=JS)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [Semgrep](https://semgrep.dev/) | Go and JavaScript security rules, XSS, secrets | [![Semgrep](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=Semgrep&label=Semgrep)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [gosec](https://github.com/securego/gosec) | Go security rules | [![gosec](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=gosec&label=gosec)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [govulncheck](https://go.dev/doc/security/vuln/) | Known Go vulnerabilities in the code actually called | [![govulncheck](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=govulncheck&label=govulncheck)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [OSV-Scanner](https://google.github.io/osv-scanner/) | Known vulnerabilities in every dependency (osv.dev) | [![OSV-Scanner](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=OSV-Scanner&label=OSV)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [Trivy](https://trivy.dev/) | Fixable high and critical vulnerabilities, secrets, misconfigurations | [![Trivy](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=Trivy&label=Trivy)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [gitleaks](https://github.com/gitleaks/gitleaks) | Secrets in the whole git history | [![gitleaks](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=gitleaks&label=gitleaks)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [zizmor](https://docs.zizmor.sh/) | Weaknesses in the CI workflows themselves | [![zizmor](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=zizmor&label=zizmor)](https://github.com/NagamoTeur/pggoulot/actions/workflows/security.yml) |
| [OpenSSF Scorecard](https://scorecard.dev/) | Supply-chain practices of the repository | [![Scorecard](https://api.scorecard.dev/projects/github.com/NagamoTeur/pggoulot/badge)](https://scorecard.dev/viewer/?uri=github.com/NagamoTeur/pggoulot) |
| Tests | Race detector, regression corpus, web tests, fuzzing of the parsers | [![CI](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml/badge.svg)](https://github.com/NagamoTeur/pggoulot/actions/workflows/ci.yml) [![Fuzz](https://img.shields.io/github/check-runs/NagamoTeur/pggoulot/main?nameFilter=fuzz&label=fuzz)](https://github.com/NagamoTeur/pggoulot/actions/workflows/fork-checks.yml) |

The CI workflows pin every action to a commit, run with read-only permissions by default, and
releases build without caches. Dependabot opens security updates, and GitHub's secret scanning
blocks pushes that contain a secret. To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Built on quellog

pggoulot follows quellog: a weekly job opens an issue when quellog has changes to merge, and the
regression corpus checks that, traces aside, pggoulot's reports are byte-identical to quellog's.
Fixes found along the way go back upstream (for example
[Alain-L/quellog#10](https://github.com/Alain-L/quellog/pull/10)). What pggoulot adds and changes is
listed in [FORK.md](FORK.md) (in French) and the [CHANGELOG](CHANGELOG.md).

## License

[PostgreSQL License](LICENSE). Copyright (c) 2025-2026 Alain Lesage for quellog; pggoulot's
additions are under the same license.
