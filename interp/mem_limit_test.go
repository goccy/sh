package interp_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/goccy/sh/v3/interp"
	"github.com/goccy/sh/v3/syntax"
)

// TestExpandByteLimit asserts that an expansion which would amplify a small input
// into a huge allocation fails with an error rather than exhausting host memory.
func TestExpandByteLimit(t *testing.T) {
	// Small cap so the test stays fast and low-memory.
	const cap = 1 << 20
	run := func(src string) (string, error) {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return "", err
		}
		var stderr strings.Builder
		r, err := interp.New(
			interp.StdIO(nil, io.Discard, &stderr),
			interp.MaxExpandBytes(cap),
		)
		if err != nil {
			return "", err
		}
		runErr := r.Run(context.Background(), f)
		return stderr.String(), runErr
	}

	for _, tc := range []struct{ name, src string }{
		// printf with many wide specifiers builds one huge output string.
		{"printf", "printf '" + strings.Repeat("%1000000d", 100) + "'"},
		// each brace element concatenates a large value (16Ki × ~1 MiB).
		{"brace-bytes", `pre=$(printf "%0999999d" 1); echo ${pre}x{1..16384}`},
		// repeated self-concatenation doubles the value each step.
		{"doubling", "x=y; " + strings.Repeat("x=$x$x; ", 40) + "echo ${#x}"},
		// ${x//pat/repl} grows to len(str)*len(with).
		{"replace", `s=$(printf '%0500000d' 1); w=$(printf '%04096d' 1); echo ${s//?/$w}`},
		// a huge pattern compiles to a much larger regexp.
		{"pattern", `s=$(printf '%0999999d' 1); case x in $s) ;; *) ;; esac`},
		// value-transform operators allocate several times the value.
		{"transform-case", "x=y; " + strings.Repeat("x=$x$x; ", 17) + "echo ${x^^}"},
		{"transform-quote", "x=y; " + strings.Repeat("x=$x$x; ", 17) + "echo ${x@E}"},
		// a word of many parts concatenates to numParts*partSize (before the join).
		{"concat-shared", "x=aaaa; " + strings.Repeat("x=$x$x; ", 14) + "echo " + strings.Repeat("$x", 200)},
		{"concat-fresh", "x=aaaa; " + strings.Repeat("x=$x$x; ", 14) + "echo " + strings.Repeat("${x^^}", 200)},
		{"concat-quoted", "x=aaaa; " + strings.Repeat("x=$x$x; ", 14) + `y=` + strings.Repeat("$x", 200)},
		// command substitution capturing unbounded output.
		{"cmdsubst", `y=$(i=0; while [ "$i" -lt 100 ]; do printf '%060000d' 0; i=$((i+1)); done)`},
		// ${x//?/…} global match allocates a match-index array ~len(subject).
		{"replace-index", "x=a; " + strings.Repeat("x=$x$x; ", 16) + "z=${x//?/}"},
	} {
		// A non-zero exit (Go error) is fine — the point is the limit fires and the
		// process does not crash with an uncatchable OOM.
		stderr, _ := run(tc.src)
		if !strings.Contains(stderr, "exceeds") {
			t.Errorf("%s: expected a byte-limit error on stderr, got %q", tc.name, stderr)
		}
	}

	// Ordinary expansions are unaffected.
	if stderr, err := run("printf '%d-%s\\n' 42 hi; echo a{1..5}b"); err != nil || stderr != "" {
		t.Errorf("ordinary expansion: err=%v stderr=%q", err, stderr)
	}
}

// TestDashHeredocByteLimit asserts that a <<- heredoc, which expands line by
// line, bounds its cumulative buffer against the budget rather than letting the
// sum of individually in-budget lines exhaust host memory.
func TestDashHeredocByteLimit(t *testing.T) {
	const cap = 1 << 20
	run := func(src string) error {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return err
		}
		r, err := interp.New(interp.StdIO(nil, io.Discard, io.Discard), interp.MaxExpandBytes(cap))
		if err != nil {
			return err
		}
		return r.Run(context.Background(), f)
	}

	// Each $x line is ~256 KiB (individually within budget); eight of them sum to
	// ~2 MiB, over the 1 MiB cap. The dash heredoc must reject rather than crash.
	var b strings.Builder
	b.WriteString("x=$(printf '%0262144d' 1)\n: <<-EOF\n")
	for range 8 {
		b.WriteString("$x\n")
	}
	b.WriteString("EOF\n")
	if err := run(b.String()); err == nil {
		t.Error("dash heredoc: expected a byte-limit error, got nil")
	}

	// An ordinary small dash heredoc still works.
	if err := run("x=hi\n: <<-EOF\n\t$x\n\tthere\nEOF\n"); err != nil {
		t.Errorf("ordinary dash heredoc should work: %v", err)
	}
}

