# good-connect — Cloudflare access-block bypass wrapper (Go)

A Go port of the design of [cloudscraper](https://github.com/VeNoMouS/cloudscraper),
implemented as a wrapper around Go's standard **`net/http` `http.RoundTripper`**.
Swap it into code that already uses `http.Client` / `http.Transport` and it handles
Cloudflare's "403 Just a moment..." challenge for you:

- **Browser emulation** (Chrome-like User-Agent, `Sec-Fetch-*` and other headers)
- **Automatic solving of Cloudflare v1 (IUAM / jschl) challenges** (pure Go, no JS engine required)
- **V2 / Managed challenges are passed through a real browser**: go-playwright + headed Chrome
  (`Options.EnablePlaywright`) or the obscura headless browser (`Options.ObscuraPath`)
- **Per-host rate limiting**: default `MaxQPS: 1` = at most **one request per second per domain**
- Solved challenges are kept as cookies (`cf_clearance` etc.) and attached to subsequent requests automatically

## Usage

```bash
cd good-connect
go test ./...
go run ./cmd/good-connect -debug https://example.com
```

To pass V2 / Managed challenges that the v1 solver cannot solve through a real browser:

```bash
# A) Headed Chrome (go-playwright)
go run ./cmd/good-connect -playwright -debug https://example.com

# B) Headless (obscura headless browser)
go run ./cmd/good-connect -obscura -debug https://example.com
```

- **Option A (headed Chrome)**: less likely to be detected by Turnstile (V2/Managed); it usually
  clears in about 5 seconds. Headless Chrome is detected by Turnstile and never clears,
  so headed mode is recommended.
- **Option B (obscura headless)**: launches [h4ckf0r0day/obscura](https://github.com/h4ckf0r0day/obscura)
  (a Rust headless browser with stealth mode) without a display. The executable is auto-detected
  via the `OBSCURA_EXECUTABLE` environment variable or `%LOCALAPPDATA%\obscura\obscura.exe`
  (exits with status 2 if not installed).
  Current obscura releases cannot render the Turnstile challenge iframe
  ([issue #844](https://github.com/h4ckf0r0day/obscura/issues/844)), so the page title may stay
  "Just a moment...", but `cf_clearance` is still issued; the cookie is harvested as soon as it
  appears and is re-sent over plain HTTP on subsequent requests
  (measured: about 1–2 minutes to pass).

Once `cf_clearance` is issued, it is harvested and later requests to the same host are sent
over plain HTTP.

(Option A requires installing the browser first:
`go run github.com/playwright-community/playwright-go/cmd/playwright@latest install chromium`)

Fetching three pages in a row (**all spaced 1 second apart**):

```bash
go run ./cmd/good-connect \
  https://example.com/ \
  https://example.com/1.html \
  https://example.org/
```

Using it as a library:

```go
import "good-connect"

client := goodconnect.NewClient(goodconnect.Options{
    MaxQPS: 1, // during development: at most 1 request per second (per host)
})
resp, err := client.Get("https://example.com/")
```

Swapping it in at the `http.Transport` level:

```go
tr := goodconnect.NewTransport(goodconnect.Options{MaxQPS: 1})
client := &http.Client{Transport: tr, Jar: jar}
```

## Layout (mapping to cloudscraper)

| File | Role | Corresponds to cloudscraper |
|---|---|---|
| `transport.go` | `http.RoundTripper` wrapper (headers → 1 req/s limit → challenge detection → solve → resend → redirects) | `cloudscraper/cloudflare.py` (`CloudflareAdapter.__cfscrape`) |
| `challenge.go` | Challenge detection, `jschl_answer` computation, form extraction, answer submission | `cloudscraper/cloudscraper/cloudflare.py` |
| `jsfuck.go` | JSFuck → number conversion, arithmetic evaluation | `cloudscraper/interpreters/native.py` |
| `fixparens.go` | Parenthesis balancing, depth-0 splitting | `native.py` (`Parentheses`) |
| `ratelimit.go` | Per-host token bucket (default 1 QPS) | cloudscraper's `delay`/stealth equivalent |
| `headers.go` | Chrome-like headers | `browser='chrome'` equivalent |
| `playwright.go` | Pass v1/v2 challenges through a real browser (go-playwright/Chrome) and harvest `cf_clearance` | Not covered by cloudscraper (real-browser approach) |
| `obscura.go` | Launch the headless browser (obscura, CDP) to pass v2 challenges and harvest `cf_clearance` | Not covered by cloudscraper (headless approach) |
| `cmd/good-connect/` | Smoke-testing CLI | `examples/` |

Dependency: [playwright-community/playwright-go](https://github.com/playwright-community/playwright-go)
(the successor module of `go-playwright`; the old `github.com/playwright-community/go-playwright` was renamed).

## Rate limiting (default: at most 1 request per second)

Controlled by `Options.MaxQPS` (per host, default 1) and `Options.Burst` (default 1).
It applies to **every request to every domain** — including challenge answers and redirect
following — through both `http.Client` and `http.Transport`, so during development requests to
a given domain are always spaced at least 1 second apart (also changeable via the `-qps` flag).

Concurrency safety: token shortfalls are kept in the bucket as "debt" (the same approach as
x/time/rate), so concurrent requests from multiple goroutines are **not fired all at once** —
each host is paced 1/qps seconds apart. Buckets are independent per host, so requests to
different hosts never block each other (verified in `ratelimit_test.go`). Challenge solving is
serialized per host, so even under concurrent load at most one real browser
(playwright / obscura) is launched at a time.

## Notes and limitations

- v1 (jschl) challenges are solved in pure Go. For **V2 / Managed challenges** that v1 cannot
  solve, set `Options.EnablePlaywright: true` (CLI: `-playwright`, headed Chrome) or
  `Options.ObscuraPath` (CLI: `-obscura`, headless) to pass them through a real browser and
  reuse the issued `cf_clearance` for plain requests (polls for up to 90 seconds. obscura
  harvests the cookie the moment the browser stores it internally, so the Turnstile rendering
  limitation ([issue #844](https://github.com/h4ckf0r0day/obscura/issues/844)) does not affect it).
- Challenge detection uses `Server: cloudflare` + 403/429/503 + challenge markers
  (v1: `challenge-form` + `__cf_chl_f_tk`; v2: `cdn-cgi/challenge-platform` / `cf_chl_opt`).
- Please review the target site's terms of service and avoid overloading it.
- If a challenge cannot be solved, the 403 response is returned as-is.

## License

MIT License ([LICENSE](LICENSE)). This project includes a port inspired by
[cloudscraper](https://github.com/VeNoMouS/cloudscraper) (MIT License,
copyright (c) 2025 Zied Boughdir / Copyright (c) 2019 VeNoMouS / Copyright (c) 2015 Anorov).
cloudscraper's license text is reproduced in LICENSE as well.
