package analysis

import (
	"strings"

	"github.com/Alain-L/quellog/parser"
	"github.com/Alain-L/quellog/trace"
)

// TraceOptions chooses the statements every analysis traces: by default the
// three slowest. Set before building the analyzer (the CLI's --trace-top);
// a Top of zero turns tracing off.
var TraceOptions = trace.Options{Top: 3}

// StampBodyOffset sets entry.BodyOffset as the streaming analyzer does, for
// callers that feed a trace.Tracer directly.
func StampBodyOffset(e *parser.LogEntry) {
	e.BodyOffset = messageBodyOffset(e.Message)
}

// flattenKeptPlan gives the SQL analyzer an auto_explain plan from a stderr
// log as it always saw it: the stderr parser now keeps a plan's lines (for
// the traces), and this joins them back the way it used to, one space and
// trimmed, so the per-query plan and every report stay as they were.
// jsonlog and csvlog plans ("plan:\nQuery Text:") never had the tab.
func flattenKeptPlan(msg string) string {
	i := strings.Index(msg, "plan:\n\t")
	if i < 0 {
		return msg
	}
	var b strings.Builder
	b.Grow(len(msg))
	lines := strings.Split(msg, "\n")
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteByte(' ')
		b.WriteString(strings.TrimRight(strings.TrimLeft(l, " \t"), " \t\r"))
	}
	return b.String()
}

// TraceQueryID returns the ID the SQL analysis gives the traced statement
// ("se-…", "xe-…"), so reports can link a trace to its query. ok is false
// when the query is not in the SQL metrics.
func TraceQueryID(sql SQLMetrics, t *trace.Trace) (id string, ok bool) {
	raw := normalizeWhitespace(strings.TrimSpace(t.LoggedText))
	norm := normalizeQuery(raw)
	if _, ok := sql.QueryStats[norm]; !ok {
		return "", false
	}
	id, _ = GenerateQueryID(raw, norm)
	return id, true
}

// skipToPlanNode drops what auto_explain prints before the plan when the
// lines are kept (jsonlog, csvlog): the rest of a multi-line Query Text and
// the Query Parameters line. The first node carries "(cost=" (text format)
// or opens the JSON plan. Without such a line the text is kept as it was.
func skipToPlanNode(text string) string {
	rest := text
	for rest != "" {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i]
		}
		t := strings.TrimLeft(line, " \t")
		if strings.Contains(t, "(cost=") || strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			return strings.TrimLeft(rest, " \n\t")
		}
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[i+1:]
		} else {
			break
		}
	}
	return strings.TrimLeft(text, " \n\t")
}
