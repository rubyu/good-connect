// good-connect is a CLI for smoke-testing the good-connect library (a Go port
// of cloudscraper).
//
// Usage:
//
//	go run ./cmd/good-connect https://example.com
//
// By default it accesses each host at most once per second (change with -qps).
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"good-connect"
)

func main() {
	qps := flag.Float64("qps", 1, "maximum requests per second (per host)")
	debug := flag.Bool("debug", false, "show debug logs")
	pw := flag.Bool("playwright", false, "pass V2/Managed challenges with go-playwright (headed Chrome)")
	obs := flag.Bool("obscura", false, "pass V2/Managed challenges with obscura (headless stealth); auto-detects OBSCURA_EXECUTABLE or %LOCALAPPDATA%\\obscura\\obscura.exe")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: good-connect [-qps N] [-playwright|-obscura] [-debug] URL...\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	urls := flag.Args()
	if len(urls) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	var dbg *log.Logger
	if *debug {
		dbg = log.New(os.Stderr, "[good-connect] ", log.LstdFlags)
	}

	client := goodconnect.NewClient(goodconnect.Options{
		MaxQPS:           *qps, // during development: at most 1 request per second
		EnablePlaywright: *pw,
		ObscuraPath:      obscuraPath(*obs),
		Debug:            dbg,
	})

	for _, u := range urls {
		if err := fetch(client, u); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR %s: %v\n", u, err)
		}
	}
}

// obscuraPath returns the executable path for the -obscura flag. Exits if it is missing.
func obscuraPath(enabled bool) string {
	if !enabled {
		return ""
	}
	p := goodconnect.LookupObscura()
	if p == "" {
		fmt.Fprintln(os.Stderr, "obscura not found: set OBSCURA_EXECUTABLE or download it from https://github.com/h4ckf0r0day/obscura")
		os.Exit(2)
	}
	return p
}

func fetch(client *http.Client, u string) error {
	start := time.Now()
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", u)
	fmt.Printf("  status : %s\n", resp.Status)
	fmt.Printf("  type   : %s\n", resp.Header.Get("Content-Type"))
	fmt.Printf("  bytes  : %d\n", len(body))
	fmt.Printf("  time   : %s\n", time.Since(start).Round(time.Millisecond))
	snippet := strings.TrimSpace(snippetFrom(string(body), 300))
	if snippet != "" {
		fmt.Printf("  body   : %s\n", snippet)
	}
	return nil
}

// snippetFrom collapses the first n bytes into a single line.
func snippetFrom(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	s = strings.ReplaceAll(s, "\n", " ")
	t := strings.TrimSpace(s)
	return t
}
