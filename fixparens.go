// This file is a Go port of interpreters/native.py (Parentheses) from
// cloudscraper (https://github.com/VeNoMouS/cloudscraper, MIT License).
// See README.md / LICENSE for background and license details.
package goodconnect

// invalidParens counts the number of mismatched parentheses.
// A port of Parentheses.invalid() in cloudscraper's native interpreter (native.py).
func invalidParens(s string) int {
	plus, minus := 0, 0
	for _, c := range s {
		if c == '(' {
			plus++
		} else if c == ')' {
			plus--
			if plus < 0 {
				minus++
				plus = 0
			}
		}
	}
	return plus + minus
}

// fixParens is a port of Parentheses().fix() in cloudscraper: it builds a list
// of balanced strings by removing the minimum number of parentheses and returns
// the first result. Balanced input is returned unchanged.
func fixParens(s string) string {
	res := []string{}
	visited := map[string]bool{s: true}
	var dfs func(cur string, n int)
	dfs = func(cur string, n int) {
		if n == 0 {
			res = append(res, cur)
			return
		}
		for i := 0; i < len(cur); i++ {
			if cur[i] == '(' || cur[i] == ')' {
				next := cur[:i] + cur[i+1:]
				if !visited[next] && invalidParens(next) < n {
					visited[next] = true
					dfs(next, invalidParens(next))
				}
			}
		}
	}
	dfs(s, invalidParens(s))
	if len(res) == 0 {
		return s
	}
	return res[0]
}

// splitTop splits s at occurrences of sep at bracket depth 0 (counting JSFuck [ ] too).
func splitTop(s string, sep byte) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}
