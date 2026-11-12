package trace

import (
	"strings"
	"testing"

	"github.com/Alain-L/quellog/parser"
)

// traceFile reads a fixture in one pass and returns the traces. keepLines
// mirrors the trace command; without it the stream is the one every report
// analyses (plans keep their lines either way).
func traceFile(t *testing.T, path string, opt Options, keepLines bool) []*Trace {
	t.Helper()
	parser.PreserveContinuationNewlines = keepLines
	defer func() { parser.PreserveContinuationNewlines = false }()

	if opt.Top == 0 {
		opt.Top = 1
	}
	tr := NewTracer(opt)
	ch := make(chan []parser.LogEntry, 16)
	errc := make(chan error, 1)
	go func() { errc <- parser.ParseFile(path, ch); close(ch) }()
	var seq int64
	for batch := range ch {
		for i := range batch {
			seq++
			batch[i].Seq = seq
			tr.Process(&batch[i])
		}
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	return tr.Finalize()
}

// A slow SQL EXECUTE, replayed on a test instance with no real data: a Qt
// client runs PREPARE/EXECUTE in SQL, the sort goes parallel and spills, the
// client reads slowly. Before the EXECUTE, the same backend logged the plan of
// another statement that was too fast for log_min_duration_statement.
func TestTraceSQLExecute(t *testing.T) {
	for _, keep := range []bool{true, false} {
		checkLonProdTrace(t, traceFile(t, "testdata/slow_execute.log", Options{}, keep), true)
	}
}

// Same session written by log_destination = jsonlog: DETAIL and STATEMENT are
// carried inside the entry, plans keep their newlines without tabs.
func TestTraceSQLExecuteJSON(t *testing.T) {
	checkLonProdTrace(t, traceFile(t, "testdata/slow_execute.json", Options{}, false), false)
}

// Same session written by log_destination = csvlog: the statement comes as
// QUERY, the workers carry leader_pid.
func TestTraceSQLExecuteCSV(t *testing.T) {
	checkLonProdTrace(t, traceFile(t, "testdata/slow_execute.csv", Options{}, false), false)
}

func checkLonProdTrace(t *testing.T, traces []*Trace, foreignPlan bool) {
	t.Helper()
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(traces))
	}
	tr := traces[0]

	if tr.PID != "83" || tr.DurationMs != 228013.837 {
		t.Errorf("execution = pid %s, %v ms", tr.PID, tr.DurationMs)
	}
	if tr.DB != "appdb" || tr.User != "app_user" {
		t.Errorf("session = db %q user %q", tr.DB, tr.User)
	}
	if !strings.HasPrefix(tr.Prepared, "PREPARE qpsqlpstmt_1001 AS SELECT") {
		t.Errorf("prepared text not taken from DETAIL: %q", tr.Prepared)
	}
	if tr.Plan == nil || !strings.HasPrefix(tr.Plan.QueryText, "PREPARE qpsqlpstmt_1001") {
		t.Fatalf("wrong plan attached: %+v", tr.Plan)
	}
	if tr.Plan.DurationMs != 2118.349 || !strings.HasPrefix(tr.Plan.Lines[0], "Gather Merge") || tr.Plan.Rebuilt {
		t.Errorf("plan = %v ms, first line %q, rebuilt %v", tr.Plan.DurationMs, tr.Plan.Lines[0], tr.Plan.Rebuilt)
	}
	if foreignPlan && (len(tr.ForeignPlans) != 1 || !strings.Contains(tr.ForeignPlans[0].QueryText, "ORDER_LINES")) {
		t.Errorf("foreign plan not set aside: %+v", tr.ForeignPlans)
	}
	if got := tr.Workers(); len(got) != 5 {
		t.Errorf("workers = %v, want 5", got)
	}
	if m := tr.StoredResult(); m == nil || m.Bytes != 963900000 {
		t.Errorf("stored result = %+v", m)
	}
	if s := tr.TimeSplit(); s.Verdict != "client" || !strings.Contains(s.Explanation, "stored result was") {
		t.Errorf("time split = %+v", s)
	}
	q := tr.Query()
	if !strings.HasPrefix(q, `SELECT * FROM "SHIPMENTS"`) || strings.Contains(q, "\t") {
		t.Errorf("query = %q", q)
	}
	if !strings.HasPrefix(tr.Parameters, "$1 = '1'") {
		t.Errorf("parameters = %q", tr.Parameters)
	}

	fs := tr.Findings()
	kinds := map[string]Finding{}
	for _, f := range fs {
		kinds[f.Kind] = f
	}
	if len(fs) == 0 || fs[0].Kind != "client" {
		t.Fatalf("first finding = %+v, want the transfer to the client", fs)
	}
	if c := fs[0]; c.Weight < 0.9 || !strings.Contains(c.Why, "Mbit/s") || !strings.Contains(c.Proposal, "setForwardOnly") {
		t.Errorf("client finding = %+v", c)
	}
	sort, ok := kinds["sort"]
	if !ok || !strings.Contains(sort.Impact, "not the cause") || !strings.Contains(sort.Proposal, "work_mem = '512MB'") || !strings.Contains(sort.Proposal, "route_id, shipment_id") {
		t.Errorf("sort finding = %+v", sort)
	}
	if _, ok := kinds["duplicate"]; !ok {
		t.Errorf("duplicate condition not found in %+v", fs)
	}
	if _, ok := kinds["result"]; ok {
		t.Errorf("large result reported apart from the transfer: %+v", fs)
	}
}

