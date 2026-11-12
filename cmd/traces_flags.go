package cmd

import (
	"os"
	"strconv"

	"github.com/Alain-L/quellog/analysis"
	"github.com/Alain-L/quellog/trace"
)

// Slow statement traces in the reports (fork). The trace subcommand has its
// own selection flags.
var (
	tracesFlag   bool   // --traces: print only the traced statements, in full
	traceTopFlag int    // --trace-top: how many of the slowest statements to trace
	langFlag     string // --lang: language of the traces (en, fr)
)

func init() {
	// QUELLOG_TRACE_TOP sets the default: the regression corpus runs with 0 so
	// it keeps checking that everything else matches the original quellog.
	if v, err := strconv.Atoi(os.Getenv("QUELLOG_TRACE_TOP")); err == nil {
		analysis.TraceOptions.Top = v
	}
	rootCmd.PersistentFlags().StringVar(&langFlag, "lang", "",
		"Language of the traces: en or fr (default: QUELLOG_LANG, then LANG)")
	rootCmd.Flags().BoolVar(&tracesFlag, "traces", false,
		"Print only the slowest statements, traced in full (plan, parameters, temp files, timeline)")
	rootCmd.Flags().IntVar(&traceTopFlag, "trace-top", analysis.TraceOptions.Top,
		"Number of slowest statements traced in every report (0 to turn tracing off)")
}

// applyTraceFlags passes --trace-top and --lang on, before the analysis.
func applyTraceFlags() error {
	analysis.TraceOptions.Top = traceTopFlag
	return trace.SetLang(langFlag)
}
