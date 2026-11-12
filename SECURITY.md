# Security

pggoulot is built on [quellog](https://github.com/Alain-L/quellog). For a vulnerability in code that
also exists in quellog, please report it to that project as well.

To report a vulnerability in pggoulot, use GitHub's private vulnerability reporting
("Report a vulnerability" in the Security tab). Please do not open a public issue.

pggoulot reads PostgreSQL logs and writes reports; it opens no network port and runs no query
against a database. The HTML report is a standalone file: log content is escaped before it is
displayed, and the only external call is the optional "Visualize" button, which sends a plan to
explain.dalibo.com after a confirmation.

Every push runs CodeQL (Go, JavaScript), Semgrep, gosec, govulncheck, OSV-Scanner, Trivy,
gitleaks, zizmor, golangci-lint, the race detector and fuzzing of the parsers; a release is only
built when all of them pass, and each published file carries a build provenance attestation. See
the Security section of the README and `.github/workflows/`.
