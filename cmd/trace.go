package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Alain-L/quellog/analysis"
	"github.com/Alain-L/quellog/output"
	"github.com/Alain-L/quellog/parser"
	"github.com/Alain-L/quellog/trace"
)

var (
	tracePID         string
	traceQuery       string
	traceTop         int
	traceMinDuration time.Duration
)

// traceCmd follows one slow statement through the logs: prepared text,
// parameters, its own auto_explain plan, temp files of the backend and of its
// parallel workers, and where the time went.
var traceCmd = &cobra.Command{
	Use:   "trace [files or dirs]",
	Short: "Follow the slowest statements through the logs (plan, parameters, temp files, timeline)",
	Long: `trace rebuilds the story of slow statements from the logs.

For each of the slowest statements (or those picked with --pid / --query) it
shows the prepared text and parameters, the auto_explain plan that really
belongs to it, the temporary files of the backend and of its parallel workers,
where the time went (executor versus sending to the client), a query ready to
replay with EXPLAIN, and everything the backend logged while it ran.

Time filters (--begin, --end, --last, --window) and --dbname/--dbuser/--appname
choose which statements are traced; the entries around them are always read in
full, since parallel workers log without database or user. The logs are read
once, so standard input works ("-", or no argument).

The main report traces the slowest statements too (--trace-top, --traces).`,
	Args: cobra.ArbitraryArgs,
	RunE: runTrace,
}

func init() {
	traceCmd.Flags().StringVarP(&tracePID, "pid", "p", "", "Only statements of this backend PID")
	traceCmd.Flags().StringVar(&traceQuery, "query", "", "Only statements whose text or prepared text contains this (case-insensitive)")
	traceCmd.Flags().IntVarP(&traceTop, "top", "n", 1, "Number of statements to trace, slowest first")
	traceCmd.Flags().DurationVar(&traceMinDuration, "min-duration", 0, "Ignore statements faster than this (e.g. 1m)")
	rootCmd.AddCommand(traceCmd)
}

func runTrace(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		args = []string{"-"}
	}
	files := collectFiles(args)
	if len(files) == 0 {
		return fmt.Errorf("no log files found")
	}
	if err := validateStdinUsage(files); err != nil {
		return err
	}
	files = dropDuplicateFormats(files)
	if err := trace.SetLang(langFlag); err != nil {
		return err
	}
	if err := validateTimeFilters(); err != nil {
		return err
	}
	var beginT, endT time.Time
	var err error
	if lastFlag != "" {
		beginT, endT, err = parseLast(lastFlag)
	} else {
		beginT, endT, err = parseDateTimes(beginTime, endTime)
		if err == nil {
			var w time.Duration
			if w, err = parseWindow(windowFlag); err == nil {
				beginT, endT = applyTimeWindow(beginT, endT, w)
			}
		}
	}
	if err != nil {
		return err
	}
	filters := buildLogFilters(beginT, endT)

	opt := trace.Options{PID: tracePID, Contains: traceQuery, MinDuration: traceMinDuration, Top: traceTop}
	if opt.Top <= 0 {
		opt.Top = 1
	}
	if !filters.IsEmpty() {
		opt.Accept = func(e *parser.LogEntry) bool { return parser.PassesFilters(*e, filters) }
	}
	// Long queries keep their lines too: the trace shows them as written.
	parser.PreserveContinuationNewlines = true
	tracer := trace.NewTracer(opt)
	if err := readEntries(cmd.Context(), files, tracer.Process); err != nil {
		return err
	}
	traces := tracer.Finalize()
	if len(traces) == 0 {
		fmt.Println(trace.T("label.none"))
		return nil
	}
	color := term.IsTerminal(int(os.Stdout.Fd()))
	for _, t := range traces {
		output.RenderTrace(os.Stdout, t, color)
	}
	return nil
}

// dropDuplicateFormats keeps one file per log when PostgreSQL writes several
// destinations (log_destination = 'stderr,jsonlog' gives x.log and x.json):
// reading both would trace every statement twice. stderr wins.
func dropDuplicateFormats(files []string) []string {
	stems := map[string]bool{}
	for _, f := range files {
		if ext := filepath.Ext(f); ext == ".log" {
			stems[strings.TrimSuffix(f, ext)] = true
		}
	}
	var out []string
	for _, f := range files {
		ext := filepath.Ext(f)
		if (ext == ".json" || ext == ".csv") && stems[strings.TrimSuffix(f, ext)] {
			slog.Info("skipping duplicate destination", "file", f)
			continue
		}
		out = append(out, f)
	}
	return out
}

// readEntries parses the files one after the other, in order ("-" is
// stdin), and numbers the entries so those sharing a timestamp keep their
// file order.
func readEntries(ctx context.Context, files []string, fn func(*parser.LogEntry)) error {
	var seq int64
	for _, f := range files {
		ch := make(chan []parser.LogEntry, 16)
		errc := make(chan error, 1)
		go func() {
			if f == "-" {
				errc <- parser.ParseStdin(ch)
			} else {
				errc <- parser.ParseFile(f, ch)
			}
			close(ch)
		}()
		for batch := range ch {
			if ctx.Err() != nil {
				continue
			}
			for i := range batch {
				e := &batch[i]
				seq++
				e.Seq = seq
				if redactFlag {
					e.Message = parser.RedactMessage(e.Message)
				}
				analysis.StampBodyOffset(e)
				fn(e)
			}
		}
		if err := <-errc; err != nil {
			slog.Warn("could not parse file", "file", f, "err", err)
		}
	}
	return ctx.Err()
}
