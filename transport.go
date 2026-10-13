// This file is a Go port of the design of the CloudflareAdapter (cloudflare.py) from
// cloudscraper (https://github.com/VeNoMouS/cloudscraper, MIT License).
// See README.md / LICENSE for background and license details.
package goodconnect

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Transport wraps an http.RoundTripper and applies cloudscraper-like processing:
// browser emulation, per-host rate limiting (default: at most 1 request per second),
// and automatic solving of Cloudflare v1 challenges. Use it the same way as
// http.DefaultTransport (http.Client{Transport: t}).
type Transport struct {
	base        http.RoundTripper
	limiter     *hostLimiter
	ua          string
	extra       http.Header
	maxSolve    int
	enablePW    bool
	obscuraPath string
	debug       *log.Logger

	mu      sync.Mutex
	cookies map[string][]*httpCookie

	// Per-host lock for challenge solving: even when many requests to the same
	// host arrive concurrently, only one browser (playwright/obscura) is launched
	// at a time.
	solveMu sync.Mutex
	solving map[string]*sync.Mutex
}

type httpCookie struct {
	name  string
	value string
	path  string
	exp   time.Time
}

// NewTransport returns a Transport with browser emulation, rate limiting, and
// challenge solving. If Options.MaxQPS is 0, it defaults to 1 (at most one
// request per second).
func NewTransport(o Options) *Transport {
	base := o.Base
	if base == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		tr.ForceAttemptHTTP2 = true
		tr.MaxIdleConnsPerHost = 4
		tr.MaxIdleConns = 8
		base = tr
	}
	ua := o.UserAgent
	if ua == "" {
		ua = DefaultUserAgent()
	}
	extra := o.Headers

	maxSolve := o.MaxSolveAttempts
	if maxSolve <= 0 {
		maxSolve = 3
	}
	b := o.Burst
	if b <= 0 {
		b = 1
	}
	return &Transport{
		base:        base,
		limiter:     newHostLimiter(o.MaxQPS, b),
		ua:          ua,
		extra:       extra,
		maxSolve:    maxSolve,
		debug:       o.Debug,
		enablePW:    o.EnablePlaywright,
		obscuraPath: o.ObscuraPath,
		cookies:     make(map[string][]*httpCookie),
		solving:     make(map[string]*sync.Mutex),
	}
}

// RoundTrip implements http.RoundTripper:
//  1. Per-host rate limiting (default: at most 1 request per second)
//  2. Browser-like headers / stored cookies
//  3. Cloudflare v1 challenge detection → auto-solve → resend → redirect following
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTrip(req, 0)
}

