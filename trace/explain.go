package trace

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Finding is one thing worth knowing about a traced statement, explained:
// what happened with its numbers, whether it matters here, the read-only
// queries that confirm it, and what to propose. Every text is computed from
// the logs, in the language of the traces.
type Finding struct {
	Kind     string   // "client", "result", "sort", "hash", "filter", "estimate", "workers", "duplicate", "jit", "lock"
	Title    string   // one line
	Why      string   // what happened, with the numbers
	Impact   string   // whether it matters for this statement's time
	Weight   float64  // share of the statement's duration it accounts for, -1 if unknown
	Checks   []string // read-only SQL to confirm it
	Proposal string   // what to propose to the DBA or the developers
}

// Findings explains what stands out in the statement, the heaviest first.
func (t *Trace) Findings() []Finding {
	var out []Finding
	split := t.TimeSplit()
	if f, ok := t.clientFinding(split); ok {
		out = append(out, f)
	}
	if f, ok := t.lockFinding(); ok {
		out = append(out, f)
	}
	if t.Plan != nil {
		if split.Verdict != "client" {
			if f, ok := t.resultFinding(); ok {
				out = append(out, f)
			}
		}
		out = append(out, t.nodeFindings()...)
		if f, ok := t.jitFinding(); ok {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	return out
}

// share is part of the statement's duration, clamped to [0, 1].
func (t *Trace) share(ms float64) float64 {
	if ms < 0 {
		return -1
	}
	if t.DurationMs <= 0 {
		return 0
	}
	return math.Min(1, ms/t.DurationMs)
}

func (t *Trace) impact(ms float64) string {
	w := t.share(ms)
	switch {
	case w < 0:
		return T("impact.unknown")
	case w >= 0.1:
		return T("impact.major", HumanDuration(ms), Percent(w))
	}
	return T("impact.minor", HumanDuration(ms), Percent(w))
}

// clientFinding explains an SQL EXECUTE that spent its time sending rows.
func (t *Trace) clientFinding(split TimeSplit) (Finding, bool) {
	if split.Verdict != "client" || split.AfterMs <= 0 {
		return Finding{}, false
	}
	var bytes int64
	var rows float64
	width := 0
	if t.Plan != nil {
		rows, bytes = t.Plan.ResultBytes()
		if len(t.Plan.Nodes) > 0 {
			width = t.Plan.Nodes[0].Width
		}
	}
	if r := t.StoredResult(); r != nil {
		bytes = r.Bytes
	}
	f := Finding{
		Kind:   "client",
		Title:  T("client.title", HumanDuration(split.AfterMs), Percent(t.share(split.AfterMs))),
		Weight: t.share(split.AfterMs),
		Impact: T("client.impact"),
		Checks: []string{T("client.check")},
	}
	if bytes > 0 {
		perSec := float64(bytes) / (split.AfterMs / 1000)
		mbit := perSec * 8 / 1e6
		f.Why = T("client.why", HumanBytes(bytes), HumanDuration(split.AfterMs), HumanBytes(int64(perSec)), decimal("%.0f", mbit), HumanDuration(split.ExecutorMs))
		if rows > 0 && width > 0 {
			f.Why += T("client.why.rows", HumanCount(rows), width)
		}
	} else {
		f.Why = T("split.client", HumanDuration(split.ExecutorMs), HumanDuration(split.AfterMs))
	}
	client := t.Client
	if client == "" {
		client = T("client.unknown")
	}
	f.Proposal = T("client.proposal", client, width)
	if strings.HasPrefix(strings.ToLower(executeName(t.Statement)), "qpsqlpstmt_") {
		f.Proposal += T("client.proposal.qt")
	}
	return f, true
}

// resultFinding flags a large result when sending it is part of the executor.
func (t *Trace) resultFinding() (Finding, bool) {
	rows, bytes := t.Plan.ResultBytes()
	if rows < 100000 || len(t.Plan.Nodes) == 0 {
		return Finding{}, false
	}
	return Finding{
		Kind:     "result",
		Title:    T("result.title", HumanCount(rows), t.Plan.Nodes[0].Width, HumanBytes(bytes)),
		Why:      T("result.why", HumanCount(rows)),
		Impact:   t.impact(t.Plan.Nodes[0].Wall()),
		Weight:   -1,
		Proposal: T("result.proposal"),
	}, true
}

var lockAfterRe = regexp.MustCompile(`after ([0-9.]+) ms`)

// lockFinding reports the longest lock wait the backend logged.
func (t *Trace) lockFinding() (Finding, bool) {
	best, text := 0.0, ""
	for _, ev := range t.Events {
		if ev.Kind != "lock" || ev.PID != t.PID {
			continue
		}
		if m := lockAfterRe.FindStringSubmatch(ev.Text); m != nil {
			if ms, _ := strconv.ParseFloat(m[1], 64); ms > best {
				best, text = ms, ev.Text
			}
		}
	}
	if best < 1000 && t.share(best) < 0.05 {
		return Finding{}, false
	}
	return Finding{
		Kind:     "lock",
		Title:    T("lock.title", HumanDuration(best)),
		Why:      T("lock.why", text),
		Impact:   t.impact(best),
		Weight:   t.share(best),
		Checks:   []string{T("lock.check")},
		Proposal: T("lock.proposal"),
	}, true
}

var (
	tableRe   = regexp.MustCompile(`\bon\s+("(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_$.]*)`)
	castColRe = regexp.MustCompile(`\(([a-z_][a-z0-9_]*)\)::([a-z_]+(?: [a-z_]+)*)`)
	jitRe     = regexp.MustCompile(`Timing:.*Total ([0-9.]+) ms`)
)

// nodeTable returns the table a node reads: the SQL name as written in the
// plan ("\"ORDERS\"", "orders") and the name stored in the catalogs.
func nodeTable(n *Node) (sqlName, catalogName string, ok bool) {
	m := tableRe.FindStringSubmatch(n.Name)
	if m == nil {
		return "", "", false
	}
	sqlName = m[1]
	name := sqlName
	if i := strings.LastIndex(name, "."); i >= 0 && !strings.HasSuffix(name, `"`) {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, `"`) {
		name = strings.ReplaceAll(strings.Trim(name, `"`), `""`, `"`)
	} else {
		name = strings.ToLower(name)
	}
	return sqlName, name, true
}

func quoteList(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = "'" + strings.ReplaceAll(c, "'", "''") + "'"
	}
	return strings.Join(q, ", ")
}

// workMemFor suggests a work_mem that would hold a spill: one and a half
// times the largest one, rounded up to a power of two in megabytes.
func workMemFor(spill int64) string {
	mb := float64(spill) * 1.5 / (1 << 20)
	p := 4.0
	for p < mb {
		p *= 2
	}
	if p >= 1024 {
		return fmt.Sprintf("%.0fGB", p/1024)
	}
	return fmt.Sprintf("%.0fMB", p)
}

// nodeFindings explains what stands out in the plan nodes.
func (t *Trace) nodeFindings() []Finding {
	var out []Finding
	for _, n := range t.Plan.Nodes {
		var spills []int64
		var planned, launched string
		var sortKey string
		for _, d := range n.Details {
			if strings.HasPrefix(d, "Sort Key:") {
				sortKey = strings.TrimSpace(strings.TrimPrefix(d, "Sort Key:"))
			}
			if strings.Contains(d, "Sort Method:") && strings.Contains(d, "Disk:") {
				for _, m := range diskRe.FindAllStringSubmatch(d, -1) {
					kb, _ := strconv.ParseInt(m[1], 10, 64)
					spills = append(spills, kb*1024)
				}
			}
			if m := workersRe.FindStringSubmatch(d); m != nil {
				if m[1] == "Planned" {
					planned = m[2]
				} else {
					launched = m[2]
				}
			}
			if m := batchesRe.FindStringSubmatch(d); m != nil && m[1] != "1" && strings.Contains(d, "Memory Usage") {
				out = append(out, Finding{
					Kind: "hash", Title: T("hash.title", n.Name, m[1]), Why: T("hash.why"),
					Impact: t.impact(n.Self()), Weight: t.share(n.Self()),
					Checks: []string{T("sort.check")}, Proposal: T("hash.proposal"),
				})
			}
			if m := removedRe.FindStringSubmatch(d); m != nil && n.Actual {
				if f, ok := t.filterFinding(n, m[1]); ok {
					out = append(out, f)
				}
			}
		}
		if len(spills) > 0 {
			var total, largest int64
			for _, s := range spills {
				total += s
				largest = max(largest, s)
			}
			f := Finding{
				Kind: "sort", Title: T("sort.title", n.Name, HumanBytes(total)),
				Why:    T("sort.why", HumanBytes(total), len(spills), HumanBytes(largest)),
				Impact: t.impact(n.Self()), Weight: t.share(n.Self()),
				Checks: []string{T("sort.check")}, Proposal: T("sort.proposal", workMemFor(largest)),
			}
			if sortKey != "" && len(n.Children) == 1 && strings.Contains(n.Children[0].Name, "Seq Scan") {
				if table, _, ok := nodeTable(n.Children[0]); ok {
					f.Proposal += T("sort.proposal.index", table, sortKey)
				}
			}
			out = append(out, f)
		}
		if planned != "" && launched != "" && planned != launched {
			out = append(out, Finding{
				Kind: "workers", Title: T("workers.title", n.Name, planned, launched), Why: T("workers.why"),
				Impact: t.impact(n.Wall()), Weight: t.share(n.Wall()),
				Checks: []string{T("workers.check")}, Proposal: T("workers.proposal"),
			})
		}
		if f, ok := t.estimateFinding(n); ok {
			out = append(out, f)
		}
		for _, d := range n.Details {
			if dup := duplicateConjuncts(d); dup != "" {
				out = append(out, Finding{
					Kind: "duplicate", Title: T("dup.title", n.Name), Why: T("dup.why", dup),
					Impact: T("dup.impact"), Weight: 0, Proposal: T("dup.proposal"),
				})
			}
		}
	}
	return out
}

// filterFinding flags a scan that reads far more rows than it keeps.
func (t *Trace) filterFinding(n *Node, removedText string) (Finding, bool) {
	removed, _ := strconv.ParseFloat(removedText, 64)
	removed *= n.Loops
	kept := n.ActRows * n.Loops
	if removed < 100000 || removed <= 10*kept {
		return Finding{}, false
	}
	f := Finding{
		Kind:   "filter",
		Title:  T("filter.title", n.Name, HumanCount(removed+kept), HumanCount(kept)),
		Why:    T("filter.why", Percent(kept/(removed+kept))),
		Impact: t.impact(n.Self()),
		Weight: t.share(n.Self()),
	}
	for _, d := range n.Details {
		if strings.HasPrefix(d, "Filter:") {
			if m := castColRe.FindStringSubmatch(d); m != nil {
				f.Why += T("filter.why.cast", m[1], m[2])
			}
			break
		}
	}
	cols := filterColumns(n)
	if table, catalog, ok := nodeTable(n); ok && len(cols) > 0 {
		f.Checks = []string{fmt.Sprintf(T("filter.check"), strings.ReplaceAll(catalog, "'", "''"), quoteList(cols))}
		f.Proposal = T("filter.proposal", table, strings.Join(cols, ", "))
	}
	return f, true
}

// estimateFinding flags a planner estimate off by more than 10×.
func (t *Trace) estimateFinding(n *Node) (Finding, bool) {
	if !n.Actual || n.Loops <= 0 {
		return Finding{}, false
	}
	est, act := n.EstRows, n.ActRows
	if (act < 1000 && est < 1000) || (act <= 10*est && est <= 10*act) {
		return Finding{}, false
	}
	ratio := math.Max(est, 1) / math.Max(act, 1)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	f := Finding{
		Kind:   "estimate",
		Title:  T("estimate.title", n.Name, HumanCount(est), HumanCount(act)),
		Why:    T("estimate.why", HumanCount(math.Round(ratio))),
		Impact: t.impact(n.Wall()),
		Weight: t.share(n.Wall()),
	}
	if table, catalog, ok := nodeTable(n); ok {
		f.Checks = []string{fmt.Sprintf(T("estimate.check"), strings.ReplaceAll(catalog, "'", "''"))}
		f.Proposal = T("estimate.proposal", table)
	} else {
		f.Proposal = T("estimate.proposal", "…")
	}
	return f, true
}

// jitFinding flags a JIT compilation that weighs on the executor.
func (t *Trace) jitFinding() (Finding, bool) {
	for _, l := range t.Plan.Lines {
		m := jitRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		ms, _ := strconv.ParseFloat(m[1], 64)
		if ms < 100 || ms < 0.1*t.Plan.DurationMs {
			return Finding{}, false
		}
		return Finding{
			Kind: "jit", Title: T("jit.title", HumanDuration(ms)),
			Why:    T("jit.why", HumanDuration(t.Plan.DurationMs), HumanDuration(ms)),
			Impact: t.impact(ms), Weight: t.share(ms),
			Checks: []string{T("jit.check")}, Proposal: T("jit.proposal"),
		}, true
	}
	return Finding{}, false
}
