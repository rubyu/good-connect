package goodconnect

import (
	"log"
	"net/http"
	"net/http/cookiejar"
)

// Options configures good-connect.
//
//   - MaxQPS:            requests per second per host. 0 means 1 (at most one per second).
//   - Burst:             token bucket capacity. 0 means 1, which effectively forces
//     consecutive accesses to be at least 1 second apart.
//   - Base:              RoundTripper used for the actual I/O. If nil, a clone of http.DefaultTransport.
//   - UserAgent:         custom User-Agent. Empty means a Chrome-like one.
//   - Headers:           extra headers; user-supplied values take precedence.
//   - MaxSolveAttempts:  maximum challenge solving attempts (default 3).
//   - EnablePlaywright:   pass V2/Managed challenges via go-playwright (headed Chrome).
//   - ObscuraPath:       path to the obscura (headless stealth browser) executable. Empty disables it.
//   - Debug:             debug logger (nil disables).
type Options struct {
	MaxQPS           float64
	Burst            float64
	Base             http.RoundTripper
	UserAgent        string
	Headers          http.Header
	MaxSolveAttempts int
	EnablePlaywright bool
	ObscuraPath      string
	Debug            *log.Logger
}

// NewClient returns an *http.Client that uses goodconnect.Transport. Used like
// http.DefaultClient; it has a cookie jar and follows redirects automatically.
func NewClient(o Options) *http.Client {
	j, _ := cookiejar.New(nil)
	return &http.Client{
		Transport: NewTransport(o),
		Jar:       j,
	}
}
