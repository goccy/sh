package interp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goccy/sh/v3/interp"
	"github.com/goccy/sh/v3/syntax"
)

// TestRecursionLimit asserts that unbounded runtime recursion (a self-calling
// function or a self-eval, which re-enter the interpreter without going through
// the parser) fails with a fatal error rather than overflowing the goroutine
// stack.
func TestRecursionLimit(t *testing.T) {
	run := func(src string, opts ...interp.RunnerOption) error {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return err
		}
		r, err := interp.New(append([]interp.RunnerOption{interp.StdIO(nil, nil, nil)}, opts...)...)
		if err != nil {
			return err
		}
		return r.Run(context.Background(), f)
	}

	for _, tc := range []struct{ name, src string }{
		{"self-call", "f(){ f; }; f"},
		{"mutual", "a(){ b; }; b(){ a; }; a"},
		{"self-eval", `x='eval "$x"'; eval "$x"`},
	} {
		if err := run(tc.src); err == nil {
			t.Errorf("%s: expected a recursion-limit error", tc.name)
		}
	}

	// A shallow program runs; a low custom limit trips on modest recursion.
	if err := run("echo hi; for i in 1 2 3; do echo $i; done"); err != nil {
		t.Fatalf("shallow program should run: %v", err)
	}
	if err := run("g(){ g; }; g", interp.RecursionLimit(50)); err == nil {
		t.Fatal("RecursionLimit(50) should trip on unbounded recursion")
	}
}
