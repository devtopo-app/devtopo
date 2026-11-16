package launch

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// freePorts returns two ports that were free a moment ago, next to each other.
func freePorts(t *testing.T) (int, int) {
	t.Helper()
	for i := 0; i < 20; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1)); err == nil {
			l2.Close()
			return p, p + 1
		}
	}
	t.Skip("no two free ports in a row")
	return 0, 0
}

func TestListenTakesTheNextPortWhenTheFirstIsTaken(t *testing.T) {
	a, b := freePorts(t)
	other, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a)) // some other program
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	ln, running, err := Listen("127.0.0.1", []int{a, b})
	if err != nil || running != "" {
		t.Fatalf("err=%v running=%q", err, running)
	}
	defer ln.Close()
	if got := ln.Addr().(*net.TCPAddr).Port; got != b {
		t.Fatalf("want the backup port %d, got %d", b, got)
	}
}

func TestListenFindsADevTopoAlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ping" {
			fmt.Fprint(w, PingReply)
		}
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])

	ln, running, err := Listen("127.0.0.1", []int{port})
	if err != nil || ln != nil || running != srv.URL[len("http://"):] {
		t.Fatalf("a running DevTopo is found, not replaced: ln=%v running=%q err=%v", ln, running, err)
	}
}

func TestListenFailsWhenEveryPortIsTaken(t *testing.T) {
	a, _ := freePorts(t)
	other, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if ln, running, err := Listen("127.0.0.1", []int{a}); err == nil || ln != nil || running != "" {
		t.Fatalf("want an error, got ln=%v running=%q", ln, running)
	}
}
