package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/devtopo-app/devtopo/internal/store"
)

// privateDir makes dir, readable only by this user: it holds the database,
// with the connectors' tokens, and the log. An existing folder is tightened.
// (On Windows the folder is already private to the user's profile.)
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	return nil
}

// privateFiles makes each existing path readable only by this user. SQLite
// gives its -wal and -shm files the database file's own permissions.
func privateFiles(paths ...string) {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

// accessKey is the key a browser must hold to use DevTopo's API (see
// server.SetAccessKey): made once per install and kept in the store, so a tab
// stays signed in when DevTopo restarts, and a second start can open the
// running one.
func accessKey(st *store.Store) (string, error) {
	if k, ok, err := st.GetSetting("access_key"); err != nil || (ok && k != "") {
		return k, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	k := hex.EncodeToString(b)
	return k, st.SetSetting("access_key", k)
}

// signedIn is the address to open: base with the access key, which the page
// swaps for a cookie and drops from the address.
func signedIn(base, key string) string { return base + "/?k=" + key }

// shown is an address as it may be logged or shown in a tooltip: without the key.
func shown(url string) string { u, _, _ := strings.Cut(url, "/?"); return u }

// requireLoopback refuses an address other machines could reach: the API
// trusts the loopback Host header, which a remote client could fake.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad --addr %q", addr)
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("--addr %q is not a loopback address: DevTopo serves this machine only (use 127.0.0.1)", addr)
}