func (t *Transport) roundTrip(req *http.Request, attempt int) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if t.debug != nil {
		t.debug.Printf("%s %s (attempt %d)", req.Method, req.URL.String(), attempt)
	}
	if err := t.limiter.wait(req.Context(), req.URL.Host); err != nil {
		return nil, err
	}

	req2 := req.Clone(req.Context())
	applyHeaders(req2, t.ua, t.extra)
	t.attachCookies(req2)

	resp, err := t.base.RoundTrip(req2)
	if err != nil {
		return nil, err
	}
	t.captureCookies(resp)

	if resp.StatusCode != 403 && resp.StatusCode != 429 && resp.StatusCode != 503 {
		return resp, nil
	}
	if !strings.HasPrefix(resp.Header.Get("Server"), "cloudflare") {
		return resp, nil
	}

	body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxChallengeBody))
	resp.Body.Close()
	if rerr != nil {
		return nil, rerr
	}
	if !isCFChallengeBody(resp.StatusCode, resp.Header, string(body)) {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}
	if attempt >= t.maxSolve {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	answer, serr := SolveJschl(string(body), req.URL.Host)
	if serr != nil {
		if t.debug != nil {
			t.debug.Printf("challenge solve failed: %v", serr)
		}
		if t.enablePW || t.obscuraPath != "" {
			// Serialize solvers per host. If a solver stored a cf_clearance for this
			// host while we were waiting, retry without solving ourselves.
			m := t.solveMuFor(req.URL.Host)
			m.Lock()
			var perr error
			if !t.hasStored(req.URL.Host, "cf_clearance") {
				if t.obscuraPath != "" {
					perr = t.solveWithObscura(req.Context(), t.obscuraPath, req.URL.String())
				} else {
					perr = t.solveWithPlaywright(req.Context(), req.URL.String())
				}
			}
			m.Unlock()
			if perr != nil && t.debug != nil {
				t.debug.Printf("solve: %v", perr)
			}
			if perr == nil {
				resp.Body = io.NopCloser(bytes.NewReader(body))
				return t.roundTrip(req, attempt+1)
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}
	if ms, ok := challengeDelay(string(body)); ok && ms > 0 {
		wait := time.Duration(ms) * time.Millisecond
		if t.debug != nil {
			t.debug.Printf("challenge delay %dms", ms)
		}
		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	action, fields, ferr := challengeForm(string(body), req.URL)
	if ferr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}
	fields["jschl_answer"] = answer
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}

	// Submit the challenge answer (equivalent to cloudscraper's Challenge_Response).
	post, perr := http.NewRequestWithContext(req.Context(), http.MethodPost, action, strings.NewReader(form.Encode()))
	if perr != nil {
		return nil, perr
	}
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", schemeOf(req.URL))
	post.Header.Set("Referer", req.URL.String())

	submitResp, xerr := t.roundTrip(post, attempt+1)
	if xerr != nil {
		return nil, xerr
	}
	if submitResp.StatusCode >= 300 && submitResp.StatusCode < 400 {
		loc := submitResp.Header.Get("Location")
		if loc == "" {
			return submitResp, nil
		}
		locURL, lerr := resolveURL(submitResp.Request.URL, loc)
		if lerr != nil {
			return submitResp, nil
		}
		submitResp.Body.Close()
		followReq, ferr := http.NewRequestWithContext(req.Context(), http.MethodGet, locURL, nil)
		if ferr != nil {
			return nil, ferr
		}
		followReq.Header.Set("Referer", submitResp.Request.URL.String())
		return t.roundTrip(followReq, attempt+1)
	}
	return submitResp, nil
}

func schemeOf(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func resolveURL(base *url.URL, ref string) (string, error) {
	if base == nil {
		return ref, nil
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	return u.String(), nil
}

// solveMuFor returns the per-host challenge-solving lock (lazily created, thread-safe).
func (t *Transport) solveMuFor(host string) *sync.Mutex {
	t.solveMu.Lock()
	defer t.solveMu.Unlock()
	m, ok := t.solving[host]
	if !ok {
		m = &sync.Mutex{}
		t.solving[host] = m
	}
	return m
}

// hasStored reports whether a cookie with the given name has been stored for the host.
func (t *Transport) hasStored(host, name string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.cookies[host] {
		if c.name == name {
			return true
		}
	}
	return false
}

// attachCookies attaches cookies for the host. Cookies with the same name are
// overridden by the most recently stored value.
func (t *Transport) attachCookies(req *http.Request) {
	t.mu.Lock()
	cs := t.cookies[req.URL.Host]
	t.mu.Unlock()
	now := time.Now()
	set := make(map[string]string)
	for i := range cs {
		c := cs[i]
		if !c.exp.IsZero() && now.After(c.exp) {
			continue
		}
		if c.path != "" && !pathMatches(c.path, req.URL.Path) {
			continue
		}
		set[c.name] = c.value
	}
	if len(set) == 0 {
		return
	}
	parts := make([]string, 0, len(set))
	for name, value := range set {
		parts = append(parts, name+"="+value)
	}
	req.Header.Set("Cookie", strings.Join(parts, "; "))
}

// captureCookies stores the response's Set-Cookie headers per host.
func (t *Transport) captureCookies(resp *http.Response) {
	if resp == nil {
		return
	}
	host := ""
	if resp.Request != nil {
		host = resp.Request.URL.Host
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cs := t.cookies[host]
	for _, c := range resp.Cookies() {
		cs = append(cs, &httpCookie{name: c.Name, value: c.Value, path: c.Path, exp: c.Expires})
	}
	t.cookies[host] = cs
}

// pathMatches reports whether the request path matches the cookie's Path.
func pathMatches(pattern, path string) bool {
	if pattern == "" {
		return true
	}
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern)
	}
	return path == pattern || strings.HasPrefix(path, pattern+"/")
}

// CloseIdleConnections closes idle connections. Compatible with http.DefaultTransport.
func (t *Transport) CloseIdleConnections() {
	if c, ok := t.base.(*http.Transport); ok {
		c.CloseIdleConnections()
	}
}