// TestMapfileByteLimit asserts that mapfile/readarray bounds its cumulative
// buffer against the budget as it streams, so an unbounded producer cannot
// accumulate past MaxBytes. It specifically defeats an amortized power-of-two
// byte check: many empty lines slip past a checkpoint at ~zero bytes, then large
// lines added before the next checkpoint would otherwise overshoot arbitrarily.
func TestMapfileByteLimit(t *testing.T) {
	const cap = 1 << 20
	run := func(stdin string) error {
		f, err := syntax.NewParser().Parse(strings.NewReader("mapfile arr"), "")
		if err != nil {
			return err
		}
		r, err := interp.New(
			interp.StdIO(strings.NewReader(stdin), io.Discard, io.Discard),
			interp.MaxExpandBytes(cap),
		)
		if err != nil {
			return err
		}
		return r.Run(context.Background(), f)
	}

	// The ramp: 4096 empty lines (0 bytes, passes every early checkpoint) then
	// large lines whose sum (~1.9 MiB) blows the 1 MiB cap. Lines stay under
	// bufio.MaxScanTokenSize (64 KiB) so it is the byte cap that fires, not the
	// scanner's own token-length limit.
	var b strings.Builder
	for range 4096 {
		b.WriteByte('\n')
	}
	big := strings.Repeat("a", 60000)
	for range 32 {
		b.WriteString(big)
		b.WriteByte('\n')
	}
	if err := run(b.String()); err == nil {
		t.Error("mapfile ramp: expected a byte-limit error, got nil")
	}

	// An ordinary small mapfile still works.
	if err := run("one\ntwo\nthree\n"); err != nil {
		t.Errorf("ordinary mapfile should work: %v", err)
	}
}

// TestReadArrayByteLimit asserts that read -a bounds the array it builds from a
// line of many fields, rather than materializing an unbounded field list that
// exhausts host memory (ReadFields keeps every field for -a).
func TestReadArrayByteLimit(t *testing.T) {
	const cap = 8 << 20
	run := func(src, stdin string) error {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return err
		}
		r, err := interp.New(
			interp.StdIO(strings.NewReader(stdin), io.Discard, io.Discard),
			interp.MaxExpandBytes(cap),
		)
		if err != nil {
			return err
		}
		return r.Run(context.Background(), f)
	}

	// A line of ~3M single-character fields would build an array far past the
	// element cap; read -a must reject it.
	huge := strings.Repeat("a ", 3_000_000)
	if err := run("read -a arr", huge); err == nil {
		t.Error("read -a on a huge field line: expected an array-size error, got nil")
	}

	// An ordinary read -a still works.
	if err := run("read -a arr; echo ${#arr[@]}", "one two three\n"); err != nil {
		t.Errorf("ordinary read -a should work: %v", err)
	}
}

// TestFieldCountLimit asserts that IFS word-splitting bounds the number of
// fields, not just their bytes: a value of many tiny fields would otherwise
// allocate a slice header and struct per field — a large multiple of the byte
// budget — via the general field path (set --, for-in, argv).
func TestFieldCountLimit(t *testing.T) {
	const cap = 1 << 20 // maxFields = cap/64 = 16384
	run := func(src string) (string, error) {
		f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
		if err != nil {
			return "", err
		}
		var stderr strings.Builder
		r, err := interp.New(interp.StdIO(nil, io.Discard, &stderr), interp.MaxExpandBytes(cap))
		if err != nil {
			return "", err
		}
		runErr := r.Run(context.Background(), f)
		return stderr.String(), runErr
	}

	// A ~40k-field value (well within the 1 MiB byte budget) exceeds the field
	// count cap; splitting it must report the limit rather than allocate ~40k
	// field structs unbounded.
	val := strings.Repeat("a:", 40000)
	for _, tc := range []struct{ name, src string }{
		{"set", "IFS=:; x=" + val + "; set -- $x"},
		{"for-in", "IFS=:; x=" + val + "; for i in $x; do :; done"},
	} {
		stderr, _ := run(tc.src)
		if !strings.Contains(stderr, "too many fields") {
			t.Errorf("%s: expected a field-count error on stderr, got %q", tc.name, stderr)
		}
	}

	// Ordinary splitting is unaffected.
	if stderr, err := run("IFS=:; x=a:b:c:d:e; set -- $x; for i in one two three; do :; done"); err != nil || stderr != "" {
		t.Errorf("ordinary splitting: err=%v stderr=%q", err, stderr)
	}
}

// TestPrintfArgCyclingContainable asserts that printf cycling its format over a
// huge argument list honors context cancellation (it cannot spin uninterruptibly).
func TestPrintfArgCyclingContainable(t *testing.T) {
	src := `a[100000]=x; printf "%1000000d" "${a[@]}"`
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := interp.New(interp.StdIO(nil, io.Discard, io.Discard), interp.MaxExpandBytes(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = r.Run(ctx, f); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("printf did not honor context cancellation (uncontainable hang)")
	}
}
