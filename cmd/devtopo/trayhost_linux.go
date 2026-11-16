package main

import (
	"github.com/godbus/dbus/v5"
)

// watcherName is the DBus service a Linux tray (StatusNotifierItem host)
// owns; the tray icon registers with it.
const watcherName = "org.kde.StatusNotifierWatcher"

// trayHost reports whether the tray icon can run: it needs a DBus session bus.
// Without one the tray library cannot start, and DevTopo runs without an
// icon. With a bus but no tray on it, the icon still runs, as it appears once
// a tray starts, and hint says what to install.
func trayHost() (ok bool, hint string) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return false, "no DBus session bus (" + err.Error() + ")"
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return false, "cannot use the DBus session bus (" + err.Error() + ")"
	}
	if err := conn.Hello(); err != nil {
		return false, "cannot use the DBus session bus (" + err.Error() + ")"
	}
	var owned bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, watcherName).Store(&owned); err == nil && !owned {
		return true, "no system tray found (nothing owns " + watcherName + "): the tray icon shows once one runs. " +
			"On GNOME, install and enable the AppIndicator and KStatusNotifierItem Support extension. " +
			"DevTopo runs either way; open it in the browser"
	}
	return true, ""
}
