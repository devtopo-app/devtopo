//go:build windows || linux || (darwin && cgo)

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"fyne.io/systray"

	"github.com/devtopo-app/devtopo/internal/launch"
	"github.com/devtopo-app/devtopo/internal/update"
)

// The macOS menu bar must be driven from the main thread, and the Windows tray
// from the thread that made its window. The Linux tray talks DBus from any
// goroutine, so it needs no thread of its own.
func init() {
	if runtime.GOOS != "linux" {
		runtime.LockOSThread()
	}
}

// started is what starting DevTopo gives the tray: the address it serves, the
// update checker, and run, which is nil when DevTopo already runs elsewhere.
type started struct {
	url     string
	updates *update.Checker
	run     func() error
}

// runTray runs DevTopo in the background with a tray icon (the menu bar on
// macOS): Open DevTopo, Start at login, Quit. A second start opens the
// running one in the browser instead. Where no tray can show the icon (a Linux
// desktop without one), it keeps running and serving the page, and stops on
// Ctrl-C or SIGTERM.
//
// The tray starts first, on the main thread, and DevTopo starts beside it:
// starting asks the login shell and the agent CLIs, which can take a while,
// and macOS reports an app whose main thread is not handling events as not
// responding.
func runTray(args []string) {
	open := startGUI(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tray, hint := trayHost()
	trayUp := make(chan struct{}) // closed once the tray runs, so it can be quit
	quitTray := func() {
		if tray {
			<-trayUp
			systray.Quit()
		}
	}
	ready := make(chan started, 1)
	done := make(chan error, 1)
	go func() {
		if runtime.GOOS != "windows" {
			launch.FixPath()
		}
		url, updates, run := startApp(ctx, nil)
		ready <- started{url, updates, run}
		if run == nil {
			_ = launch.OpenBrowser(url)
			done <- nil
			quitTray()
			return
		}
		log.Printf("DevTopo %s live at %s", version, shown(url))
		if open {
			_ = launch.OpenBrowser(url)
		}
		done <- run()
		quitTray()
	}()

	if !tray {
		log.Printf("no tray icon: %s; DevTopo keeps running", hint)
	} else {
		if hint != "" {
			log.Print(hint)
		}
		systray.Run(func() {
			close(trayUp)
			trayMenu(ready, stop)
		}, nil)
		stop()
	}
	if err := <-done; err != nil {
		log.Printf("stopped: %v", err)
	}
}

// trayMenu sets the icon and the menu, and acts on its clicks once DevTopo
// has started (until then Open DevTopo is greyed out). When the update check
// finds a newer release, a quiet "Update available" item appears that opens
// the release page; the checker is read every minute from the same loop that
// handles the clicks, so the item's state has one owner.
func trayMenu(ready <-chan started, quit func()) {
	if runtime.GOOS == "windows" {
		systray.SetIcon(launch.IconICO())
	} else {
		systray.SetIcon(launch.IconPNG())
	}
	if runtime.GOOS == "linux" {
		systray.SetTitle("DevTopo") // the name tray settings list it under
	}
	systray.SetTooltip("DevTopo - starting")
	about := systray.AddMenuItem("DevTopo "+version, "")
	about.Disable()
	updateItem := systray.AddMenuItem("Update available", "Open the release page")
	updateItem.Hide()
	systray.AddSeparator()
	openItem := systray.AddMenuItem("Open DevTopo", "Open DevTopo in the browser")
	openItem.Disable()
	login := systray.AddMenuItemCheckbox("Start at login", "Start DevTopo when you log in", launch.StartsAtLogin())
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Stop DevTopo")
	go func() {
		var s started
		select {
		case s = <-ready:
		case <-quitItem.ClickedCh:
			quit()
			return
		}
		if s.run == nil {
			return // another DevTopo runs; this one is quitting
		}
		url, updates := s.url, s.updates
		systray.SetTooltip("DevTopo - " + shown(url))
		openItem.Enable()
		shown, releaseURL := "", ""
		poll := time.NewTicker(time.Minute)
		defer poll.Stop()
		for {
			select {
			case <-poll.C:
				if st := updates.Status(); st.Available && st.Latest != shown {
					shown, releaseURL = st.Latest, st.URL
					updateItem.SetTitle("Update available: " + st.Latest)
					updateItem.Show()
				}
			case <-updateItem.ClickedCh:
				if releaseURL != "" {
					_ = launch.OpenBrowser(releaseURL)
				}
			case <-openItem.ClickedCh:
				_ = launch.OpenBrowser(url)
			case <-login.ClickedCh:
				if err := launch.SetStartAtLogin(!login.Checked()); err != nil {
					log.Printf("start at login: %v", err)
				} else if login.Checked() {
					login.Uncheck()
				} else {
					login.Check()
				}
			case <-quitItem.ClickedCh:
				quit()
				return
			}
		}
	}()
}
