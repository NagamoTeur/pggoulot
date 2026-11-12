package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Alain-L/quellog/analysis"
	"github.com/Alain-L/quellog/trace"
)

// Slow statement traces (fork): the same trace in every format. RenderTrace
// is the full text form (quellog trace, --traces, --full), printTracesSummary
// the compact block of the default report, writeTracesMarkdown the Markdown
// form and convertTraces what JSON, YAML and the HTML report carry.

// Offline (--offline) marks the HTML report so it hides the buttons that send
// data to an outside service (the explain.dalibo.com Visualize buttons).
var Offline bool

// traceQuery is the query that ran, parameters substituted.
func traceQuery(t *trace.Trace) string {
	q := t.Query()
	if t.Parameters != "" {
		q = SubstituteParameters(q, t.Parameters)
	}
	return q
}

// traceReplay is the statement to paste to see the plan without running it.
func traceReplay(t *trace.Trace) string {
	return "EXPLAIN (BUFFERS, SETTINGS)\n" + traceQuery(t) + ";"
}

func traceSession(t *trace.Trace) string {
	var who []string
	for _, kv := range [][2]string{{"db", t.DB}, {"user", t.User}, {"app", t.App}, {"client", t.Client}} {
		if kv[1] != "" {
			who = append(who, kv[0]+"="+kv[1])
		}
	}
	return strings.Join(who, "  ")
}

func clockTime(t interface{ Format(string) string }) string { return t.Format("2006-01-02 15:04:05") }

// RenderTrace writes a trace as text, in the language of the traces. color
// adds ANSI bold and grey.
func RenderTrace(w io.Writer, t *trace.Trace, color bool) {
	bold, grey, reset := "", "", ""
	if color {
		bold, grey, reset = "\033[1m", "\033[38;5;243m", "\033[0m"
	}
	T := trace.T
	section := func(title string) { fmt.Fprintf(w, "\n%s%s%s\n\n", bold, title, reset) }
	field := func(label, value string) { fmt.Fprintf(w, "  %-11s: %s\n", label, value) }

	fmt.Fprintf(w, "%s%s  %s  pid %s%s\n\n", bold, T("label.trace"), trace.HumanDuration(t.DurationMs), t.PID, reset)
	if s := traceSession(t); s != "" {
		field(T("label.session"), s)
	}
	field(T("label.statement"), oneLine(t.Statement, 160))
	field(T("label.window"), clockTime(t.Start())+" → "+clockTime(t.End))
	if ws := t.Workers(); len(ws) > 0 {
		field(T("label.workers"), strings.Join(ws, ", ")+" ("+T("label.workers.note")+")")
	}

	section(T("label.time"))
	split := t.TimeSplit()
	if split.Known && t.DurationMs > 0 {
		fmt.Fprintf(w, "  %-26s %10s  %s\n", T("label.executor"), trace.HumanDuration(split.ExecutorMs), trace.Percent(split.ExecutorMs/t.DurationMs))
		fmt.Fprintf(w, "  %-26s %10s  %s\n\n", T("label.after"), trace.HumanDuration(split.AfterMs), trace.Percent(split.AfterMs/t.DurationMs))
	}
	fmt.Fprintln(w, wrap(split.Explanation, 78, "  "))

	if fs := t.Findings(); len(fs) > 0 {
		section(T("label.findings"))
		for i, f := range fs {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "  %s• %s%s\n", bold, f.Title, reset)
			if f.Why != "" {
				fmt.Fprintf(w, "%s\n", wrap(T("label.why")+T("label.sep")+f.Why, 78, "    "))
			}
			if f.Impact != "" {
				fmt.Fprintf(w, "%s\n", wrap(T("label.impact")+T("label.sep")+f.Impact, 78, "    "))
			}
			if len(f.Checks) > 0 {
				fmt.Fprintf(w, "    %s%s\n", T("label.check"), strings.TrimRight(T("label.sep"), " "))
				for _, c := range f.Checks {
					fmt.Fprintf(w, "%s%s%s\n", grey, indentLines(c, "      "), reset)
				}
			}
			if f.Proposal != "" {
				fmt.Fprintf(w, "%s\n", wrap(T("label.proposal")+T("label.sep")+f.Proposal, 78, "    "))
			}
		}
	}

	section(T("label.query"))
	fmt.Fprintln(w, indentLines(traceQuery(t), "  "))
	if t.Parameters != "" {
		fmt.Fprintf(w, "\n  %s%s: %s%s\n", grey, T("label.parameters"), oneLine(t.Parameters, 300), reset)
	}
	if t.Prepared != "" {
		fmt.Fprintf(w, "  %s%s%s\n", grey, T("label.prepared"), reset)
	}

	section(T("label.replay"))
	fmt.Fprintf(w, "%s\n\n", indentLines(traceReplay(t), "  "))
	fmt.Fprintf(w, "%s%s%s\n", grey, wrap(T("label.replay.note"), 78, "  "), reset)

	if t.Plan != nil {
		section(T("label.plan", clockTime(t.Plan.Time), trace.HumanDuration(t.Plan.DurationMs)))
		for _, l := range t.Plan.Lines {
			fmt.Fprintf(w, "  %s\n", l)
		}
		if t.Plan.Rebuilt {
			fmt.Fprintf(w, "\n  %s%s%s\n", grey, T("label.plan.rebuilt"), reset)
		}
	}

	if len(t.ForeignPlans) > 0 {
		section(T("label.foreign"))
		for _, p := range t.ForeignPlans {
			fmt.Fprintf(w, "  %s\n", T("label.foreign.line", clockTime(p.Time), trace.HumanDuration(p.DurationMs), oneLine(p.QueryText, 100)))
		}
		fmt.Fprintf(w, "\n%s%s%s\n", grey, wrap(T("label.foreign.note"), 78, "  "), reset)
	}

	if len(t.TempFiles) > 0 {
		stored := t.StoredResult()
		section(T("label.temp", len(t.TempFiles), trace.HumanBytes(t.TempBytes())))
		for i := range t.TempFiles {
			f := &t.TempFiles[i]
			note := ""
			if stored != nil && f.Seq == stored.Seq {
				note = "  " + T("label.temp.stored")
			}
			fmt.Fprintf(w, "  %s  pid %-7s %-8s %10s  %s%s%s%s\n", clockTime(f.Time), f.PID, f.Role, trace.HumanBytes(f.Bytes), grey, f.Text, reset, note)
		}
	}

	section(T("label.timeline"))
	for _, ev := range t.Events {
		fmt.Fprintf(w, "  %s  pid %-7s %-10s %s\n", clockTime(ev.Time), ev.PID, ev.Kind, oneLine(ev.Text, 110))
	}
	fmt.Fprintln(w)
}

