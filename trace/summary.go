package trace

import "time"

// TimeSplit says where a statement's time went, as far as the logs tell.
type TimeSplit struct {
	Known       bool    // an auto_explain plan gave the executor time
	ExecutorMs  float64 // in the executor (auto_explain)
	AfterMs     float64 // the rest of the statement's duration
	Verdict     string  // "client", "outside", "executor" or "unknown"
	Explanation string
}

// TimeSplit compares the executor time logged by auto_explain with the
// statement's duration.
func (t *Trace) TimeSplit() TimeSplit {
	if t.Plan == nil {
		return TimeSplit{Verdict: "unknown", Explanation: T("split.unknown")}
	}
	s := TimeSplit{Known: true, ExecutorMs: t.Plan.DurationMs, AfterMs: t.DurationMs - t.Plan.DurationMs}
	if s.AfterMs < 0 {
		s.AfterMs = 0
	}
	switch {
	case t.IsSQLExecute() && s.AfterMs > s.ExecutorMs:
		s.Verdict = "client"
		s.Explanation = T("split.client", HumanDuration(s.ExecutorMs), HumanDuration(s.AfterMs))
		if r := t.StoredResult(); r != nil {
			s.Explanation += T("split.client.stored", HumanBytes(r.Bytes))
		}
	case s.AfterMs > 0.2*t.DurationMs:
		s.Verdict = "outside"
		s.Explanation = T("split.outside", Percent(t.share(s.AfterMs)))
	default:
		s.Verdict = "executor"
		s.Explanation = T("split.executor")
	}
	return s
}

// StoredResult is the backend's temp file freed when an SQL EXECUTE ends:
// the tuplestore that held the whole result. nil for other statements.
func (t *Trace) StoredResult() *Event {
	if !t.IsSQLExecute() {
		return nil
	}
	for i := len(t.TempFiles) - 1; i >= 0; i-- {
		f := &t.TempFiles[i]
		if f.Role == "backend" && !f.Time.Before(t.End.Truncate(time.Second)) {
			return f
		}
	}
	return nil
}

// TempBytes is the total size of the trace's temp files.
func (t *Trace) TempBytes() int64 {
	var total int64
	for _, f := range t.TempFiles {
		total += f.Bytes
	}
	return total
}
