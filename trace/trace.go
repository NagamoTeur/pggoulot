// Package trace rebuilds the story of the slowest statements from PostgreSQL
// logs: the statement and its prepared text, the bound parameters, the
// auto_explain plan that really belongs to it, the temporary files of the
// backend and of its parallel workers, and what the backend logged while it
// ran.
//
// It works in a single pass, so it runs inside the streaming analysis (every
// report format, stdin, the WASM build) as well as in the trace command. Each
// backend keeps what it logged since its previous timed statement; when a
// timed statement ends and is among the slowest, its trace is built from that
// buffer, from the temp files of parallel workers and from the server events
// of its window.
package trace

import (
	"container/heap"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Alain-L/quellog/parser"
)

// Options chooses which statements are traced.
type Options struct {
	Top         int           // keep the N slowest (0: none)
	PID         string        // only this backend
	Contains    string        // statement or prepared text contains this (case-insensitive)
	MinDuration time.Duration // ignore faster statements
	// Accept, when set, filters the timed statements themselves (the trace
	// command's --begin, --dbname…): the entries around them are always
	// read, since parallel workers log without database or user.
	Accept func(*parser.LogEntry) bool
}

// Limits on what is kept while streaming. They bound memory on logs with many
// backends; a trace only loses events beyond them.
const (
	maxBackendEvents = 64
	maxBackendPlans  = 4
	maxBackendTemps  = 256
	maxPoolTemps     = 4096
	maxServerEvents  = 1024
	sweepEvery       = 50000
	idleBackend      = time.Hour
)

// Event is one log entry shown in a trace.
type Event struct {
	Time  time.Time
	PID   string
	Role  string // "backend", "worker", "server"
	Kind  string // "plan", "temp", "lock", "error", "checkpoint", "detail", "end", "log"
	Text  string
	Bytes int64
	Seq   int64

	statement string // temp files: the statement that created them
	leader    string // temp files of a parallel worker: its leader (csvlog, jsonlog)
}

// Trace is everything known about one timed statement.
type Trace struct {
	PID        string
	End        time.Time
	DurationMs float64
	Kind       string // "statement" or "execute <name>"
	Statement  string
	// LoggedText is the statement as the log line carries it: with jsonlog
	// and csvlog the inline DETAIL follows. The SQL analysis keys its
	// queries on this text.
	LoggedText string
	DB         string
	User       string
	App        string
	Client     string
	Prepared   string // prepared statement text (DETAIL: prepare:)
	Parameters string // "$1 = '…', …" (DETAIL: parameters:, auto_explain or EXECUTE arguments)
	Plan       *Plan  // auto_explain plan of this statement, nil if none was logged
	// ForeignPlans are auto_explain plans logged by the same backend during
	// the window but for another statement: they must not be read as this
	// statement's plan.
	ForeignPlans []*Plan
	TempFiles    []Event
	Events       []Event

	seq     int64
	matched bool // --query matched (it may only match the DETAIL that follows)
}

// Start is when the statement began, give or take the log's time resolution.
func (t *Trace) Start() time.Time {
	return t.End.Add(-time.Duration(t.DurationMs * float64(time.Millisecond)))
}

// windowStart is the earliest time an entry of this statement can carry: %t
// has one-second resolution.
func (t *Trace) windowStart() time.Time {
	return t.Start().Truncate(time.Second).Add(-time.Second)
}

// Workers returns the PIDs of the parallel workers seen in the trace.
func (t *Trace) Workers() []string {
	seen := map[string]bool{}
	var out []string
	for _, ev := range t.TempFiles {
		if ev.Role == "worker" && !seen[ev.PID] {
			seen[ev.PID] = true
			out = append(out, ev.PID)
		}
	}
	return out
}

type backend struct {
	events []Event
	plans  []*Plan
	temps  []Event
	last   time.Time
}

func (b *backend) reset() {
	b.events, b.plans, b.temps = b.events[:0], b.plans[:0], b.temps[:0]
}

