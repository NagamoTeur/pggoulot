// Package cmd implements the command-line interface for quellog.
package cmd

import (
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/term"
)

// openInBrowser launches the system default handler for path. Skipped
// silently when stderr is not a TTY or when CI is set, to avoid
// surprising headless / cron / pipeline runs. Fire-and-forget — the
// browser keeps running after quellog exits.
func openInBrowser(path string) {
	if !term.IsTerminal(int(os.Stderr.Fd())) || os.Getenv("CI") != "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// Not "cmd /C start": cmd.exe would interpret & | ^ in the file name
		// (a log named "x&calc.log" would run calc). rundll32 takes the path
		// as a plain argument.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("could not open report in browser", "path", path, "error", err)
		return
	}
	_ = cmd.Process.Release()
}
