package trace

import (
	"strings"
	"testing"
	"time"

	"github.com/Alain-L/quellog/parser"
)

// The tracer reads whatever the logs hold: none of its parsers may panic.

func FuzzSplit(f *testing.F) {
	for _, s := range []string{
		"[83] 00000: db=a,user=b,app=c,client=d LOG:  duration: 1.0 ms  statement: SELECT 1",
		"[83]: user=a,db=b LOG: temporary file: path \"x\", size 1 STATEMENT: SELECT 1",
		"[83]: LOG: duration: 1 ms statement: x DETAIL: prepare: y QUERY: z SQLSTATE = '00000'",
		"DETAIL:  parameters: $1 = '1'",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m := Split(s)
		_ = severityAt(s, bodyOffset(s))
		_, _, _, _ = parseDuration(m.Body)
		_ = mentionsPID(s, "83")
		_ = executeArguments(s)
	})
}

func FuzzParsePlan(f *testing.F) {
	for _, s := range []string{
		"duration: 1 ms  plan:\n\tQuery Text: SELECT 1\n\tSeq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.1..0.2 rows=1 loops=1)\n\t  Filter: ((a)::text = 'x'::text)\n\t  Rows Removed by Filter: 500000",
		"duration: 2 ms  plan: Query Text: SELECT 1 Gather Merge  (cost=1.00..2.00 rows=10 width=8) ->  Sort  (cost=1.00..1.50 rows=5 width=8) Sort Method: external merge  Disk: 2048kB",
		"duration: 3 ms  plan:",
	} {
		f.Add(s, 1000.0)
	}
	f.Fuzz(func(t *testing.T, body string, total float64) {
		p := ParsePlan(body)
		if p == nil {
			return
		}
		tr := &Trace{DurationMs: total, Statement: "EXECUTE qpsqlpstmt_1 (1)", Plan: p, End: time.Unix(0, 0)}
		_ = tr.TimeSplit()
		_ = tr.Findings()
		_ = tr.Query()
	})
}

// FuzzTracer feeds arbitrary lines, each split into a stream of entries.
func FuzzTracer(f *testing.F) {
	f.Add("[83] LOG:  temporary file: path \"a\", size 10\n[83] STATEMENT:  SELECT 1\n[83] LOG:  duration: 5000.0 ms  statement: SELECT 1\n[83] DETAIL:  parameters: $1 = 'x'")
	f.Add("[84] LOG:  duration: 10.0 ms  plan:\n[84] LOG:  process 84 still waiting for ShareLock after 1000.0 ms\n[84] LOG:  duration: 20 ms  execute s1: SELECT 2\n[84] LOG:  disconnection: session time")
	f.Fuzz(func(t *testing.T, stream string) {
		tr := NewTracer(Options{Top: 2})
		base := time.Unix(1700000000, 0)
		for i, line := range strings.Split(stream, "\n") {
			e := parser.NewLogEntry(base.Add(time.Duration(i)*time.Second), line, strings.Contains(line, "DETAIL:") || strings.Contains(line, "STATEMENT:"))
			e.Seq = int64(i + 1)
			if i%3 == 0 {
				e.LeaderPID = "83"
			}
			tr.Process(&e)
		}
		for _, t := range tr.Finalize() {
			_ = t.Findings()
			_ = t.Workers()
		}
	})
}