// Tracer follows the stream and keeps the slowest statements' traces.
type Tracer struct {
	opt      Options
	backends map[string]*backend
	pool     []Event // temp files of every backend, for the parallel workers
	poolNext int
	server   []Event // checkpoints and lock events, any backend
	srvNext  int
	// awaitTemp is a stderr temp file line waiting for its STATEMENT line.
	awaitTemp map[string]int // pid → index in pool
	// awaitLock is a stderr lock line waiting for its DETAIL line.
	awaitLock map[string]int // pid → index in server
	// awaitDetail is a trace waiting for the DETAIL of its end line.
	awaitDetail map[string]*Trace
	top         traceHeap
	count       int
}

// NewTracer returns a tracer; a Top of zero or less traces nothing.
func NewTracer(opt Options) *Tracer {
	opt.Contains = strings.ToLower(opt.Contains)
	return &Tracer{
		opt:         opt,
		backends:    map[string]*backend{},
		awaitTemp:   map[string]int{},
		awaitLock:   map[string]int{},
		awaitDetail: map[string]*Trace{},
	}
}

// Process reads one entry. Entries must come in stream order with Seq set;
// BodyOffset must be stamped (analysis.StampBodyOffset).
func (tr *Tracer) Process(e *parser.LogEntry) {
	if tr.opt.Top <= 0 || e.PID == "" {
		return
	}
	tr.count++
	if tr.count%sweepEvery == 0 {
		tr.sweep(e.Timestamp)
	}

	if e.IsContinuation {
		tr.continuation(e)
		return
	}
	delete(tr.awaitTemp, e.PID)
	delete(tr.awaitLock, e.PID)
	if t := tr.awaitDetail[e.PID]; t != nil {
		delete(tr.awaitDetail, e.PID)
		tr.settle(t)
	}
	off := int(e.BodyOffset)
	if off <= 0 {
		// Not stamped by the analysis dispatcher (tests), or no severity.
		if off = bodyOffset(e.Message); off <= 0 {
			return
		}
	}
	if off > len(e.Message) {
		return
	}
	body := e.Message[off:]
	sev := severityAt(e.Message, off)

	if e.LeaderPID != "" {
		// A parallel worker (csvlog, jsonlog): only its temp files matter.
		if strings.HasPrefix(body, "temporary file: ") {
			if ev, ok := tempEvent(e, body); ok {
				ev.leader = e.LeaderPID
				tr.addPool(ev)
			}
		}
		return
	}

	switch {
	case strings.HasPrefix(body, "duration: "):
		ms, kind, text, ok := parseDuration(body)
		if !ok {
			return
		}
		if kind == "plan" {
			if p := ParsePlan(body); p != nil {
				p.Time, p.seq = e.Timestamp, e.Seq
				b := tr.backend(e)
				if len(b.plans) == maxBackendPlans {
					b.plans = b.plans[1:]
				}
				b.plans = append(b.plans, p)
				b.addEvent(Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: "plan", Text: firstLine(body), Seq: e.Seq})
			}
			return
		}
		if kind == "parse" || kind == "bind" {
			return
		}
		tr.ended(e, ms, kind, text)
	case strings.HasPrefix(body, "temporary file: "):
		if ev, ok := tempEvent(e, body); ok {
			ev.statement = Split(e.Message).Statement
			if i := tr.addPool(ev); ev.statement == "" {
				tr.awaitTemp[e.PID] = i // stderr: the STATEMENT line follows
			}
			b := tr.backend(e)
			if len(b.temps) < maxBackendTemps {
				b.temps = append(b.temps, ev)
			}
		}
	case strings.HasPrefix(body, "checkpoint ") || strings.HasPrefix(body, "restartpoint "):
		tr.addServer(Event{Time: e.Timestamp, PID: e.PID, Role: "server", Kind: "checkpoint", Text: firstLine(body), Seq: e.Seq})
	case isLockLine(body):
		ev := Event{Time: e.Timestamp, PID: e.PID, Role: "server", Kind: "lock", Text: firstLine(body), Seq: e.Seq}
		if msg := Split(e.Message); msg.Detail != "" {
			ev.Text += " — " + firstLine(msg.Detail)
		}
		i := tr.addServer(ev)
		tr.awaitLock[e.PID] = i
		tr.backend(e).addEvent(Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: "lock", Text: firstLine(body), Seq: e.Seq})
	case strings.HasPrefix(body, "disconnection: "):
		delete(tr.backends, e.PID)
	case strings.HasPrefix(body, "statement: ") || strings.HasPrefix(body, "connection ") || strings.HasPrefix(body, "execute ") || strings.HasPrefix(body, "parameters: "):
		// log_statement and connection lines: too frequent to keep, and the
		// timed statement already says what ran.
	default:
		kind := "log"
		switch sev {
		case "ERROR", "FATAL", "PANIC", "WARNING":
			kind = "error"
		case "LOG":
		default:
			return
		}
		tr.backend(e).addEvent(Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: kind, Text: firstLine(body), Seq: e.Seq})
	}
}

