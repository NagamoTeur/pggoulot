# Tracing a slow statement

`quellog trace` follows the slowest statements through the logs and puts in one place what
otherwise takes a dozen greps: the prepared text, the parameters, the plan that belongs to the
statement, where the time went, the temporary files of the backend and of its parallel workers,
and what the backend logged while it ran.

```bash
quellog trace /var/log/postgresql/            # the slowest statement
quellog trace /var/log/postgresql/ -n 5       # the five slowest
quellog trace /var/log/postgresql/ --query shipments --min-duration 1m
quellog trace /var/log/postgresql/ --pid 108478 --last 2h
zcat postgresql-*.log.gz | quellog trace -    # standard input
```

| Flag | Meaning |
|---|---|
| `-n`, `--top` | number of statements, slowest first (default 1) |
| `-p`, `--pid` | only this backend |
| `--query` | the statement or its prepared text contains this (case-insensitive) |
| `--min-duration` | ignore faster statements (`30s`, `1m`) |

The usual filters (`--begin`, `--end`, `--last`, `--window`, `--dbname`, `--dbuser`, `--appname`)
choose which statements are traced. The entries around a traced statement are always read in
full: parallel workers log without database or user. The logs are read once. When
`log_destination` writes both `x.log` and `x.json` (or `x.csv`), only `x.log` is read.

## In the reports

Every report traces the three slowest statements as well:

| Output | What it shows |
|---|---|
| text (default) | a compact block: duration, query, time split, findings, temp files |
| text `--traces`, `--full` | the full traces, as `quellog trace` prints them |
| `--md` | a section per trace; with `--traces` or `--full`, plan, temp files and timeline too |
| `--json`, `--yaml` | a `traces` array (query, replay, time split, plan lines, findings, temp files, events, `query_id`) |
| `--html` | a *Slowest Statements Traced* section; a row opens the trace. The query detail lists its *Traced Runs* |

`--trace-top N` changes the number (0 turns tracing off). With `--split`, each period has its own
traces. The HTML time filter keeps the traces that ran in the selected range; they remain the
slowest of the whole log. Unlike `quellog trace`, the report's `--dbname`/`--dbuser`/`--appname`
filters apply before the analysis, so they hide the parallel workers' temp files (workers log
without database or user).

## Findings and language

Each finding is explained: what happened, with its numbers; whether it matters here (its share
of the statement's time, computed on the plan tree, so a sort that spilled 900 MB in 0.6 s of a
4-minute statement is reported as "not the cause"); read-only queries that confirm it; and what to
propose to the DBA or the developers. The findings are sorted by weight. They cover the transfer
to the client (actual throughput, Qt QPSQL specifics), sorts and hashes spilled to disk, scans
that throw most rows away (with the index to propose and the type cast that would defeat it),
estimates off by more than 10×, parallel workers planned but not launched, lock waits, JIT
compilation and conditions repeated in a filter. Everything is computed from the logs.

`--lang fr` (or `QUELLOG_LANG=fr`, or a French `LANG`) writes the traces in French, in every
output; the rest of the report stays in English.

## Personal data

`--redact` masks quoted literals, bound parameters and constraint key values in every entry
before the analysis, so neither the traces nor the rest of the report show them. `--offline`
removes the HTML report's buttons that send a plan to explain.dalibo.com.

## Log formats

stderr (and the syslog and cloud variants quellog reads), jsonlog and csvlog, plain or in
archives. jsonlog and csvlog carry `leader_pid`, which ties parallel workers to their leader;
stderr has no labelled leader, so workers are found through the statement their temp files
report. syslog flattens multi-line messages: a plan is cut back into lines, but its indentation
is rebuilt, not the original (the trace says so).

## What it needs in the logs

- `log_min_duration_statement`: only timed statements can be traced.
- `%p` in `log_line_prefix` (or jsonlog/csvlog): everything is tied to the backend PID.
- `log_temp_files`: temp files, and through them the parallel workers.
- `auto_explain` (`log_analyze` for actual rows and times): the plan and the time split.

## Reading a trace

**Where the time went.** auto_explain logs the executor time; the statement line logs the total.
An SQL `EXECUTE` (drivers such as Qt QPSQL send `PREPARE` / `EXECUTE` as SQL) stores its whole
result before sending it, so the gap between the two is the time spent sending rows to the client
— a slow network or a client reading slowly — and the backend's last temp file is the stored
result. For a plain statement, rows are sent while the executor runs and that time is already in
the executor share. PostgreSQL never logs the network time itself: this split is the closest the
logs get.

**Query and replay.** The prepared text comes from `DETAIL: prepare:` (written right after an SQL
`EXECUTE`), from auto_explain's `Query Text`, or from the statement. Parameters come from
`DETAIL: parameters:`, auto_explain's `Query Parameters` or the `EXECUTE` arguments, and are
substituted into the query shown for `EXPLAIN`. `EXPLAIN` alone only plans the query;
`EXPLAIN ANALYZE` runs it.

**Plan and findings.** The plan is shown as PostgreSQL wrote it. Findings point at a large result
sent to the client, sorts and hashes that spilled to disk, scans that read far more rows than they
keep (with the filter columns to check for an index), estimates off by more than 10×, workers
planned but not launched, and conditions repeated in a filter.

**Other plans on this backend.** A plan logged by the same backend during the window whose
`Query Text` is another statement — typically a statement slower than
`auto_explain.log_min_duration` but faster than `log_min_duration_statement`. It is listed apart
so it is not read as the traced statement's plan.
