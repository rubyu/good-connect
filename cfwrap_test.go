package goodconnect

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJsfuckSimple(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"(+1+1)", 2},
		{"(1+1)", 2},
		{"(+1+1+[1])", 3},
		{"+((1+1+1+1+1+1+1+1+1+0)+(+1+[1]))", 11},
		{"(+1+1+1+1+1+1+0)", 6},
		{"(-1+1+1+1+1)", 3},
		{"(4*2)+(3-1)+[5]", 825},
	}
	for _, c := range cases {
		got, err := jsfuckToInt(c.in)
		if err != nil {
			t.Fatalf("jsfuckToInt(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("jsfuckToInt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEvalArith(t *testing.T) {
	cases := map[string]float64{
		"1+2*3":     7,
		"(1+2)*3":   9,
		"[1+2]*3+1": 10,
		"1-1+[4]/2": 2,
		"-1+5*2":    9,
	}
	for expr, want := range cases {
		got, err := evalArith(expr)
		if err != nil {
			t.Fatalf("evalArith(%q): %v", expr, err)
		}
		if got != want {
			t.Errorf("evalArith(%q) = %v, want %v", expr, got, want)
		}
	}
}

func TestJsfuckRejectsJunk(t *testing.T) {
	if _, err := jsfuckToInt("abc"); err == nil {
		t.Fatal("expected error for junk input")
	}
}

func TestFixParensStable(t *testing.T) {
	for _, s := range []string{"(1+1)", "((1+1)+(2+2))", "1+1"} {
		if got := fixParens(s); got != s {
			t.Errorf("fixParens(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestSolveJschlRejectsNormalBody(t *testing.T) {
	if _, err := SolveJschl("<html><body>hello</body></html>", "example.com"); err == nil {
		t.Fatal("expected error for non-challenge body")
	}
}

func TestSplitTop(t *testing.T) {
	parts := splitTop("(1+1)+(2+2)", '+')
	if len(parts) != 2 || parts[0] != "(1+1)" || parts[1] != "(2+2)" {
		t.Fatalf("splitTop = %#v", parts)
	}
	if got := strings.Join(parts, "|"); got != "(1+1)|(2+2)" {
		t.Fatalf("splitTop joined = %q", got)
	}
}

func TestRateLimitOnePerSecond(t *testing.T) {
	l := newHostLimiter(1, 1)
	ctx := context.Background()
	start := time.Now()
	_ = l.wait(ctx, "syosetu.org")
	t1 := time.Now()
	_ = l.wait(ctx, "syosetu.org")
	t2 := time.Now()
	_ = l.wait(ctx, "syosetu.org")
	t3 := time.Now()
	if t3.Sub(t1) < 900*time.Millisecond {
		t.Fatalf("expected >= 1s between requests, got %s", t3.Sub(t1))
	}
	_ = t2
	_ = start
}
