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
built when all of them pass, and each published file carries a build provenance attestation and
a SLSA provenance (`pggoulot_<tag>.intoto.jsonl`, checked with
[slsa-verifier](https://github.com/slsa-framework/slsa-verifier)). See the Security section of the
README and `.github/workflows/`.

The `.rpm` packages and the dnf repository metadata are signed with the key in
`packaging/RPM-GPG-KEY-pggoulot`, fingerprint `2731 BA17 E2FF D85B 2C0B 77F3 2BCC A3F7 D067 8743`.
