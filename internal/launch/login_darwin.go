package launch

import (
	"os"
	"path/filepath"
)

// agentPath is where macOS looks for the user's LaunchAgents.
func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

// StartsAtLogin reports whether DevTopo's LaunchAgent is installed.
func StartsAtLogin() bool {
	_, err := os.Stat(agentPath())
	return err == nil
}

// SetStartAtLogin installs the LaunchAgent that starts this program at login,
// or removes it.
func SetStartAtLogin(on bool) error {
	if !on {
		if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(agentPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(agentPath(), []byte(launchAgent(exe)), 0o644)
}
