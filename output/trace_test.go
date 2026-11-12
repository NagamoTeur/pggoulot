package output

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Alain-L/quellog/analysis"
	"github.com/Alain-L/quellog/parser"
	"github.com/Alain-L/quellog/trace"
)

// The slow EXECUTE replay in its three formats goes through the full
// analysis: every report must carry the same trace.
func TestTracesInEveryReport(t *testing.T) {
	for _, f := range []string{"slow_execute.log", "slow_execute.json", "slow_execute.csv"} {
		t.Run(f, func(t *testing.T) {
			ch := make(chan []parser.LogEntry, 16)
			go func() {
				if err := parser.ParseFile("../trace/testdata/"+f, ch); err != nil {
					t.Error(err)
				}
				close(ch)
			}()
			m := analysis.AggregateMetrics(context.Background(), ch)
			// The JSON and CSV replays also time the ORDER_LINES
			// statement: the 7 min EXECUTE comes first.
			if len(m.Traces) == 0 {
				t.Fatal("no trace")
			}

			data := buildJSONData(m, []string{"all"}, true)
			raw, err := json.Marshal(data["traces"])
			if err != nil {
				t.Fatal(err)
			}
			var traces []TraceJSON
			if err := json.Unmarshal(raw, &traces); err != nil {
				t.Fatal(err)
			}
			tr := traces[0]
			if tr.PID != "83" || tr.TimeSplit.Verdict != "client" || tr.Plan == nil || len(tr.Workers) != 5 {
				t.Errorf("trace = pid %s, verdict %s, plan %v, workers %v", tr.PID, tr.TimeSplit.Verdict, tr.Plan != nil, tr.Workers)
			}
			if !strings.HasPrefix(tr.QueryID, "xe-") {
				t.Errorf("query id = %q, want the EXECUTE's xe- id", tr.QueryID)
			}
			if !strings.Contains(tr.Query, `"SHIPMENTS"`) || strings.Contains(tr.Query, "$1") {
				t.Errorf("query without its parameters: %q", tr.Query)
			}
			stored := 0
			for _, f := range tr.TempFiles {
				if f.StoredResult {
					stored++
				}
			}
			if stored != 1 {
				t.Errorf("%d temp files marked as the stored result", stored)
			}

			var text bytes.Buffer
			RenderTrace(&text, m.Traces[0], false)
			for _, want := range []string{"WHERE THE TIME WENT", "After the executor", "stored result", "EXPLAIN (BUFFERS, SETTINGS)", "FINDINGS"} {
				if !strings.Contains(text.String(), want) {
					t.Errorf("text trace lacks %q", want)
				}
			}
			var md strings.Builder
			writeTracesMarkdown(&md, m.Traces, true)
			for _, want := range []string{"## Slowest statements traced", "```sql", "**PLAN", "(stored result)", "*To check (read-only)*"} {
				if !strings.Contains(md.String(), want) {
					t.Errorf("markdown lacks %q", want)
				}
			}
		})
	}
}

// --lang fr: every rendering of the traces speaks French.
func TestTracesInFrench(t *testing.T) {
	if err := trace.SetLang("fr"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = trace.SetLang("en") }()
	ch := make(chan []parser.LogEntry, 16)
	go func() {
		_ = parser.ParseFile("../trace/testdata/slow_execute.log", ch)
		close(ch)
	}()
	m := analysis.AggregateMetrics(context.Background(), ch)
	var text bytes.Buffer
	RenderTrace(&text, m.Traces[0], false)
	for _, want := range []string{"OÙ EST PASSÉ LE TEMPS", "CONSTATS", "Pourquoi : ", "À vérifier (lecture seule) :", "Mbit/s", "Mo"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("French trace lacks %q", want)
		}
	}
	data := buildJSONData(m, []string{"all"}, true)
	if data["traces_lang"] != "fr" {
		t.Errorf("traces_lang = %v", data["traces_lang"])
	}
}
