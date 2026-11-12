package cmd

import (
	"context"
	"os"
	"strconv"

	"github.com/Alain-L/quellog/analysis"
	"github.com/Alain-L/quellog/output"
	"github.com/Alain-L/quellog/parser"
	"github.com/Alain-L/quellog/trace"
)

// Slow statement traces in the reports (fork). The trace subcommand has its
// own selection flags.
var (
	tracesFlag   bool   // --traces: print only the traced statements, in full
	traceTopFlag int    // --trace-top: how many of the slowest statements to trace
	langFlag     string // --lang: language of the traces (en, fr)
	redactFlag   bool   // --redact: mask literals and parameter values everywhere
	offlineFlag  bool   // --offline: no button sending data out of the HTML report
)

func init() {
	// The command is installed as pggoulot: help and completion use that name.
	rootCmd.Use = "pggoulot [files or dirs]"

	// QUELLOG_TRACE_TOP sets the default: the regression corpus runs with 0 so
	// it keeps checking that everything else matches the original quellog.
	if v, err := strconv.Atoi(os.Getenv("QUELLOG_TRACE_TOP")); err == nil {
		analysis.TraceOptions.Top = v
	}
	rootCmd.PersistentFlags().StringVar(&langFlag, "lang", "",
		"Language of the traces: en or fr (default: QUELLOG_LANG, then LANG)")
	rootCmd.PersistentFlags().BoolVar(&redactFlag, "redact", false,
		"Mask quoted literals, query parameters and constraint key values in every output (personal data)")
	rootCmd.Flags().BoolVar(&offlineFlag, "offline", false,
		"HTML report without the buttons that send a plan to explain.dalibo.com")
	rootCmd.Flags().BoolVar(&tracesFlag, "traces", false,
		"Print only the slowest statements, traced in full (plan, parameters, temp files, timeline)")
	rootCmd.Flags().IntVar(&traceTopFlag, "trace-top", analysis.TraceOptions.Top,
		"Number of slowest statements traced in every report (0 to turn tracing off)")
}

// applyTraceFlags passes --trace-top and --lang on, before the analysis.
func applyTraceFlags() error {
	analysis.TraceOptions.Top = traceTopFlag
	output.Offline = offlineFlag
	return trace.SetLang(langFlag)
}

// redactIfAsked masks literals and parameter values in the stream when
// --redact is set.
func redactIfAsked(ctx context.Context, in <-chan []parser.LogEntry) <-chan []parser.LogEntry {
	if !redactFlag {
		return in
	}
	out := make(chan []parser.LogEntry, 64)
	go parser.RedactStream(ctx, in, out)
	return out
}
