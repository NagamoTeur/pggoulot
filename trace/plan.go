package trace

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Plan is an auto_explain plan in text format.
type Plan struct {
	Time       time.Time
	DurationMs float64 // executor time, as measured by auto_explain
	QueryText  string
	Parameters string
	Lines      []string // plan lines as PostgreSQL wrote them (one tab removed)
	Nodes      []*Node
	// Rebuilt is set when the log had flattened the plan onto one line
	// (syslog): the lines were cut again, but the indentation is a guess.
	Rebuilt bool

	seq int64
}

// Node is one plan node and the detail lines printed under it.
type Node struct {
	Name     string // "Parallel Seq Scan on \"SHIPMENTS\""
	Depth    int    // indentation, in characters
	EstRows  float64
	Width    int
	Actual   bool
	ActRows  float64 // per loop
	Loops    float64
	ActTotal float64 // ms, per loop
	Details  []string
	Children []*Node
	// Parallel is set for the nodes under a Gather: their times are per
	// process, the processes running at the same time.
	Parallel bool
}

var nodeRe = regexp.MustCompile(`^(\s*)(?:->\s+)?(.+?)\s+\(cost=[0-9.]+\.\.[0-9.]+ rows=([0-9.]+) width=(\d+)\)(?:\s+\(actual time=([0-9.]+)\.\.([0-9.]+) rows=([0-9.]+) loops=([0-9.]+)\))?`)

// ParsePlan reads an auto_explain message body ("duration: … ms  plan: …").
// It returns nil for a duration line that is not a plan.
func ParsePlan(body string) *Plan {
	ms, kind, text, ok := parseDuration(body)
	if !ok || kind != "plan" {
		return nil
	}
	p := &Plan{DurationMs: ms}
	text = strings.TrimLeft(text, " ")
	if !strings.Contains(text, "\n") {
		text, p.Rebuilt = unflatten(text), true
	}
	lines := strings.Split(text, "\n")
	inQuery := false
	for _, raw := range lines {
		line := strings.TrimPrefix(raw, "\t")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Query Text:"):
			p.QueryText = strings.TrimSpace(strings.TrimPrefix(trimmed, "Query Text:"))
			inQuery = true
			continue
		case strings.HasPrefix(trimmed, "Query Parameters:"):
			p.Parameters = strings.TrimSpace(strings.TrimPrefix(trimmed, "Query Parameters:"))
			inQuery = false
			continue
		case inQuery && len(p.Nodes) == 0 && !nodeRe.MatchString(line):
			p.QueryText += "\n" + line
			continue
		}
		inQuery = false
		if trimmed == "" {
			continue
		}
		p.Lines = append(p.Lines, line)
		if m := nodeRe.FindStringSubmatch(line); m != nil {
			n := &Node{Name: m[2], Depth: len(m[1])}
			n.EstRows, _ = strconv.ParseFloat(m[3], 64)
			n.Width, _ = strconv.Atoi(m[4])
			if m[5] != "" {
				n.Actual = true
				n.ActTotal, _ = strconv.ParseFloat(m[6], 64)
				n.ActRows, _ = strconv.ParseFloat(m[7], 64)
				n.Loops, _ = strconv.ParseFloat(m[8], 64)
			}
			p.Nodes = append(p.Nodes, n)
		} else if len(p.Nodes) > 0 {
			last := p.Nodes[len(p.Nodes)-1]
			last.Details = append(last.Details, trimmed)
		}
	}
	p.QueryText = strings.TrimSpace(p.QueryText)
	p.buildTree()
	return p
}

// buildTree links each node to its children through the indentation.
func (p *Plan) buildTree() {
	var stack []*Node
	for _, n := range p.Nodes {
		for len(stack) > 0 && stack[len(stack)-1].Depth >= n.Depth {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			n.Parallel = parent.Parallel || strings.HasPrefix(parent.Name, "Gather")
		}
		stack = append(stack, n)
	}
}

// Wall is the time the node and its children took, in milliseconds, or -1
// without actual times. Under a Gather the processes run at the same time,
// so the per-process time is the wall time.
func (n *Node) Wall() float64 {
	if !n.Actual {
		return -1
	}
	if n.Parallel {
		return n.ActTotal
	}
	return n.ActTotal * n.Loops
}

// Self is the time the node took without its children, or -1.
func (n *Node) Self() float64 {
	w := n.Wall()
	if w < 0 {
		return -1
	}
	for _, c := range n.Children {
		if cw := c.Wall(); cw > 0 {
			w -= cw
		}
	}
	if w < 0 {
		return 0
	}
	return w
}

// planFields start the detail lines under a plan node.
var planFields = []string{
	"Query Text: ", "Query Parameters: ", "Output: ", "Filter: ", "Index Cond: ", "Hash Cond: ",
	"Merge Cond: ", "Join Filter: ", "Recheck Cond: ", "Sort Key: ", "Sort Method: ",
	"Group Key: ", "Rows Removed by ", "Buffers: ", "Batches: ", "Buckets: ", "Memory Usage: ",
	"Inner Unique: ", "Workers Planned: ", "Workers Launched: ", "Worker ", "Heap Fetches: ",
	"Heap Blocks: ", "Planning Time: ", "Execution Time: ", "InitPlan ", "SubPlan ",
}

