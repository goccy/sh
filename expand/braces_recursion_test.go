package expand

import (
	"strings"
	"testing"

	"github.com/goccy/sh/v3/syntax"
)

// TestBracesDepthLimit asserts that deeply nested brace expansion fails with an
// error rather than overflowing the goroutine stack. Braces are a flat literal
// at parse time (SplitBraces builds the BraceExp tree iteratively), so the
// parser's recursion limit does not bound them — bracesSeqRec must bound itself.
// Left-nested input defeats the element-count limit because it descends fully
// before yielding any word.
func TestBracesDepthLimit(t *testing.T) {
	const n = 100000
	for _, tc := range []struct{ name, src string }{
		{"left-nested", "x" + strings.Repeat("{", n) + "a,b" + strings.Repeat(",z}", n)},
		{"right-nested", "x" + strings.Repeat("{a,", n) + "b" + strings.Repeat("}", n)},
	} {
		var w *syntax.Word
		if err := syntax.NewParser().Words(strings.NewReader(tc.src), func(word *syntax.Word) bool {
			w = word
			return false
		}); err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if w == nil {
			t.Fatalf("%s: no word parsed", tc.name)
		}
		syntax.SplitBraces(w)
		var gotErr error
		for _, err := range BracesSeq(nil, w) {
			if err != nil {
				gotErr = err
				break
			}
		}
		if gotErr == nil {
			t.Errorf("%s: expected a brace-expansion depth error", tc.name)
		}
	}
}

// TestBracesPaddingLimit asserts that an excessive numeric-sequence zero-padding
// width errors before allocating (an amplification: a small input with many
// leading zeros would otherwise repeat the padding across every element and
// exhaust host memory), while ordinary padding still expands.
func TestBracesPaddingLimit(t *testing.T) {
	parse := func(src string) *syntax.Word {
		var w *syntax.Word
		if err := syntax.NewParser().Words(strings.NewReader(src), func(word *syntax.Word) bool {
			w = word
			return false
		}); err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if w != nil {
			syntax.SplitBraces(w)
		}
		return w
	}

	var gotErr error
	for _, err := range BracesSeq(nil, parse("x{"+strings.Repeat("0", 500000)+"1..9}")) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected an error for excessive zero-padding")
	}

	n := 0
	for _, err := range BracesSeq(nil, parse("x{001..005}")) {
		if err != nil {
			t.Fatalf("ordinary padding: %v", err)
		}
		n++
	}
	if n != 5 {
		t.Errorf("padded sequence yielded %d elements, want 5", n)
	}
}
