package launch

import (
	"path/filepath"
	"strings"
)

// autostartFile names DevTopo's XDG autostart entry, which Linux desktops
// run at login.
const autostartFile = "devtopo.desktop"

// autostartDir is the folder of the user's XDG autostart entries:
// $XDG_CONFIG_HOME/autostart, or ~/.config/autostart when that is unset or
// not absolute (the spec says to ignore a relative one).
func autostartDir(xdgConfigHome, home string) string {
	if xdgConfigHome != "" && filepath.IsAbs(xdgConfigHome) {
		return filepath.Join(xdgConfigHome, "autostart")
	}
	return filepath.Join(home, ".config", "autostart")
}

// autostartEntry is the desktop entry that starts exe in the tray when the
// user logs in, without opening the browser.
func autostartEntry(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=DevTopo
Comment=Starts DevTopo in the tray
Exec=` + desktopExec(exe, "tray", "--no-open") + `
Terminal=false
X-GNOME-Autostart-enabled=true
`
}

// desktopExec is the value of a desktop entry's Exec key that runs args, quoted
// per the Desktop Entry spec: an argument that is empty or has a reserved
// character (a space, a quote, a shell metacharacter) goes in double quotes,
// inside which ", `, $ and \ take a backslash; a literal % is written %%; and,
// as the key's value is a string, backslashes and control characters are
// escaped once more (so a \ in a path ends up as \\\\).
func desktopExec(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\n\r\"'\\><~|&;$*?#()`") {
			quoted[i] = a
			continue
		}
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range a {
			switch r {
			case '"', '`', '$', '\\':
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
		quoted[i] = b.String()
	}
	line := strings.ReplaceAll(strings.Join(quoted, " "), "%", "%%")
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(line)
}
