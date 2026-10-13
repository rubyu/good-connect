// This file is based on the browser emulation of cloudscraper
// (https://github.com/VeNoMouS/cloudscraper, MIT License; the browser='chrome'
// equivalent). See README.md / LICENSE for background and license details.
package goodconnect

import "net/http"

// DefaultUserAgent returns the default browser-like User-Agent:
// a Chrome-like string equivalent to cloudscraper's browser emulation.
func DefaultUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
}

// BrowserHeaders returns the default browser-like headers, equivalent to
// cloudscraper's requests header emulation. User-supplied headers take
// precedence (applyHeaders does not override them).
func BrowserHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", DefaultUserAgent())
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	h.Set("Accept-Language", "ja-JP,ja;q=0.9,en-US;q=0.8,en;q=0.7")
	h.Set("Upgrade-Insecure-Requests", "1")
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("Sec-Fetch-User", "?1")
	h.Set("Sec-Ch-Ua", `"Chromium";v="125", "Google Chrome";v="125", "Not.A/Brand";v="24"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	// Accept-Encoding is intentionally not set: net/http adds "gzip" automatically
	// and decompresses transparently.
	return h
}

// applyHeaders applies browser-like headers to the request. Headers that are
// already set are treated as user-supplied and take precedence.
func applyHeaders(req *http.Request, ua string, extra http.Header) {
	if req.Header.Get("User-Agent") == "" && ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	for k, vs := range extra {
		for _, v := range vs {
			if req.Header.Get(k) == "" {
				req.Header.Set(k, v)
			}
		}
	}
}
