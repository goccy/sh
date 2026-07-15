package interp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/goccy/sh/v3/interp"
	"github.com/goccy/sh/v3/syntax"
)

// TestArrayIndexLimit asserts that an unbounded indexed-array subscript fails
// with an error rather than allocating a dense slice proportional to the index
// (an uncatchable OOM), while ordinary sparse and list arrays still work.
func TestArrayIndexLimit(t *testing.T) {
	run := func(src string) error {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return err
		}
		var out strings.Builder
		r, err := interp.New(interp.StdIO(nil, &out, &out), interp.MaxExpandBytes(4<<20))
		if err != nil {
			return err
		}
		return r.Run(context.Background(), f)
	}

	big := `big=$(printf '%01000000d' 0); `
	for _, tc := range []struct{ name, src string }{
		// index too large -> dense slice OOM.
		{"assign", "a[50000000]=x"},
		{"append", "a[50000000]+=x"},
		{"literal", "declare -a a=([50000000]=1)"},
		// cumulative bytes past the budget (each element is individually in-budget).
		{"total-bytes", big + `a=("$big" "$big" "$big" "$big" "$big")`},
		{"append-bytes", big + `for i in 1 2 3 4 5 6 7 8; do a+=("$big"); done`},
		{"index-bytes", big + `for i in 1 2 3 4 5 6 7 8; do a[$i]=$big; done`},
		{"assoc-bytes", big + `declare -A m; for k in a b c d e f g h; do m[$k]=$big; done`},
		// += must re-check the element-count cap the literal guard only saw locally.
		{"append-count", "c=(); for i in {1..1100}; do c+=($i); done; for j in {1..1100}; do a+=(\"${c[@]}\"); done"},
		// ramp past the budget landing on a non-power-of-two length: an amortized
		// power-of-two byte check would skip the scan here and miss the overshoot.
		{"ramp-append", big + `a=("$big" "$big"); a+=("$big" "$big" "$big")`},
		{"ramp-index", big + `a[0]=$big; a[1]=$big; a[2]=$big; a[3]=$big; a[4]=$big`},
	} {
		if err := run(tc.src); err == nil {
			t.Errorf("%s: expected an array-size error, got nil", tc.name)
		}
	}

	// Ordinary sparse, list, growing, and associative arrays are unaffected.
	if err := run("a[5]=hi; a[2]=yo; a=(one two three); b=(); for i in {1..2000}; do b+=($i); done; declare -A m; m[x]=1"); err != nil {
		t.Errorf("ordinary arrays should work: %v", err)
	}
}
