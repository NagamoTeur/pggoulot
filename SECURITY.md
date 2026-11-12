# Security

This is a fork of [quellog](https://github.com/Alain-L/quellog). For a vulnerability in code that
also exists upstream, please report it to the original project as well.

To report a vulnerability in this fork, use GitHub's private vulnerability reporting
("Report a vulnerability" in the Security tab). Please do not open a public issue.

quellog reads PostgreSQL logs and writes reports; it opens no network port and runs no query
against a database. The HTML report is a standalone file: log content is escaped before it is
displayed, and the only external call is the optional "Visualize" button, which sends a plan to
explain.dalibo.com after a confirmation.

Every push runs CodeQL (Go, JavaScript), govulncheck, gosec, gitleaks, Trivy, golangci-lint,
the race detector and fuzzing of the trace parsers; see `.github/workflows/`.
