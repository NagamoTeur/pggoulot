package analysis

import (
	"strings"
	"testing"

	"github.com/Alain-L/quellog/parser"
)

// A statement too fast for log_min_duration_statement but slower
// than auto_explain.log_min_duration left its plan pending on the backend, and
// the report showed that plan under the next timed statement, a 7-minute SQL
// EXECUTE. The fixture replays that on a test instance with no real data.
func TestPendingPlanOfAnotherStatementIsDropped(t *testing.T) {
	ch := make(chan []parser.LogEntry, 16)
	errc := make(chan error, 1)
	go func() { errc <- parser.ParseFile("../trace/testdata/slow_execute.log", ch); close(ch) }()
	var entries []parser.LogEntry
	for batch := range ch {
		entries = append(entries, batch...)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	m := sqlRunSingle(entries)
	var exec *QueryStat
	for _, qs := range m.QueryStats {
		if strings.HasPrefix(qs.ID, "xe-") {
			exec = qs
		}
		if strings.Contains(qs.LastPlan, "ORDER_LINES") {
			t.Errorf("%s carries the ORDER_LINES plan", qs.ID)
		}
	}
	if exec == nil {
		t.Fatal("EXECUTE statement not found")
	}
	if !strings.HasPrefix(exec.LastPlan, "Gather Merge") || !strings.Contains(exec.LastPlan, `Parallel Seq Scan on "SHIPMENTS"`) {
		t.Errorf("EXECUTE plan = %q", exec.LastPlan)
	}
}

func TestPlanBelongsTo(t *testing.T) {
	cases := []struct {
		queryText, query string
		want             bool
	}{
		{"", "select 1", true},
		{"select * from t where id = 1; seq scan on t (cost=…", "SELECT *\n  FROM t WHERE id = 1;", true},
		{"prepare qpsqlpstmt_1001 as select * from \"shipments\" gather merge …", "EXECUTE qpsqlpstmt_1001 (1, 2, 3)", true},
		{"select * from \"order_lines\" where load_ref = '00091505'; seq scan …", "EXECUTE qpsqlpstmt_1001 (1, 2, 3)", false},
		{"select * from a; seq scan on a …", "select * from b", false},
	}
	for _, c := range cases {
		if got := planBelongsTo(c.queryText, c.query); got != c.want {
			t.Errorf("planBelongsTo(%q, %q) = %v, want %v", c.queryText, c.query, got, c.want)
		}
	}
}

// jsonlog and csvlog keep the plan's lines: a multi-line Query Text and the
// Query Parameters line must not end up in the plan.
func TestPlanTextSkipsMultiLineQueryText(t *testing.T) {
	msg := "duration: 2118.349 ms  plan:\nQuery Text: PREPARE s AS SELECT *\n  FROM t\n  WHERE a = $1;\nQuery Parameters: $1 = '1'\nSeq Scan on t  (cost=0.00..1.00 rows=1 width=4) (actual time=0.01..0.02 rows=1 loops=1)\n  Filter: (a = 1)"
	got := extractPlanText(msg)
	if !strings.HasPrefix(got, "Seq Scan on t") || strings.Contains(got, "Query Parameters") || strings.Contains(got, "FROM t") {
		t.Errorf("plan = %q", got)
	}
}