func TestQueryMatchesPreparedText(t *testing.T) {
	traces := traceFile(t, "testdata/slow_execute.log", Options{Contains: "shipments"}, false)
	if len(traces) != 1 || traces[0].PID != "83" {
		t.Fatalf("--query on the prepared text found %d traces", len(traces))
	}
	if got := traceFile(t, "testdata/slow_execute.log", Options{Contains: "no_such_table"}, false); len(got) != 0 {
		t.Errorf("--query matched %d unrelated traces", len(got))
	}
}

// syslog flattens continuation lines: the plan is cut back into lines.
func TestFlatPlan(t *testing.T) {
	p := ParsePlan(`duration: 2118.349 ms  plan: Query Text: PREPARE s AS SELECT * FROM t ORDER BY a Gather Merge  (cost=1.00..2.00 rows=10 width=8) (actual time=1.0..2.0 rows=10 loops=1) Workers Planned: 2 Workers Launched: 2 ->  Sort  (cost=1.00..1.50 rows=5 width=8) (actual time=1.0..1.5 rows=5 loops=3) Sort Key: a Sort Method: external merge  Disk: 2048kB ->  Parallel Seq Scan on t  (cost=0.00..1.00 rows=5 width=8) (actual time=0.1..0.5 rows=5 loops=3)`)
	if p == nil || !p.Rebuilt {
		t.Fatalf("plan = %+v", p)
	}
	if p.QueryText != "PREPARE s AS SELECT * FROM t ORDER BY a" {
		t.Errorf("query text = %q", p.QueryText)
	}
	if len(p.Nodes) != 3 || p.Nodes[0].Name != "Gather Merge" || p.Nodes[2].Name != "Parallel Seq Scan on t" {
		for _, n := range p.Nodes {
			t.Logf("node %q", n.Name)
		}
		t.Errorf("got %d nodes", len(p.Nodes))
	}
}

func TestSeverityAt(t *testing.T) {
	for msg, want := range map[string]string{
		"[83] 00000: db=a,user=b LOG:  duration: 1 ms":    "LOG",
		"[83] db=a LOG:  00000: duration: 1 ms":           "LOG",
		"[83] db=a ERROR:  relation \"x\" does not exist": "ERROR",
		"[83] db=a ERROR:  42P01: relation":               "ERROR",
	} {
		if got := severityAt(msg, bodyOffset(msg)); got != want {
			t.Errorf("severityAt(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestSeqScanFinding(t *testing.T) {
	p := ParsePlan("duration: 3012.114 ms  plan:\n\tQuery Text: SELECT 1\n" +
		"\tSeq Scan on \"ORDER_LINES\"  (cost=0.00..117661.00 rows=248 width=36) (actual time=3.010..3011.950 rows=121 loops=1)\n" +
		"\t  Filter: ((load_ref)::text = '00091505'::text)\n" +
		"\t  Rows Removed by Filter: 6184449")
	tr := &Trace{DurationMs: 3100, Statement: "SELECT 1", Plan: p}
	fs := tr.Findings()
	if len(fs) != 1 {
		t.Fatalf("findings = %+v", fs)
	}
	f := fs[0]
	if !strings.Contains(f.Title, "reads 6.2 M rows to keep 121") || f.Weight < 0.9 {
		t.Errorf("title %q, weight %v", f.Title, f.Weight)
	}
	if !strings.Contains(f.Why, "casts load_ref to text") {
		t.Errorf("why = %q", f.Why)
	}
	if len(f.Checks) != 1 || !strings.Contains(f.Checks[0], "tablename = 'ORDER_LINES'") || !strings.Contains(f.Checks[0], "'load_ref'") {
		t.Errorf("checks = %q", f.Checks)
	}
	if !strings.Contains(f.Proposal, `CREATE INDEX CONCURRENTLY ON "ORDER_LINES" (load_ref)`) {
		t.Errorf("proposal = %q", f.Proposal)
	}
}

// The traces speak French with --lang fr: texts, units and decimals.
func TestFrench(t *testing.T) {
	if err := SetLang("fr"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = SetLang("en") }()
	traces := traceFile(t, "testdata/slow_execute.log", Options{}, false)
	fs := traces[0].Findings()
	if !strings.HasPrefix(fs[0].Title, "L'envoi du résultat a pris") || !strings.Contains(fs[0].Why, "Mo") {
		t.Errorf("client finding = %+v", fs[0])
	}
	if s := traces[0].TimeSplit(); !strings.Contains(s.Explanation, "2,1 s") {
		t.Errorf("explanation = %q", s.Explanation)
	}
	if err := SetLang("de"); err == nil {
		t.Error("SetLang accepted an unsupported language")
	}
}

func TestExecuteArguments(t *testing.T) {
	got := executeArguments("EXECUTE s1 (1, 'a,b', ARRAY[1,2], f(3, 4));")
	want := "$1 = 1, $2 = 'a,b', $3 = ARRAY[1,2], $4 = f(3, 4)"
	if got != want {
		t.Errorf("executeArguments = %q, want %q", got, want)
	}
}
