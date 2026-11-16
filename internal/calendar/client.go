package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxFeedBytes bounds a feed download; a larger feed is an error rather than
// a silently truncated (and so partly missing) calendar.
const maxFeedBytes = 8 << 20

// Client fetches an iCal feed. httpDo is injectable so tests never hit the
// network. The feed URL is a secret, so errors never include it.
type Client struct {
	httpDo func(*http.Request) (*http.Response, error)
}

// NewClient builds the production client.
func NewClient() *Client {
	hc := &http.Client{Timeout: 20 * time.Second}
	return &Client{httpDo: hc.Do}
}

// Fetch GETs the iCal feed at url and returns its bytes. A feed over 8MB is an
// error. A webcal:// address is fetched over https. An error never contains the
// url (a network error can carry the address, so it is not wrapped).
func (c *Client) Fetch(ctx context.Context, url string) ([]byte, error) {
	if len(url) >= len("webcal://") && strings.EqualFold(url[:len("webcal://")], "webcal://") {
		url = "https://" + url[len("webcal://"):]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		// A request-build error can quote the url; return a generic message.
		return nil, fmt.Errorf("calendar: invalid URL")
	}
	resp, err := c.httpDo(req)
	if err != nil {
		return nil, fmt.Errorf("calendar: fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("calendar: unexpected status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("calendar: read failed")
	}
	if len(raw) > maxFeedBytes {
		return nil, fmt.Errorf("calendar: feed larger than %d MB", maxFeedBytes>>20)
	}
	return raw, nil
}
