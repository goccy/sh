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
