package syntax

import (
	"strings"
	"testing"
)

// TestRecursionLimit asserts that deeply nested input fails with a parse error
// rather than overflowing the goroutine stack (a fatal, unrecoverable throw).
func TestRecursionLimit(t *testing.T) {
	// Each nesting family must be bounded, not crash the process.
	for _, tc := range []struct{ name, open, close string }{
		{"arithmetic", "((", "))"},
		{"subshell", "( ", ") "},
		{"cmdSubst", "$( ", ") "},
		{"dblQuoted", `"$( `, `) "`},
		{"paramExp", "${x:-", "}"},
		{"testClause", "[[ ( ", " ) ]]"},
	} {
		src := strings.Repeat(tc.open, 100000) + strings.Repeat(tc.close, 100000)
		if _, err := NewParser().Parse(strings.NewReader(src), ""); err == nil {
			t.Errorf("%s: expected a recursion-limit error for deeply nested input", tc.name)
		}
	}

	// The arithmetic sub-expression productions recurse into themselves without
	// passing back through arithmExpr, so each right-associative / unary chain must
	// be guarded independently (long chains, not bracket nesting).
	for _, tc := range []struct{ name, expr string }{
		{"arith-ternary", strings.Repeat("1?1:", 200000) + "1"},
		{"arith-unary", strings.Repeat("!", 200000) + "1"},
		{"arith-power", strings.Repeat("2**", 200000) + "2"},
		{"arith-assign", strings.Repeat("a=", 200000) + "1"},
	} {
		src := "$(( " + tc.expr + " ))"
		if _, err := NewParser().Parse(strings.NewReader(src), ""); err == nil {
			t.Errorf("%s: expected a recursion-limit error for a long arithmetic chain", tc.name)
		}
	}

	// time/coproc recurse via gotStmtPipe, not the guarded getStmt, so they carry
	// their own guard ("time time time … cmd").
	for _, tc := range []struct{ name, src string }{
		{"time", strings.Repeat("time ", 100000) + "true"},
		{"coproc", strings.Repeat("coproc ", 100000) + "true"},
	} {
		if _, err := NewParser().Parse(strings.NewReader(tc.src), ""); err == nil {
			t.Errorf("%s: expected a recursion-limit error", tc.name)
		}
	}

	// Left-associative statement chains (&&/||/|) are built in a loop, so each
	// link must count against the guard; otherwise the deep BinaryCmd tree they
	// build overflows a depth-unguarded walker (e.g. the printer via declare -f).
	for _, tc := range []struct{ name, src string }{
		{"andor", "a" + strings.Repeat("&&a", 200000)},
		{"or", "a" + strings.Repeat("||a", 200000)},
		{"pipe", "a" + strings.Repeat("|a", 200000)},
		// elif clauses are also loop-built into a nested .Else chain that the
		// printer (declare -f) and Walk descend recursively without a guard.
		{"elif", "if a; then b; " + strings.Repeat("elif a; then b; ", 200000) + "fi"},
		{"elif-else", "if a; then b; " + strings.Repeat("elif a; then b; ", 200000) + "else c; fi"},
	} {
		if _, err := NewParser().Parse(strings.NewReader(tc.src), ""); err == nil {
			t.Errorf("%s: expected a recursion-limit error for a long command chain", tc.name)
		}
	}

	// Zsh nested parameter expansions ${${…}} recurse paramExp -> paramExp
	// without passing back through the guarded wordPart, so paramExp guards itself.
	zsh := NewParser(Variant(LangZsh))
	zsrc := strings.Repeat("${", 200000) + "x" + strings.Repeat("}", 200000)
	if _, err := zsh.Parse(strings.NewReader(zsrc), ""); err == nil {
		t.Error("zsh nested paramExp: expected a recursion-limit error")
	}
	if _, err := zsh.Parse(strings.NewReader("echo ${${x}}"), ""); err != nil {
		t.Errorf("shallow zsh nested paramExp should parse: %v", err)
	}

	// Shallow input parses; a custom lower limit rejects it.
	shallow := strings.Repeat("$(", 50) + "echo hi" + strings.Repeat(")", 50)
	if _, err := NewParser().Parse(strings.NewReader(shallow), ""); err != nil {
		t.Fatalf("shallow input should parse: %v", err)
	}
	if _, err := NewParser(RecursionLimit(10)).Parse(strings.NewReader(shallow), ""); err == nil {
		t.Fatal("RecursionLimit(10) should reject 50-deep nesting")
	}
	// A non-positive limit falls back to the default.
	p := NewParser(RecursionLimit(0))
	if p.recDepthMax != DefaultRecursionLimit {
		t.Fatalf("RecursionLimit(0) = %d, want default %d", p.recDepthMax, DefaultRecursionLimit)
	}
}