// unflatten cuts a plan that the log flattened onto one line back into lines:
// before each "->" and each detail field. The depth of the nodes is lost, so
// each child is indented one step further than the previous node.
func unflatten(text string) string {
	if k := topNodeStart(text); k > 0 {
		text = text[:k] + "\n" + text[k:]
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(text); {
		if i > 0 && strings.HasPrefix(text[i:], "->  ") {
			depth++
			b.WriteString("\n" + strings.Repeat("  ", depth) + "->  ")
			i += 4
			continue
		}
		if i > 0 && text[i-1] == ' ' {
			cut := false
			for _, f := range planFields {
				if strings.HasPrefix(text[i:], f) {
					b.WriteString("\n" + strings.Repeat("  ", depth+1) + f)
					i += len(f)
					cut = true
					break
				}
			}
			if cut {
				continue
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

// nodeNames start a plan node. A flattened plan glues the top node to the
// Query Text, so its name is found back from the first "(cost=".
var nodeNames = []string{
	"Seq Scan", "Index Scan", "Index Only Scan", "Bitmap Heap Scan", "Bitmap Index Scan",
	"Gather Merge", "Gather", "Sort", "Incremental Sort", "Limit", "Hash Join", "Hash",
	"Nested Loop", "Merge Join", "Aggregate", "HashAggregate", "GroupAggregate", "Group",
	"Unique", "Result", "Append", "Merge Append", "Subquery Scan", "Materialize", "Memoize",
	"CTE Scan", "WindowAgg", "Function Scan", "Values Scan", "Foreign Scan", "Custom Scan",
	"LockRows", "SetOp", "ProjectSet", "Insert on", "Update on", "Delete on", "Merge on",
	"BitmapAnd", "BitmapOr", "Tid Scan", "Sample Scan", "WorkTable Scan", "Recursive Union",
}

func topNodeStart(text string) int {
	cost := strings.Index(text, " (cost=")
	if cost < 0 {
		return -1
	}
	best := -1
	for _, n := range nodeNames {
		k := strings.LastIndex(text[:cost], " "+n)
		if k < 0 || best >= 0 && k+1 >= best {
			continue
		}
		// The name must run up to the cost: "Gather Merge  (cost=",
		// "Seq Scan on t  (cost=", "Index Scan using i on t  (cost=".
		rest := text[k+1+len(n) : cost]
		if strings.TrimSpace(rest) == "" || strings.HasPrefix(rest, " on ") || strings.HasPrefix(rest, " using ") {
			if !strings.ContainsAny(rest, "()") {
				best = k + 1
			}
		}
	}
	if best < 0 {
		return -1
	}
	for _, mod := range []string{"Parallel ", "Partial ", "Finalize "} {
		if best >= len(mod) && text[best-len(mod):best] == mod {
			best -= len(mod)
		}
	}
	return best
}

// Matches tells whether the plan's Query Text is the traced statement.
func (p *Plan) Matches(t *Trace) bool {
	q := normalizeSQL(p.QueryText)
	if q == "" {
		return false
	}
	if q == normalizeSQL(t.Statement) || (t.Prepared != "" && q == normalizeSQL(t.Prepared)) {
		return true
	}
	name := executeName(t.Statement)
	return t.Prepared == "" && name != "" && strings.HasPrefix(q, "prepare "+strings.ToLower(name)+" ")
}

var (
	diskRe    = regexp.MustCompile(`Disk:\s+(\d+)kB`)
	removedRe = regexp.MustCompile(`^Rows Removed by (?:Join )?Filter:\s+([0-9.]+)`)
	workersRe = regexp.MustCompile(`^Workers (Planned|Launched):\s+(\d+)`)
	batchesRe = regexp.MustCompile(`Batches:\s+(\d+)`)
	columnRe  = regexp.MustCompile(`\b([a-z_][a-z0-9_]*)\)?\s*(?:=|<>|<=|>=|<|>|~~\*?|!~~|IS\b|IN\b)`)
	castRe    = regexp.MustCompile(`::[a-z_]+(?: [a-z_]+)*(?:\[\])?`)
)

// ResultBytes estimates the size of the rows the plan returned to the client:
// rows of the top node × its average width.
func (p *Plan) ResultBytes() (rows float64, bytes int64) {
	if len(p.Nodes) == 0 {
		return 0, 0
	}
	root := p.Nodes[0]
	rows = root.EstRows
	if root.Actual {
		rows = root.ActRows * root.Loops
	}
	return rows, int64(rows * float64(root.Width))
}

// filterColumns returns the columns compared in the node's Filter.
func filterColumns(n *Node) []string {
	seen := map[string]bool{}
	var cols []string
	for _, d := range n.Details {
		if !strings.HasPrefix(d, "Filter:") {
			continue
		}
		for _, m := range columnRe.FindAllStringSubmatch(castRe.ReplaceAllString(d, ""), -1) {
			if c := m[1]; !seen[c] && c != "and" && c != "or" && c != "not" {
				seen[c] = true
				cols = append(cols, c)
			}
		}
	}
	return cols
}

// duplicateConjuncts finds a top-level AND term repeated in a Filter or Cond.
func duplicateConjuncts(d string) string {
	i := strings.Index(d, ": ")
	if i < 0 || !(strings.HasPrefix(d, "Filter") || strings.HasSuffix(d[:i], "Cond")) {
		return ""
	}
	expr := strings.TrimSpace(d[i+2:])
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") && balanced(expr[1:len(expr)-1]) {
		expr = expr[1 : len(expr)-1]
	}
	seen := map[string]bool{}
	for _, term := range splitTop(expr, " AND ") {
		if seen[term] {
			return term
		}
		seen[term] = true
	}
	return ""
}

func balanced(s string) bool {
	depth := 0
	for _, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func splitTop(s, sep string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}
