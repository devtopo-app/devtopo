// Package launch starts DevTopo the way a desktop app starts: on the first
// free port, once per user, and from a tray icon.
package launch

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PingReply is what GET /api/ping answers, so a second start can tell a
// running DevTopo from another program on the port.
const PingReply = "devtopo"

// Ports are where DevTopo listens: 8787, else the next free one up to 8796.
var Ports = []int{8787, 8788, 8789, 8790, 8791, 8792, 8793, 8794, 8795, 8796}

// Listen binds the first free port of ports on host. When a port is taken by
// a DevTopo that is already running, it returns that one's address instead
// and no listener, so the caller opens it rather than starting a second copy.
// It fails only when every port is taken by something else.
func Listen(host string, ports []int) (net.Listener, string, error) {
	for _, p := range ports {
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, "", nil
		}
		if isDevTopo(addr) {
			return nil, addr, nil
		}
	}
	return nil, "", fmt.Errorf("every port from %d to %d is taken", ports[0], ports[len(ports)-1])
}

// isDevTopo reports whether DevTopo answers at addr.
func isDevTopo(addr string) bool {
	return pingReply(addr, 2000) == PingReply
}

// pingReply is what addr answers on /api/ping within timeout milliseconds; ""
// when it does not answer.
func pingReply(addr string, timeout int) string {
	c := http.Client{Timeout: time.Duration(timeout) * time.Millisecond}
	resp, err := c.Get("http://" + addr + "/api/ping")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.TrimSpace(string(b))
}
