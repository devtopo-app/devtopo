package main

import (
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestRequireLoopback(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787", "127.0.0.2:9000"} {
		if err := requireLoopback(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:8787", ":8787", "192.168.1.5:8787", "example.com:80", "nonsense"} {
		if err := requireLoopback(bad); err == nil {
			t.Errorf("%s must be refused", bad)
		}
	}
}

// The key is made once and kept, so a second start and a restarted DevTopo
// use the same one.
func TestAccessKeyIsMadeOnceAndKept(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	k1, err := accessKey(st)
	if err != nil || len(k1) != 48 {
		t.Fatalf("key %q: %v", k1, err)
	}
	if k2, _ := accessKey(st); k2 != k1 {
		t.Fatalf("a second call made a new key: %q", k2)
	}
	if shown(signedIn("http://127.0.0.1:8787", k1)) != "http://127.0.0.1:8787" {
		t.Fatal("the shown address must leave the key out")
	}
}