// continuation handles the DETAIL and STATEMENT lines stderr writes as
// separate entries.
func (tr *Tracer) continuation(e *parser.LogEntry) {
	pid := e.PID
	if t := tr.awaitDetail[pid]; t != nil {
		delete(tr.awaitDetail, pid)
		if m := Split(e.Message); m.Severity == "DETAIL" {
			t.readDetail(m.Body)
			t.Events = append(t.Events, Event{Time: e.Timestamp, PID: pid, Role: "backend", Kind: "detail", Text: firstLine(m.Body), Seq: e.Seq})
		}
		tr.settle(t)
		return
	}
	if i, ok := tr.awaitTemp[pid]; ok {
		delete(tr.awaitTemp, pid)
		if m := Split(e.Message); m.Severity == "STATEMENT" && i >= 0 && i < len(tr.pool) && tr.pool[i].PID == pid {
			tr.pool[i].statement = m.Body
		}
		return
	}
	if i, ok := tr.awaitLock[pid]; ok {
		delete(tr.awaitLock, pid)
		if m := Split(e.Message); m.Severity == "DETAIL" && i >= 0 && i < len(tr.server) && tr.server[i].PID == pid {
			tr.server[i].Text += " — " + firstLine(m.Body)
		}
	}
}

func (tr *Tracer) backend(e *parser.LogEntry) *backend {
	b := tr.backends[e.PID]
	if b == nil {
		b = &backend{}
		tr.backends[e.PID] = b
	}
	b.last = e.Timestamp
	return b
}

func (b *backend) addEvent(ev Event) {
	if len(b.events) == maxBackendEvents {
		copy(b.events, b.events[1:])
		b.events = b.events[:len(b.events)-1]
	}
	b.events = append(b.events, ev)
}

// addPool keeps a temp file in the ring shared by all backends and returns
// its index.
func (tr *Tracer) addPool(ev Event) int {
	if len(tr.pool) < maxPoolTemps {
		tr.pool = append(tr.pool, ev)
		return len(tr.pool) - 1
	}
	i := tr.poolNext
	tr.pool[i] = ev
	tr.poolNext = (i + 1) % maxPoolTemps
	return i
}

func (tr *Tracer) addServer(ev Event) int {
	if len(tr.server) < maxServerEvents {
		tr.server = append(tr.server, ev)
		return len(tr.server) - 1
	}
	i := tr.srvNext
	tr.server[i] = ev
	tr.srvNext = (i + 1) % maxServerEvents
	return i
}

// sweep forgets backends that have been silent for a long time: parallel
// workers of stderr logs and sessions whose disconnection was not logged.
func (tr *Tracer) sweep(now time.Time) {
	for pid, b := range tr.backends {
		if now.Sub(b.last) > idleBackend {
			delete(tr.backends, pid)
		}
	}
}

