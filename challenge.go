// This file is a Go port of the Cloudflare v1 (IUAM) JavaScript challenge
// detection and solving logic from cloudflare.py + interpreters/native.py of
// cloudscraper (https://github.com/VeNoMouS/cloudscraper, MIT License).
// See README.md / LICENSE for background and license details.
package goodconnect

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The regular expressions and string operations faithfully follow the structure
// of the original native.py.

const maxChallengeBody = 4 << 20 // maximum body size read for challenge detection

var (
	errNotChallenge = errors.New("not a cloudflare challenge")

	// Extracts the whole challenge (variable, initial value, operation chain);
	// the regex from native.py's challengeSolve.
	challengeRe = regexp.MustCompile(`setTimeout\(function\(\)\{\s+var.*?f,\s*(?P<variable>\w+).*?:(?P<init>\S+)};.*?\('challenge-form'\);.*?;(?P<challenge>.*?a\.value)\s*=\s*\S+\.toFixed\(10\);`)
	// The variable's operation chain: <var>...<op>=(<expr>);
	divisorExprRe = regexp.MustCompile(`^(.*?)(.)\(function`)
)

// isCFChallengeBody reports whether the response is a Cloudflare challenge page.
// v1 (IUAM): challenge-form + __cf_chl_f_tk
// v2/Managed: cdn-cgi/challenge-platform (orchestrate JS) or the cf_chl_opt config variable
func isCFChallengeBody(status int, h http.Header, body string) bool {
	if !strings.HasPrefix(h.Get("Server"), "cloudflare") {
		return false
	}
	if status != 403 && status != 429 && status != 503 {
		return false
	}
	v1 := strings.Contains(body, "challenge-form") && strings.Contains(body, "__cf_chl_f_tk")
	v2 := strings.Contains(body, "cdn-cgi/challenge-platform")
	v2opt := strings.Contains(body, "cf_chl_opt") && strings.Contains(body, "cdn-cgi")
	return (v1 || v2 || v2opt) && strings.Contains(body, "cdn-cgi")
}