// printTracesSummary is the compact block of the default text report: one
// paragraph per traced statement, the full trace being one command away.
func printTracesSummary(w io.Writer, traces []*trace.Trace, bold, reset string) {
	T := trace.T
	fmt.Fprintf(w, "%s\n%s%s\n\n", bold, T("label.traced"), reset)
	for i, t := range traces {
		split := t.TimeSplit()
		fmt.Fprintf(w, "  %d. %s  pid %s  %s", i+1, trace.HumanDuration(t.DurationMs), t.PID, T("label.ended", clockTime(t.End)))
		if s := traceSession(t); s != "" {
			fmt.Fprintf(w, "  %s", s)
		}
		fmt.Fprintf(w, "\n     %s\n", oneLine(traceQuery(t), 110))
		if split.Known {
			fmt.Fprintf(w, "     %s\n", T("label.split.short", trace.HumanDuration(split.ExecutorMs), trace.HumanDuration(split.AfterMs)))
		}
		for _, f := range t.Findings() {
			fmt.Fprintf(w, "     • %s\n", f.Title)
		}
		if len(t.TempFiles) > 0 {
			fmt.Fprintf(w, "     %s\n", T("label.temp.short", len(t.TempFiles), trace.HumanBytes(t.TempBytes())))
		}
		fmt.Fprintf(w, "     %s\n\n", T("label.full", t.PID))
	}
}