// ended handles a timed statement: it becomes a trace if it is among the
// slowest so far, and the backend's buffer starts over either way.
func (tr *Tracer) ended(e *parser.LogEntry, ms float64, kind, text string) {
	b := tr.backends[e.PID]
	defer func() {
		if b != nil {
			b.reset()
			b.last = e.Timestamp
		}
	}()
	if tr.opt.PID != "" && e.PID != tr.opt.PID {
		return
	}
	if time.Duration(ms*float64(time.Millisecond)) < tr.opt.MinDuration {
		return
	}
	if tr.opt.Accept != nil && !tr.opt.Accept(e) {
		return
	}
	if len(tr.top) >= tr.opt.Top && ms <= tr.top[0].DurationMs {
		return
	}

	m := Split(e.Message)
	// jsonlog and csvlog carry DETAIL and STATEMENT in the same entry.
	logged := text
	if _, _, own, ok := parseDuration(m.Body); ok {
		text = own
	}
	t := &Trace{
		PID: e.PID, End: e.Timestamp, DurationMs: ms, Kind: kind, Statement: strings.TrimSpace(text), LoggedText: logged,
		DB: m.DB, User: m.User, App: m.App, Client: m.Client, seq: e.Seq,
	}
	if m.Statement != "" && t.Statement == "" {
		t.Statement = m.Statement
	}
	ws := t.windowStart()
	if b != nil {
		for _, ev := range b.events {
			if !ev.Time.Before(ws) {
				t.Events = append(t.Events, ev)
			}
		}
		for _, p := range b.plans {
			if !p.Time.Before(ws) {
				t.ForeignPlans = append(t.ForeignPlans, p)
			}
		}
		for _, ev := range b.temps {
			if !ev.Time.Before(ws) {
				ev.Role = "backend"
				t.TempFiles = append(t.TempFiles, ev)
			}
		}
	}
	t.Events = append(t.Events, Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: "end", Text: firstLine(m.Body), Seq: e.Seq})
	for _, ev := range tr.server {
		if ev.Time.Before(ws) || ev.Time.After(e.Timestamp.Add(time.Second)) {
			continue
		}
		if ev.Kind == "checkpoint" || (ev.PID != e.PID && mentionsPID(ev.Text, e.PID)) {
			t.Events = append(t.Events, ev)
		}
	}
	t.matched = tr.opt.Contains == "" || strings.Contains(strings.ToLower(t.Statement), tr.opt.Contains)
	if m.Detail != "" {
		t.readDetail(m.Detail)
		t.Events = append(t.Events, Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: "detail", Text: firstLine(m.Detail), Seq: e.Seq})
		tr.settle(t)
		return
	}
	tr.awaitDetail[e.PID] = t
}

// settle finishes a trace once its DETAIL is known: the workers' temp files
// can be told apart by the prepared text, and --query may match it.
func (tr *Tracer) settle(t *Trace) {
	if !t.matched && strings.Contains(strings.ToLower(t.Prepared), tr.opt.Contains) {
		t.matched = true
	}
	if !t.matched {
		return
	}
	if len(tr.top) >= tr.opt.Top && t.DurationMs <= tr.top[0].DurationMs {
		return
	}
	ws, end := t.windowStart(), t.End.Add(time.Second)
	stmt, prep := normalizeSQL(t.Statement), normalizeSQL(t.Prepared)
	name := strings.ToLower(executeName(t.Statement))
	for _, ev := range tr.pool {
		if ev.PID == t.PID || ev.Time.Before(ws) || ev.Time.After(end) {
			continue
		}
		if ev.leader == t.PID || ev.statement != "" && isSameStatement(normalizeSQL(ev.statement), stmt, prep, name) {
			ev.Role = "worker"
			t.TempFiles = append(t.TempFiles, ev)
		}
	}
	t.finish()
	heap.Push(&tr.top, t)
	for len(tr.top) > tr.opt.Top {
		heap.Pop(&tr.top)
	}
}

// isSameStatement tells whether a parallel worker's STATEMENT is the traced
// one. An SQL EXECUTE's workers log the PREPARE text.
func isSameStatement(s, stmt, prep, executeName string) bool {
	return s == stmt || (prep != "" && s == prep) || (executeName != "" && strings.HasPrefix(s, "prepare "+executeName+" "))
}

