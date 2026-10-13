package goodconnect

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/playwright-community/playwright-go"
)

// LookupObscura returns the path of the obscura executable (Rust headless
// browser). The OBSCURA_EXECUTABLE environment variable takes precedence; on
// Windows, %LOCALAPPDATA%\obscura\obscura.exe is checked next. Returns "" if
// not found.
//
// Download (Windows x86_64, stealth build):
//
//	curl -LO https://github.com/h4ckf0r0day/obscura/releases/latest/download/obscura-x86_64-windows-stealth.zip
func LookupObscura() string {
	if p := os.Getenv("OBSCURA_EXECUTABLE"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		if a := os.Getenv("LOCALAPPDATA"); a != "" {
			p := filepath.Join(a, "obscura", "obscura.exe")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

// solveWithObscura opens the page in a browser launched with obscura (headless
// stealth mode) and stores the cookies issued after the challenge (including
// V2/Managed; cf_clearance etc.) in the Transport. The harvested cookies are
// reused by the plain HTTP (RoundTripper) side.
//
// The obscura service is started as a CDP server (obscura serve) and driven
// through playwright's ConnectOverCDP.
func (t *Transport) solveWithObscura(ctx context.Context, obscuraPath, pageURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.debug != nil {
		t.debug.Printf("obscura: launching %s (headless stealth)", obscuraPath)
	}

	port := freePort()
	cmd := exec.Command(obscuraPath, "serve", "--port", strconv.Itoa(port), "--stealth")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("obscura: start: %w", err)
	}
	defer killTree(cmd)

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("obscura: playwright.Run: %w", err)
	}
	defer pw.Stop()

	// Wait for the CDP server to become ready.
	var browser playwright.Browser
	ready := time.Now().Add(20 * time.Second)
	for time.Now().Before(ready) {
		b, cerr := pw.Chromium.ConnectOverCDP("http://127.0.0.1:" + strconv.Itoa(port))
		if cerr == nil {
			browser = b
			break
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if browser == nil {
		return fmt.Errorf("obscura: CDP server did not become ready on port %d", port)
	}
	defer browser.Close()

	pageCtx, err := browser.NewContext()
	if err != nil {
		return fmt.Errorf("obscura: NewContext: %w", err)
	}
	page, err := pageCtx.NewPage()
	if err != nil {
		return fmt.Errorf("obscura: NewPage: %w", err)
	}
	page.SetDefaultTimeout(60000)

	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil && t.debug != nil {
		t.debug.Printf("obscura: Goto: %v", err)
	}

	// Wait for the challenge to clear (JS execution → automatic verification →
	// cookie issuance) by polling. For Turnstile challenges the page title may
	// stay "Just a moment..." because obscura cannot render the iframe (known
	// issue #844), so we save the cookies as soon as cf_clearance is issued and
	// let the HTTP retry handle the rest. A freshly issued cf_clearance is
	// sometimes re-challenged once when reused immediately; the next solve then
	// finishes quickly and the following request passes.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		cs, cerr := pageCtx.Cookies(pageURL)
		if cerr != nil {
			return fmt.Errorf("obscura: Cookies: %w", cerr)
		}
		title, terr := page.Title()
		if terr != nil {
			title = ""
		}
		u := page.URL()
		if hasCookieNamed(cs, "cf_clearance") {
			host := reqHost(pageURL)
			for _, c := range cs {
				t.storeOne(host, c.Name, c.Value, c.Path)
				if t.debug != nil {
					t.debug.Printf("obscura: cookie %s=%s", c.Name, c.Value)
				}
			}
			if t.debug != nil {
				t.debug.Printf("obscura: cf_clearance issued (title=%q url=%q)", title, u)
			}
			return nil
		}
		if t.debug != nil {
			t.debug.Printf("obscura: waiting for challenge... (title=%q url=%q)", title, u)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("obscura: cf_clearance not issued within 90s")
}

// freePort returns a free local TCP port (the reservation is released before returning).
func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 19399
	}
	defer l.Close()
	if addr, ok := l.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 19399
}

// killTree terminates the process and its child processes.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
