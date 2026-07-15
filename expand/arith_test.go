package expand

import (
	"testing"

	"github.com/goccy/sh/v3/syntax"
)

// TestArithmDepthLimit asserts that the arithmetic evaluator bounds its own
// recursion depth, so a hand-built (parser-bypassing) deep AST fails with an
// error rather than overflowing the goroutine stack with an uncatchable throw.
func TestArithmDepthLimit(t *testing.T) {
	lit := func(s string) *syntax.Word {
		return &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: s}}}
	}

	// Build a left-leaning chain 1+1+1+…+1 deeper than the evaluator's bound.
	var expr syntax.ArithmExpr = lit("1")
	for range maxArithmDepth + 2 {
		expr = &syntax.BinaryArithm{Op: syntax.Add, X: expr, Y: lit("1")}
	}
	cfg := &Config{Env: ListEnviron()}
	if _, err := Arithm(cfg, expr); err == nil {
		t.Fatal("expected an arithmetic recursion-depth error on a deep AST")
	}

	// A shallow expression still evaluates correctly.
	shallow := &syntax.BinaryArithm{Op: syntax.Add, X: lit("2"), Y: lit("3")}
	if got, err := Arithm(cfg, shallow); err != nil || got != 5 {
		t.Fatalf("shallow arithmetic: got %d err %v, want 5", got, err)
	}
}