// finish keeps the plan only when its Query Text is the traced statement and
// fills the parameters.
func (t *Trace) finish() {
	for i := len(t.ForeignPlans) - 1; i >= 0; i-- {
		if t.ForeignPlans[i].Matches(t) {
			t.Plan = t.ForeignPlans[i]
			t.ForeignPlans = append(t.ForeignPlans[:i:i], t.ForeignPlans[i+1:]...)
			break
		}
	}
	if t.Parameters == "" && t.Plan != nil {
		t.Parameters = t.Plan.Parameters
	}
	if t.Parameters == "" {
		t.Parameters = executeArguments(t.Statement)
	}
	sort.SliceStable(t.TempFiles, func(i, j int) bool { return t.TempFiles[i].Seq < t.TempFiles[j].Seq })
	sort.SliceStable(t.Events, func(i, j int) bool { return t.Events[i].Seq < t.Events[j].Seq })
}

// Finalize returns the traces, slowest first.
func (tr *Tracer) Finalize() []*Trace {
	for pid, t := range tr.awaitDetail {
		delete(tr.awaitDetail, pid)
		tr.settle(t)
	}
	out := make([]*Trace, len(tr.top))
	copy(out, tr.top)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DurationMs != out[j].DurationMs {
			return out[i].DurationMs > out[j].DurationMs
		}
		return out[i].seq < out[j].seq
	})
	return out
}

// traceHeap is a min-heap on duration: the fastest kept trace is evicted first.
type traceHeap []*Trace