// writeTracesMarkdown writes the traces; full adds plan, temp files and
// timeline.
func writeTracesMarkdown(b *strings.Builder, traces []*trace.Trace, full bool) {
	T := trace.T
	fmt.Fprintf(b, "## %s\n\n", T("label.md.section"))
	for i, t := range traces {
		split := t.TimeSplit()
		fmt.Fprintf(b, "### %d. %s, pid %s, %s\n\n", i+1, trace.HumanDuration(t.DurationMs), t.PID, T("label.ended", clockTime(t.End)))
		if s := traceSession(t); s != "" {
			fmt.Fprintf(b, "- **%s**: %s\n", T("label.session"), strings.ReplaceAll(s, "  ", ", "))
		}
		fmt.Fprintf(b, "- **%s**: %s → %s\n", T("label.window"), clockTime(t.Start()), clockTime(t.End))
		if ws := t.Workers(); len(ws) > 0 {
			fmt.Fprintf(b, "- **%s**: %s\n", T("label.md.parallel"), strings.Join(ws, ", "))
		}
		if split.Known {
			fmt.Fprintf(b, "- %s\n", T("label.split.short", trace.HumanDuration(split.ExecutorMs), trace.HumanDuration(split.AfterMs)))
		}
		fmt.Fprintf(b, "\n%s\n\n", split.Explanation)
		if fs := t.Findings(); len(fs) > 0 {
			fmt.Fprintf(b, "**%s**\n\n", strings.ToUpper(T("label.findings")[:1])+strings.ToLower(T("label.findings")[1:]))
			for _, f := range fs {
				fmt.Fprintf(b, "- **%s**\n", f.Title)
				for _, kv := range [][2]string{{T("label.why"), f.Why}, {T("label.impact"), f.Impact}, {T("label.proposal"), f.Proposal}} {
					if kv[1] != "" {
						fmt.Fprintf(b, "  - *%s*%s%s\n", kv[0], T("label.sep"), kv[1])
					}
				}
				for _, c := range f.Checks {
					fmt.Fprintf(b, "  - *%s*%s\n\n    ```sql\n%s\n    ```\n", T("label.check"), strings.TrimRight(T("label.sep"), " "), indentLines(c, "    "))
				}
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "```sql\n%s\n```\n\n", traceQuery(t))
		if t.Parameters != "" {
			fmt.Fprintf(b, "%s: `%s`\n\n", T("label.parameters"), t.Parameters)
		}
		if !full {
			continue
		}
		fmt.Fprintf(b, "**%s**\n\n```sql\n%s\n```\n\n%s\n\n", T("label.replay"), traceReplay(t), T("label.replay.note"))
		if t.Plan != nil {
			fmt.Fprintf(b, "**%s**\n\n```\n%s\n```\n\n", T("label.plan", clockTime(t.Plan.Time), trace.HumanDuration(t.Plan.DurationMs)), strings.Join(t.Plan.Lines, "\n"))
		}
		for _, p := range t.ForeignPlans {
			fmt.Fprintf(b, "- %s\n", T("label.md.other", clockTime(p.Time), trace.HumanDuration(p.DurationMs), oneLine(p.QueryText, 100)))
		}
		if len(t.TempFiles) > 0 {
			stored := t.StoredResult()
			fmt.Fprintf(b, "\n**%s**\n\n%s\n|---|---|---|---:|---|\n", T("label.temp", len(t.TempFiles), trace.HumanBytes(t.TempBytes())), T("label.md.temp.head"))
			for i := range t.TempFiles {
				f := &t.TempFiles[i]
				path := f.Text
				if stored != nil && f.Seq == stored.Seq {
					path += " (" + T("label.stored") + ")"
				}
				fmt.Fprintf(b, "| %s | %s | %s | %s | `%s` |\n", clockTime(f.Time), f.PID, f.Role, trace.HumanBytes(f.Bytes), path)
			}
		}
		fmt.Fprintf(b, "\n**%s**\n\n%s\n|---|---|---|---|\n", T("label.timeline"), T("label.md.time.head"))
		for _, ev := range t.Events {
			fmt.Fprintf(b, "| %s | %s | %s | %s |\n", clockTime(ev.Time), ev.PID, ev.Kind, strings.ReplaceAll(oneLine(ev.Text, 140), "|", "\\|"))
		}
		b.WriteString("\n")
	}
}

// TraceJSON is a trace as JSON, YAML and the HTML report carry it.
type TraceJSON struct {
	PID          string          `json:"pid"`
	QueryID      string          `json:"query_id,omitempty"` // the query in sql_performance
	Start        string          `json:"start"`
	End          string          `json:"end"`
	DurationMs   float64         `json:"duration_ms"`
	Kind         string          `json:"kind"`
	Statement    string          `json:"statement"`
	Database     string          `json:"database,omitempty"`
	User         string          `json:"user,omitempty"`
	App          string          `json:"app,omitempty"`
	Client       string          `json:"client,omitempty"`
	Prepared     string          `json:"prepared,omitempty"`
	Parameters   string          `json:"parameters,omitempty"`
	Query        string          `json:"query"`
	Replay       string          `json:"replay"`
	TimeSplit    TraceSplitJSON  `json:"time_split"`
	Plan         *TracePlanJSON  `json:"plan,omitempty"`
	Findings     []FindingJSON   `json:"findings,omitempty"`
	ForeignPlans []TracePlanJSON `json:"foreign_plans,omitempty"`
	Workers      []string        `json:"workers,omitempty"`
	TempFiles    []TraceTempJSON `json:"temp_files,omitempty"`
	TempBytes    int64           `json:"temp_bytes,omitempty"`
	Events       []TraceEvJSON   `json:"events"`
}

type TraceSplitJSON struct {
	Known       bool    `json:"known"`
	ExecutorMs  float64 `json:"executor_ms,omitempty"`
	AfterMs     float64 `json:"after_ms,omitempty"`
	Verdict     string  `json:"verdict"`
	Explanation string  `json:"explanation"`
}

type TracePlanJSON struct {
	Time       string   `json:"time"`
	DurationMs float64  `json:"duration_ms"`
	QueryText  string   `json:"query_text,omitempty"`
	Lines      []string `json:"lines,omitempty"`
	Rebuilt    bool     `json:"rebuilt,omitempty"`
}

type FindingJSON struct {
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Why      string   `json:"why,omitempty"`
	Impact   string   `json:"impact,omitempty"`
	Weight   float64  `json:"weight"` // share of the duration, -1 if unknown
	Checks   []string `json:"checks,omitempty"`
	Proposal string   `json:"proposal,omitempty"`
}

type TraceTempJSON struct {
	Time         string `json:"time"`
	PID          string `json:"pid"`
	Role         string `json:"role"`
	Bytes        int64  `json:"bytes"`
	Path         string `json:"path"`
	StoredResult bool   `json:"stored_result,omitempty"`
}

type TraceEvJSON struct {
	Time string `json:"time"`
	PID  string `json:"pid"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func tracePlanJSON(p *trace.Plan, withLines bool) TracePlanJSON {
	j := TracePlanJSON{Time: p.Time.Format("2006-01-02T15:04:05"), DurationMs: p.DurationMs, QueryText: p.QueryText, Rebuilt: p.Rebuilt}
	if withLines {
		j.Lines = p.Lines
	}
	return j
}

func convertTraces(m analysis.AggregatedMetrics) []TraceJSON {
	out := make([]TraceJSON, 0, len(m.Traces))
	for _, t := range m.Traces {
		split := t.TimeSplit()
		j := TraceJSON{
			PID: t.PID, Start: t.Start().Format("2006-01-02T15:04:05"), End: t.End.Format("2006-01-02T15:04:05"),
			DurationMs: t.DurationMs, Kind: t.Kind, Statement: t.Statement,
			Database: t.DB, User: t.User, App: t.App, Client: t.Client,
			Prepared: t.Prepared, Parameters: t.Parameters,
			Query: traceQuery(t), Replay: traceReplay(t),
			TimeSplit: TraceSplitJSON{Known: split.Known, ExecutorMs: split.ExecutorMs, AfterMs: split.AfterMs, Verdict: split.Verdict, Explanation: split.Explanation},
			Workers:   t.Workers(), TempBytes: t.TempBytes(), Events: []TraceEvJSON{},
		}
		j.QueryID, _ = analysis.TraceQueryID(m.SQL, t)
		if t.Plan != nil {
			p := tracePlanJSON(t.Plan, true)
			j.Plan = &p
		}
		for _, f := range t.Findings() {
			j.Findings = append(j.Findings, FindingJSON{Kind: f.Kind, Title: f.Title, Why: f.Why, Impact: f.Impact, Weight: f.Weight, Checks: f.Checks, Proposal: f.Proposal})
		}
		for _, p := range t.ForeignPlans {
			j.ForeignPlans = append(j.ForeignPlans, tracePlanJSON(p, false))
		}
		stored := t.StoredResult()
		for i := range t.TempFiles {
			f := &t.TempFiles[i]
			j.TempFiles = append(j.TempFiles, TraceTempJSON{
				Time: f.Time.Format("2006-01-02T15:04:05"), PID: f.PID, Role: f.Role, Bytes: f.Bytes, Path: f.Text,
				StoredResult: stored != nil && f.Seq == stored.Seq,
			})
		}
		for _, ev := range t.Events {
			j.Events = append(j.Events, TraceEvJSON{Time: ev.Time.Format("2006-01-02T15:04:05"), PID: ev.PID, Kind: ev.Kind, Text: ev.Text})
		}
		out = append(out, j)
	}
	return out
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func indentLines(s, pad string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		lines[i] = pad + strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// wrap breaks a paragraph at width columns, each line starting with pad.
func wrap(s string, width int, pad string) string {
	var lines []string
	line := pad
	for _, word := range strings.Fields(s) {
		if len(line) > len(pad) && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = pad
		}
		if len(line) > len(pad) {
			line += " "
		}
		line += word
	}
	return strings.Join(append(lines, line), "\n")
}
