package goodconnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// solveWithPlaywright drives the challenge (including V2/Managed) through
// go-playwright's Chrome — launched headed, since headless is detected by
// V2/Managed — and stores the issued cookies (cf_clearance etc.) in the Transport.
// The harvested cookies are reused by the plain HTTP (RoundTripper) side.
func (t *Transport) solveWithPlaywright(ctx context.Context, pageURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.debug != nil {
		t.debug.Printf("playwright: launching chromium (headed; headless is detected by V2/Managed)")
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright.Run: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		// Headless Chrome is detected by Turnstile (V2/Managed) and never clears,
		// so launch in normal (headed) mode; once the challenge is cleared the
		// cookies are harvested and later requests to the same host use plain HTTP.
		Headless: playwright.Bool(false),
		Args:     []string{"--disable-blink-features=AutomationControlled"},
	})
	if err != nil {
		// If the browser is not installed, install it with:
		//   go run github.com/playwright-community/playwright-go/cmd/playwright@latest install chromium
		return fmt.Errorf("playwright.Launch: %w", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return fmt.Errorf("playwright.NewPage: %w", err)
	}
	page.SetDefaultTimeout(60000)

	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		if t.debug != nil {
			t.debug.Printf("playwright.Goto: %v", err)
		}
	}

	// Wait for the challenge to clear (JS execution → automatic verification →
	// cookie issuance) by polling.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		cs, err := page.Context().Cookies()
		if err != nil {
			return fmt.Errorf("playwright.Cookies: %w", err)
		}
		title, terr := page.Title()
		if terr != nil {
			title = ""
		}
		u := page.URL()
		hasClearance := hasCookieNamed(cs, "cf_clearance")
		if hasClearance && !isChallengeTitle(title) {
			host := reqHost(pageURL)
			for _, c := range cs {
				t.storeOne(host, c.Name, c.Value, c.Path)
				if t.debug != nil {
					t.debug.Printf("playwright: cookie %s=%s", c.Name, c.Value)
				}
			}
			if t.debug != nil {
				t.debug.Printf("playwright: challenge cleared (title=%q url=%q)", title, u)
			}
			return nil
		}
		if t.debug != nil {
			t.debug.Printf("playwright: waiting for challenge... (title=%q url=%q)", title, u)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("playwright: challenge not cleared within 90s")
}

// hasCookieNamed reports whether the cookie list contains the given name.
func hasCookieNamed(cs []playwright.Cookie, name string) bool {
	for _, c := range cs {
		if c.Name == name {
			return true
		}
	}
	return false
}

// isChallengeTitle reports whether the page title is a Cloudflare challenge/error page.
func isChallengeTitle(title string) bool {
	l := strings.ToLower(title)
	return strings.Contains(l, "just a moment") ||
		strings.Contains(l, "attention required") ||
		strings.Contains(l, "checking your browser") ||
		strings.Contains(l, "cf-challenge")
}

// reqHost is a small helper that extracts the "//host" part.
func reqHost(u string) string {
	for i := 0; i+3 <= len(u); i++ {
		if u[i] == ':' && i+3 <= len(u) && u[i+1] == '/' && u[i+2] == '/' {
			s := u[i+2:]
			for j := i + 2; j < len(s); j++ {
				if s[j] == '/' {
					return s[:j]
				}
			}
			return s
		}
	}
	if len(u) >= 2 && u[0] == '/' && u[1] == '/' {
		s := u[2:]
		for j := 0; j < len(s); j++ {
			if s[j] == '/' {
				return s[:j]
			}
		}
		return s
	}
	return u
}

// storeOne stores a single cookie for the host.
func (t *Transport) storeOne(host, name, value, path string) {
	if path == "" {
		path = "/"
	}
	hc := &httpCookie{name: name, value: value, path: path}
	t.mu.Lock()
	t.cookies[host] = append(t.cookies[host], hc)
	t.mu.Unlock()
}