func (h traceHeap) Len() int { return len(h) }
func (h traceHeap) Less(i, j int) bool {
	if h[i].DurationMs != h[j].DurationMs {
		return h[i].DurationMs < h[j].DurationMs
	}
	return h[i].seq > h[j].seq
}
func (h traceHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *traceHeap) Push(x any)   { *h = append(*h, x.(*Trace)) }
func (h *traceHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// readDetail takes the prepared text or the parameters from the DETAIL of the
// timed statement.
func (t *Trace) readDetail(body string) {
	if rest, ok := strings.CutPrefix(body, "prepare: "); ok {
		t.Prepared = strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(body, "parameters: "); ok {
		t.Parameters = strings.TrimSpace(rest)
	}
}

var tempRe = regexp.MustCompile(`^temporary file: path "([^"]+)", size (\d+)`)

func tempEvent(e *parser.LogEntry, body string) (Event, bool) {
	m := tempRe.FindStringSubmatch(body)
	if m == nil {
		return Event{}, false
	}
	size, _ := strconv.ParseInt(m[2], 10, 64)
	return Event{Time: e.Timestamp, PID: e.PID, Role: "backend", Kind: "temp", Text: m[1], Bytes: size, Seq: e.Seq}, true
}

func isLockLine(body string) bool {
	return strings.HasPrefix(body, "process ") && (strings.Contains(body, " still waiting for ") || strings.Contains(body, " acquired ") || strings.Contains(body, " avoided deadlock ") || strings.Contains(body, " detected deadlock ")) ||
		strings.HasPrefix(body, "deadlock detected")
}

// mentionsPID tells whether a lock line names the backend: "process 123 …",
// "Process holding the lock: 123." or "Wait queue: 456, 123."
func mentionsPID(text, pid string) bool {
	for i := strings.Index(text, pid); i >= 0; {
		before := i == 0 || !isDigit(text[i-1])
		after := i+len(pid) == len(text) || !isDigit(text[i+len(pid)])
		if before && after {
			return true
		}
		j := strings.Index(text[i+1:], pid)
		if j < 0 {
			break
		}
		i += j + 1
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// severityAt reads the severity word that ends right before the body offset
// ("… LOG:  body" → "LOG", "… ERROR:  42P01: body" → "ERROR").
func severityAt(msg string, off int) string {
	i := off
	for i > 0 && msg[i-1] == ' ' {
		i--
	}
	// log_error_verbosity = verbose: a SQLSTATE sits between the two.
	if i >= 7 && msg[i-1] == ':' && isSQLStateAt(msg, i-6) {
		j := i - 6
		for j > 0 && msg[j-1] == ' ' {
			j--
		}
		if j > 0 && msg[j-1] == ':' {
			i = j
		}
	}
	if i == 0 || msg[i-1] != ':' {
		return ""
	}
	end := i - 1
	s := end
	for s > 0 && (msg[s-1] >= 'A' && msg[s-1] <= 'Z' || isDigit(msg[s-1])) {
		s--
	}
	return msg[s:end]
}

var bodyRe = regexp.MustCompile(`(?:^|\s)(?:LOG|ERROR|WARNING|FATAL|PANIC|NOTICE|INFO|DEBUG[1-5]?):\s+(?:[0-9A-Z]{5}:\s+)?`)

// bodyOffset finds where the message body starts, as the analysis
// dispatcher's BodyOffset does.
func bodyOffset(msg string) int {
	if loc := bodyRe.FindStringIndex(msg); loc != nil {
		return loc[1]
	}
	return 0
}

func isSQLStateAt(msg string, i int) bool {
	if i < 0 || i+5 > len(msg) {
		return false
	}
	for _, c := range []byte(msg[i : i+5]) {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// parseDuration reads a log_min_duration_statement or auto_explain line.
// kind is "statement", "execute <name>", "parse <name>", "bind <name>" or
// "plan".
func parseDuration(body string) (ms float64, kind, text string, ok bool) {
	rest, ok := strings.CutPrefix(body, "duration: ")
	if !ok {
		return 0, "", "", false
	}
	i := strings.Index(rest, " ms")
	if i <= 0 {
		return 0, "", "", false
	}
	ms, err := strconv.ParseFloat(rest[:i], 64)
	if err != nil {
		return 0, "", "", false
	}
	rest = strings.TrimLeft(rest[i+3:], " ")
	switch {
	case strings.HasPrefix(rest, "statement:"):
		return ms, "statement", strings.TrimSpace(rest[len("statement:"):]), true
	case strings.HasPrefix(rest, "plan:"):
		return ms, "plan", rest[len("plan:"):], true
	case strings.HasPrefix(rest, "execute "), strings.HasPrefix(rest, "parse "), strings.HasPrefix(rest, "bind "):
		j := strings.IndexByte(rest, ':')
		if j < 0 {
			return 0, "", "", false
		}
		head := rest[:j]
		if strings.HasPrefix(head, "parse ") {
			return ms, "parse", "", true
		}
		if strings.HasPrefix(head, "bind ") {
			return ms, "bind", "", true
		}
		return ms, head, strings.TrimSpace(rest[j+1:]), true
	}
	return 0, "", "", false
}

// Message is the part of a log entry that follows the prefix.
type Message struct {
	Severity string // LOG, DETAIL, STATEMENT, ERROR…
	Body     string
	DB       string
	User     string
	App      string
	Client   string
	// Detail and Statement are the DETAIL and STATEMENT parts that jsonlog and
	// csvlog carry in the same entry (stderr writes them as separate entries).
	Detail    string
	Statement string
}

// inlineParts are the sections the JSON and CSV parsers append to a message,
// from the last one back. csvlog calls the statement QUERY and ends with the
// SQLSTATE.
var inlineParts = []string{" SQLSTATE = '", " CONTEXT: ", " STATEMENT: ", " QUERY: ", " HINT: ", " DETAIL: "}

var severityRe = regexp.MustCompile(`(?:^|\s)(LOG|DETAIL|STATEMENT|ERROR|WARNING|HINT|CONTEXT|FATAL|PANIC|NOTICE|INFO|QUERY|DEBUG[1-5]?):\s+`)

// Split separates the prefix fields from the message body and the inline
// DETAIL and STATEMENT parts.
func Split(raw string) Message {
	loc := severityRe.FindStringSubmatchIndex(raw)
	if loc == nil {
		return Message{Body: raw}
	}
	m := Message{Severity: raw[loc[2]:loc[3]], Body: raw[loc[1]:]}
	prefix := raw[:loc[0]]
	if m.Severity != "STATEMENT" && m.Severity != "DETAIL" {
		for _, part := range inlineParts {
			i := strings.LastIndex(m.Body, part)
			if i < 0 {
				continue
			}
			switch part {
			case " STATEMENT: ", " QUERY: ":
				if m.Statement == "" {
					m.Statement = m.Body[i+len(part):]
				}
			case " DETAIL: ":
				m.Detail = m.Body[i+len(part):]
			}
			m.Body = m.Body[:i]
		}
	}
	m.DB = prefixField(prefix, "db=")
	m.User = prefixField(prefix, "user=")
	m.App = prefixField(prefix, "app=")
	if m.Client = prefixField(prefix, "client="); m.Client == "" {
		m.Client = prefixField(prefix, "host=")
	}
	return m
}

// prefixField reads key=value from a log_line_prefix. Values end at a comma,
// a space or a bracket; app= may contain spaces, so it ends at the next key.
func prefixField(prefix, key string) string {
	i := strings.Index(prefix, key)
	for i > 0 && isWord(prefix[i-1]) {
		j := strings.Index(prefix[i+1:], key)
		if j < 0 {
			return ""
		}
		i += j + 1
	}
	if i < 0 {
		return ""
	}
	v := prefix[i+len(key):]
	if key == "app=" {
		if j := strings.IndexByte(v, ','); j >= 0 && strings.Contains(v[j:], "=") {
			return strings.TrimSpace(v[:j])
		}
	}
	if j := strings.IndexAny(v, ", []"); j >= 0 {
		v = v[:j]
	}
	return v
}

func isWord(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Query returns the SQL that really ran: the prepared text without its
// PREPARE header, or the statement itself.
func (t *Trace) Query() string {
	q := t.Prepared
	if q == "" && t.Plan != nil && t.Plan.QueryText != "" && !strings.HasPrefix(strings.ToUpper(t.Plan.QueryText), "EXECUTE ") {
		q = t.Plan.QueryText
	}
	if q == "" {
		q = t.Statement
	}
	return strings.TrimSuffix(strings.TrimSpace(stripPrepare(untab(q))), ";")
}

// untab removes the tab PostgreSQL puts in front of each continuation line.
func untab(s string) string {
	return strings.ReplaceAll(s, "\n\t", "\n")
}

var prepareRe = regexp.MustCompile(`(?is)^\s*PREPARE\s+\S+?(\s*\([^)]*\))?\s+AS\s+`)

func stripPrepare(q string) string {
	if loc := prepareRe.FindStringIndex(q); loc != nil {
		return q[loc[1]:]
	}
	return q
}

var executeRe = regexp.MustCompile(`(?is)^\s*EXECUTE\s+([A-Za-z0-9_$"]+)\s*(?:\((.*)\))?\s*;?\s*$`)

// IsSQLExecute tells whether the statement is an SQL EXECUTE (drivers such as
// Qt QPSQL send PREPARE / EXECUTE as SQL).
func (t *Trace) IsSQLExecute() bool { return executeName(t.Statement) != "" }

func executeName(stmt string) string {
	if m := executeRe.FindStringSubmatch(stmt); m != nil {
		return m[1]
	}
	return ""
}

// executeArguments turns "EXECUTE s (1, 'a,b')" into "$1 = 1, $2 = 'a,b'".
func executeArguments(stmt string) string {
	m := executeRe.FindStringSubmatch(stmt)
	if m == nil || strings.TrimSpace(m[2]) == "" {
		return ""
	}
	var parts []string
	for i, a := range splitArgs(m[2]) {
		parts = append(parts, "$"+strconv.Itoa(i+1)+" = "+a)
	}
	return strings.Join(parts, ", ")
}

// splitArgs splits on commas outside quotes, parentheses and brackets.
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

var spaceRe = regexp.MustCompile(`\s+`)

func normalizeSQL(s string) string {
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	return strings.ToLower(strings.TrimSuffix(s, ";"))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}
