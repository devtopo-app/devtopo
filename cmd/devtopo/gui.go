package main

import (
	"flag"
	"log"
	"os"

	"github.com/devtopo-app/devtopo/internal/launch"
)

// startGUI readies a start with no terminal, from a double-click, the Finder
// or login: output goes to ~/.devtopo/devtopo.log. It returns whether to
// open the browser once DevTopo runs. The caller then fixes the PATH on macOS
// and Linux (launch.FixPath), off the tray's thread, as it asks the shell.
func startGUI(args []string) (open bool) {
	fs := flag.NewFlagSet("tray", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "do not open the browser on start (used at login)")
	_ = fs.Parse(args)

	data := launch.Data()
	_ = privateDir(data.Dir)
	privateFiles(data.Log)
	if f, err := os.OpenFile(data.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		os.Stdout, os.Stderr = f, f
		log.SetOutput(f)
	}
	return !*noOpen
}
