package goodconnect

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReqHost(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"HTTPS with path", "https://syosetu.org/novel/272610/", "syosetu.org"},
		{"HTTP without path", "http://example.com", "example.com"},
		{"short host", "https://a.io/path", "a.io"},
		{"query and fragment", "https://example.com?next=/chapter#section", "example.com"},
		{"explicit port", "https://example.com:8443/path", "example.com:8443"},
		{"loopback port", "http://127.0.0.1:12345/path", "127.0.0.1:12345"},
		{"IPv6 port", "https://[2001:db8::1]:8443/path", "[2001:db8::1]:8443"},
		{"scheme relative", "//example.com/path", "example.com"},
		{"userinfo", "https://user:password@example.com/path", "example.com"},
		{"invalid URL", "https://%gh/path", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reqHost(tc.raw); got != tc.want {
				t.Fatalf("reqHost(%q) = %q; want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// Both browser solvers use reqHost + storeOne. Exercise that same handoff
// through an actual HTTP request, without launching a browser or contacting a site.
func TestBrowserClearanceCookieReusedByHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("cf_clearance")
		if err != nil || cookie.Value != "local-clearance" {
			http.Error(w, "clearance missing", http.StatusForbidden)
			return
		}
		io.WriteString(w, "cleared")
	}))
	defer server.Close()
	pageURL := server.URL + "/novel/272610/"
	transport := NewTransport(Options{})
	defer transport.CloseIdleConnections()
	transport.storeOne(reqHost(pageURL), "cf_clearance", "local-clearance", "/")
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get(pageURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP retry did not receive the browser cookie: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "cleared" {
		t.Fatalf("response %q, error %v", body, err)
	}
}
