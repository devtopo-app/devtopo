// Package update tells whether a newer DevTopo release is out. It asks
// GitHub's public API for the latest published release of the repository the
// release build names, compares its tag with the running version, and works
// out from where the program is installed how to upgrade (Homebrew, Scoop or a
// download). It never downloads or installs anything, and sends nothing about
// the user: one unauthenticated GET of public release metadata.
package update

import (
	"path"
	"strconv"
	"strings"
)

// SettingKey is the store setting that turns the check off ("off"); unset or
// any other value leaves it on.
const SettingKey = "update_check"

// EnabledIn reads SettingKey through get (the store's GetSetting). The check is
// on unless the setting says "off"; a read error keeps the default.
func EnabledIn(get func(string) (string, bool, error)) bool {
	v, ok, err := get(SettingKey)
	if err != nil || !ok {
		return true
	}
	return strings.TrimSpace(v) != "off"
}

// semver is a parsed vMAJOR.MINOR.PATCH[-pre][+build].
type semver struct {
	nums [3]int
	pre  []string
}

func parse(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var s semver
	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core = v[:i]
		if v[i+1:] == "" {
			return s, false
		}
		s.pre = strings.Split(v[i+1:], ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.nums[i] = n
	}
	return s, true
}

// Valid reports whether v is a semantic version (v prefix optional).
func Valid(v string) bool {
	_, ok := parse(v)
	return ok
}

// compare returns -1, 0 or 1 as a is below, equal to or above b, by semver
// precedence: a prerelease is below its release.
func compare(a, b semver) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			return sign(a.nums[i] - b.nums[i])
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				return sign(xn - yn)
			}
		case xerr == nil: // numeric identifiers are below alphanumeric ones
			return -1
		case yerr == nil:
			return 1
		case x != y:
			return sign(strings.Compare(x, y))
		}
	}
	return sign(len(a.pre) - len(b.pre))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// Newer reports whether latest is a higher version than current. Either one
// not being a semantic version ("dev", a nightly tag) means no.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	return ok1 && ok2 && compare(l, c) > 0
}

// DirectHint is the upgrade hint for a program installed by hand.
const DirectHint = "download from the release page"

// Install is how the running program was installed and how to upgrade it.
type Install struct {
	Method string // "homebrew", "scoop" or "direct"
	Name   string // the formula, cask or app name the package manager knows
	Hint   string
}

// DetectInstall works out the install method from the executable's path
// (symlinks already resolved): a Homebrew Cellar or Caskroom, a Homebrew
// prefix, a Scoop apps folder, else a direct download. The package name is
// read from the path, never assumed. It is pure, so it is testable for every
// OS on any OS.
func DetectInstall(exe string) Install {
	p := strings.ReplaceAll(exe, `\`, "/")
	segs := strings.Split(p, "/")
	after := func(dir string) string {
		for i := 0; i+1 < len(segs)-1; i++ {
			if strings.EqualFold(segs[i], dir) && segs[i+1] != "" {
				return segs[i+1]
			}
		}
		return ""
	}
	if name := after("Cellar"); name != "" {
		return Install{Method: "homebrew", Name: name, Hint: "brew upgrade " + name}
	}
	if name := after("Caskroom"); name != "" {
		return Install{Method: "homebrew", Name: name, Hint: "brew upgrade --cask " + name}
	}
	if strings.HasPrefix(p, "/opt/homebrew/") || strings.HasPrefix(p, "/home/linuxbrew/.linuxbrew/") {
		name := path.Base(p)
		return Install{Method: "homebrew", Name: name, Hint: "brew upgrade " + name}
	}
	lower := strings.ToLower(p)
	if i := strings.Index(lower, "/scoop/apps/"); i >= 0 {
		rest := p[i+len("/scoop/apps/"):]
		if j := strings.IndexByte(rest, '/'); j > 0 {
			name := rest[:j]
			return Install{Method: "scoop", Name: name, Hint: "scoop update " + name}
		}
	}
	return Install{Method: "direct", Hint: DirectHint}
}
