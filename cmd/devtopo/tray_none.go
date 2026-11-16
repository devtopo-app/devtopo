//go:build !(windows || linux || (darwin && cgo))

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/devtopo-app/devtopo/internal/launch"
)

// runTray, where there is no tray (a macOS build without cgo, the BSDs), starts
// DevTopo, opens it in the browser and runs until stopped.
func runTray(args []string) {
	open := startGUI(args)
	launch.FixPath()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url, _, run := startApp(ctx, nil)
	if run == nil || open {
		_ = launch.OpenBrowser(url)
	}
	if run == nil {
		return
	}
	log.Printf("DevTopo %s live at %s (no tray icon in this build)", version, shown(url))
	if err := run(); err != nil {
		log.Printf("stopped: %v", err)
	}
}
