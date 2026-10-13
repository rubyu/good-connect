// This file is a Go port of interpreters/native.py (jsfuckToNumber / eval) from
// cloudscraper (https://github.com/VeNoMouS/cloudscraper, MIT License).
// See README.md / LICENSE for background and license details.
package goodconnect

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// jsfuckToFloat returns the numeric value of a JSFuck expression as float64.
// A port of native.py's jsfuckToNumber.
func jsfuckToFloat(s string) (float64, error) {
	s = strings.NewReplacer("!+[]", "1", "!![]", "1", "[]", "0").Replace(s)
	s = strings.TrimLeft(s, "+")
	s = strings.ReplaceAll(s, "(+", "(")
	s = strings.ReplaceAll(s, " ", "")
	s = fixParens(s)
	parts := splitTop(s, '+')
	var sb strings.Builder
	for _, part := range parts {
		v, err := evalArith(part)
		if err != nil {
			return 0, err
		}
		// Equivalent to native.py: int(''.join(str(Calc.doMath(part)))).
		if v == float64(int64(v)) {
			sb.WriteString(strconv.FormatInt(int64(v), 10))
		} else {
			sb.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	if sb.Len() == 0 {
		return 0, errors.New("jsfuck: empty expression")
	}
	v, err := strconv.ParseFloat(sb.String(), 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// jsfuckToInt returns the numeric value of a JSFuck expression as int64.
func jsfuckToInt(s string) (int64, error) {
	v, err := jsfuckToFloat(s)
	if err != nil {
		return 0, err
	}
	if v != float64(int64(v)) {
		return 0, fmt.Errorf("jsfuck: not an integer: %q", s)
	}
	return int64(v), nil
}

// arithParser is a recursive-descent parser for arithmetic expressions with
// + - * / and [ ] brackets. The equivalent of Calc(ast.NodeVisitor) in native.py.
type arithParser struct {
	s string
	i int
}

func (p *arithParser) skipSpaces() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

// evalArith evaluates an arithmetic expression (digits, the four operations,
// brackets, unary +-).
func evalArith(s string) (float64, error) {
	p := &arithParser{s: s}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.i < len(p.s) {
		return 0, fmt.Errorf("evalArith: trailing tokens in %q", s)
	}
	return v, nil
}

func (p *arithParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.i >= len(p.s) {
			return v, nil
		}
		switch p.s[p.i] {
		case '+':
			p.i++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v += r
		case '-':
			p.i++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v -= r
		default:
			return v, nil
		}
	}
}

func (p *arithParser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.i >= len(p.s) {
			return v, nil
		}
		switch p.s[p.i] {
		case '*':
			p.i++
			r, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			v *= r
		case '/':
			p.i++
			r, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, errors.New("evalArith: division by zero")
			}
			v /= r
		default:
			return v, nil
		}
	}
}

func (p *arithParser) parseFactor() (float64, error) {
	p.skipSpaces()
	if p.i >= len(p.s) {
		return 0, errors.New("evalArith: unexpected end")
	}
	switch c := p.s[p.i]; c {
	case '(':
		p.i++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
			return v, nil
		}
		// Treat JSFuck's [ ] like parentheses too (matching native.py).
		return 0, errors.New("evalArith: unbalanced '('")
	case '[':
		p.i++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return v, nil
		}
		return 0, errors.New("evalArith: unbalanced '['")
	case '+':
		p.i++
		return p.parseFactor()
	case '-':
		p.i++
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	default:
		start := p.i
		for p.i < len(p.s) && ((p.s[p.i] >= '0' && p.s[p.i] <= '9') || p.s[p.i] == '.') {
			p.i++
		}
		if start == p.i {
			return 0, fmt.Errorf("evalArith: unexpected char %q", p.s[p.i:])
		}
		return strconv.ParseFloat(p.s[start:p.i], 64)
	}
}