// challengeDelay extracts the wait time (milliseconds) the challenge page requires.
// Equivalent to cloudscraper's delay extraction (submit(); }, <ms>).
func challengeDelay(body string) (ms int, ok bool) {
	re := regexp.MustCompile(`submit\(\);[\r\n]+\s*},\s*([0-9]+)`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// SolveJschl computes jschl_answer from the challenge body. A port of native.py's challengeSolve.
func SolveJschl(body, domain string) (string, error) {
	m := challengeRe.FindStringSubmatch(body)
	if m == nil {
		return "", errNotChallenge
	}
	var variable, initStr, challenge string
	names := challengeRe.SubexpNames()
	for i, name := range names {
		if i == 0 {
			continue
		}
		switch name {
		case "variable":
			variable = m[i]
		case "init":
			initStr = m[i]
		case "challenge":
			challenge = m[i]
		}
	}
	if variable == "" || strings.TrimSpace(challenge) == "" {
		return "", errNotChallenge
	}

	// k management (kJSFUCK / kID / kValues in native.py).
	kJSFUCKRe := regexp.MustCompile(`(;|)\s*k.=(?P<kJSFUCK>\S+);`)
	kStr := ""
	if km := kJSFUCKRe.FindStringSubmatch(challenge); km != nil {
		kIdx, err := jsfuckToInt(km[3])
		if err == nil {
			kIDRe := regexp.MustCompile(`\s*k\s*=\s*'(?P<kID>\S+)';`)
			if kidm := kIDRe.FindStringSubmatch(body); kidm != nil {
				kID := kidm[3]
				divRe := regexp.MustCompile(`<div id="` + regexp.QuoteMeta(kID) + `(?P<id>\d+)">\s*(?P<jsfuck>[^<>]*)</div>`)
				kValues := make(map[int]string)
				for _, dm := range divRe.FindAllStringSubmatch(body, -1) {
					idn, err := strconv.Atoi(dm[4])
					if err == nil {
						kValues[idn] = dm[5]
					}
				}
				if v, ok := kValues[int(kIdx)]; ok {
					kStr = v
				}
			}
		}
	}

	// Initial value (native.py: division if init contains '/').
	var answer float64
	if strings.Contains(initStr, "/") {
		parts := strings.Split(initStr, "/")
		if len(parts) != 2 {
			return "", errors.New("SolveJschl: invalid init")
		}
		a, err := jsfuckToFloat(parts[0])
		if err != nil {
			return "", err
		}
		b, err := jsfuckToFloat(strings.TrimSuffix(parts[1], `}`))
		if err != nil {
			return "", err
		}
		answer = a / b
	} else {
		v, err := jsfuckToFloat(initStr)
		if err != nil {
			return "", err
		}
		answer = v
	}

	// Apply the operation chain <var>...<op>=(<expr>); in order (the challenge loop in native.py).
	opsRe := regexp.MustCompile(regexp.QuoteMeta(variable) + `.*?([+\-*/])=(.*?);`)
	for _, mm := range opsRe.FindAllStringSubmatch(challenge, -1) {
		oper := mm[1]
		expr := mm[2]
		var exprVal float64
		var err error
		if strings.Contains(expr, "/") {
			exprVal, err = divisorMath(expr, "function(p)", domain)
		} else if strings.Contains(expr, "Element") {
			exprVal, err = divisorMath(kStr, `"("+p+")")}`, domain)
		} else {
			exprVal, err = jsfuckToFloat(expr)
		}
		if err != nil {
			return "", err
		}
		switch oper {
		case "+":
			answer += exprVal
		case "-":
			answer -= exprVal
		case "*":
			answer *= exprVal
		case "/":
			answer /= exprVal
		}
	}

	return fmt.Sprintf("%.10f", answer), nil
}

// divisorMath is a port of native.py's divisorMath.
func divisorMath(payload, needle, domain string) (float64, error) {
	parts := strings.Split(payload, "/")
	if len(parts) != 2 {
		return 0, errors.New("divisorMath: no '/'")
	}
	var expressionValue float64
	if strings.Contains(parts[1], needle) {
		em := divisorExprRe.FindStringSubmatch(parts[1])
		if em == nil {
			return 0, errors.New("divisorMath: no function(p)")
		}
		left, err := jsfuckToFloat(em[1])
		if err != nil {
			return 0, err
		}
		pos := strings.Index(parts[1], needle) + len(needle)
		if pos >= len(parts[1]) || pos > len(parts[1])-2 {
			return 0, errors.New("divisorMath: bad index slice")
		}
		idxStr := parts[1][pos : len(parts[1])-2]
		idx, err := jsfuckToInt(idxStr)
		if err != nil {
			return 0, err
		}
		if idx < 0 || idx >= int64(len(domain)) {
			return 0, fmt.Errorf("divisorMath: index out of range: %d", idx)
		}
		expressionValue = left / float64(domain[idx])
	} else {
		v, err := jsfuckToFloat(parts[1])
		if err != nil {
			return 0, err
		}
		expressionValue = v
	}
	top, err := jsfuckToFloat(parts[0])
	if err != nil {
		return 0, err
	}
	if expressionValue == 0 {
		return 0, errors.New("divisorMath: division by zero")
	}
	return top / expressionValue, nil
}

// challengeForm extracts the challenge page form (action / hidden fields).
// Equivalent to cloudscraper's IUAM_Challenge_Response.
func challengeForm(body string, target *url.URL) (action string, fields map[string]string, err error) {
	formRe := regexp.MustCompile(`<form (?P<form>.*?="challenge-form" action="(?P<challengeUUID>.*?__cf_chl_f_tk=\S+)"(.*?)</form>)`)
	m := formRe.FindStringSubmatch(body)
	if m == nil {
		return "", nil, errNotChallenge
	}
	names := formRe.SubexpNames()
	form, challengeUUID := "", ""
	for i, name := range names {
		switch name {
		case "form":
			form = m[i]
		case "challengeUUID":
			challengeUUID = m[i]
		}
	}
	if form == "" {
		return "", nil, errNotChallenge
	}
	fields = make(map[string]string)
	inputRe := regexp.MustCompile(`(?m)^\s*<input\s(.*?)/>`)
	for _, im := range inputRe.FindAllStringSubmatch(form, -1) {
		for _, kv := range kvRe.FindAllStringSubmatch(im[1], -1) {
			if len(kv) == 3 && (kv[1] == "r" || kv[1] == "jschl_vc" || kv[1] == "pass") {
				fields[kv[1]] = kv[2]
			}
		}
	}
	action = html.UnescapeString(challengeUUID)
	if action == "" {
		return "", nil, errNotChallenge
	}
	u, err := url.Parse(action)
	if err != nil {
		return "", nil, err
	}
	if !u.IsAbs() {
		u = target.ResolveReference(u)
	}
	return u.String(), fields, nil
}

var kvRe = regexp.MustCompile(`(\S+)="(\S+)"`)
