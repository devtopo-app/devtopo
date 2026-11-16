package launch

import (
	"os"
	"path/filepath"
)

// autostartPath is where DevTopo's XDG autostart entry lives.
func autostartPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(autostartDir(os.Getenv("XDG_CONFIG_HOME"), home), autostartFile)
}

// StartsAtLogin reports whether DevTopo's autostart entry is installed.
func StartsAtLogin() bool {
	_, err := os.Stat(autostartPath())
	return err == nil
}

// SetStartAtLogin installs the XDG autostart entry that starts this program at
// login, or removes it.
func SetStartAtLogin(on bool) error {
	p := autostartPath()
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(autostartEntry(exe)), 0o644)
}
