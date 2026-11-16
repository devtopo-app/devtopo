package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// An app started from the Finder, a Linux desktop's app menu or at login gets
// only the system PATH (the shell rc files, which add ~/.local/bin, npm's
// global bin and the like, never run), so claude, codex and gh look missing.
// FixPath adds the PATH the user's own shell has, plus the usual install
// folders. It is for macOS and Linux, whose PATH separator is ":".

const pathMarker = "__DEVTOPO_PATH__"

// FixPath extends this process's PATH with the login shell's and the common
// install folders, so the agent CLIs and gh are found.
func FixPath() {
	home, _ := os.UserHomeDir()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
		if runtime.GOOS == "linux" {
			shell = "/bin/bash"
		}
	}
	more := []string{shellPATH(shell, 5*time.Second), "/usr/local/bin"}
	if runtime.GOOS == "darwin" {
		more = append(more, "/opt/homebrew/bin")
	} else {
		more = append(more, "/home/linuxbrew/.linuxbrew/bin", "/snap/bin")
	}
	for _, d := range []string{".local/bin", ".npm-global/bin", ".volta/bin", ".bun/bin"} {
		more = append(more, filepath.Join(home, d))
	}
	os.Setenv("PATH", mergePath(os.Getenv("PATH"), more...))
}

// shellPATH asks the login shell for its PATH, giving up after timeout; ""
// when it gives none. -i as well as -l: nvm and similar set their PATH in the
// interactive rc file. A program the rc files leave running in the background
// keeps the output open after the shell exits, so the output is not waited on
// for more than a second after the shell is done or stopped.
func shellPATH(shell string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", "printf '"+pathMarker+"%s' \"$PATH\"")
	cmd.WaitDelay = time.Second
	out, _ := cmd.Output()
	return shellPath(string(out))
}

// shellPath is the PATH a shell printed after the marker; "" when it printed none.
func shellPath(out string) string {
	_, after, ok := strings.Cut(out, pathMarker)
	if !ok {
		return ""
	}
	return strings.TrimSpace(after)
}

// mergePath appends to current each folder of more (each may be a PATH list)
// that it does not have yet.
func mergePath(current string, more ...string) string {
	var dirs []string
	seen := map[string]bool{}
	for _, list := range append([]string{current}, more...) {
		for _, d := range strings.Split(list, ":") {
			if d != "" && !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return strings.Join(dirs, ":")
}
