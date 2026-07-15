// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"github.com/goccy/sh/v3/syntax"
)

// Braces performs brace expansion on a word, given that it contains any
// [syntax.BraceExp] parts. For example, the word with a brace expansion
// "foo{bar,baz}" will return two literal words, "foobar" and "foobaz".
//
// Note that the resulting words may share word parts.
//
// Deprecated: use [BracesSeq], which yields words lazily and reports an
// error rather than letting a large sequence allocate huge amounts.
func Braces(word *syntax.Word) []*syntax.Word {
	var all []*syntax.Word
	bracesSeqRec(word, 0, func(w *syntax.Word, err error) bool {
		// The deprecated API has no error channel; stop safely on an error or once
		// the element count reaches the same bound BracesSeq enforces, so a huge
		// sequence like {1..9999999999} cannot exhaust memory here either.
		if err != nil || len(all) >= maxBraceElems {
			return false
		}
		all = append(all, w)
		return true
	})
	return all
}

// maxBraceDepth bounds how deeply brace expansions may nest. Unlike the width
// limit in [BracesSeq], depth is not visible to the yield callback (left-nested
// input like {{{…,z},z},z} descends fully before yielding a single word), so
// bracesSeqRec must bound its own recursion or a deep literal overflows the
// goroutine stack — a fatal, unrecoverable runtime throw.
const maxBraceDepth = 16 << 10

// maxBraceElems bounds the number of words a single brace expansion may yield —
// a guard against combinatorial blow-ups like {1..100}{1..100}{1..100}.
const maxBraceElems = 16 << 10

// maxBraceSeqPad bounds the zero-padding width of a numeric brace sequence
// ({0001..9}). The width is attacker-controlled and applied to every generated
// element, so without a bound a tiny input (many leading zeros) allocates memory
// proportional to width times the element count — an amplification that can
// exhaust host memory (an uncatchable OOM). Real padding is a handful of digits.
const maxBraceSeqPad = 256

// BracesSeq performs brace expansion on a word, given that it contains any
// [syntax.BraceExp] parts. For example, the word with a brace expansion
// "foo{bar,baz}" will return two literal words, "foobar" and "foobaz".
//
// The iteration yields an error and stops if the total expansion is too
// large, including combinatorial blow-ups across multiple brace expansions
// like {1..100}{1..100}{1..100}. This may be configurable with cfg in the
// future; the parameter is entirely unused for now.
//
// Note that the resulting words may share word parts.
func BracesSeq(cfg *Config, word *syntax.Word) iter.Seq2[*syntax.Word, error] {
	return func(yield func(*syntax.Word, error) bool) {
		// maxBraceElems expanded elements is more than any script should need in
		// practice, but small enough that we don't waste too much memory and CPU.
		count := 0
		bracesSeqRec(word, 0, func(w *syntax.Word, err error) bool {
			if err != nil {
				yield(nil, err)
				return false
			}
			count++
			if count > maxBraceElems {
				yield(nil, fmt.Errorf("brace expansion would exceed %d elements", maxBraceElems))
				return false
			}
			return yield(w, nil)
		})
	}
}

// bracesSeqRec yields each fully-expanded word descended from word. It yields a
// non-nil error (and stops) if the brace nesting exceeds maxBraceDepth, so deep
// input fails gracefully instead of overflowing the goroutine stack. It returns
// false if iteration should stop.
func bracesSeqRec(word *syntax.Word, depth int, yield func(*syntax.Word, error) bool) bool {
	if depth > maxBraceDepth {
		return yield(nil, fmt.Errorf("brace expansion nested more than %d deep", maxBraceDepth))
	}
	var left []syntax.WordPart
	for i, wp := range word.Parts {
		br, ok := wp.(*syntax.BraceExp)
		if !ok {
			left = append(left, wp)
			continue
		}
		rest := word.Parts[i+1:]
		// Yield each word produced by recursing on `next`,
		// after prepending `left` to its Parts.
		expand := func(next *syntax.Word) bool {
			return bracesSeqRec(next, depth+1, func(w *syntax.Word, err error) bool {
				if err != nil {
					return yield(nil, err)
				}
				w.Parts = append(append([]syntax.WordPart(nil), left...), w.Parts...)
				return yield(w, nil)
			})
		}
		if br.Sequence {
			fromLit := br.Elems[0].Lit()
			toLit := br.Elems[1].Lit()
			zeros := max(extraLeadingZeros(fromLit), extraLeadingZeros(toLit))
			if zeros > maxBraceSeqPad {
				return yield(nil, fmt.Errorf("brace expansion zero-padding exceeds %d digits", maxBraceSeqPad))
			}

			chars := false
			// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
			from, err1 := strconv.ParseInt(fromLit, 10, 64)
			to, err2 := strconv.ParseInt(toLit, 10, 64)
			if err1 != nil || err2 != nil {
				chars = true
				from = int64(fromLit[0])
				to = int64(toLit[0])
			}
			upward := from <= to
			incr := int64(1)
			if !upward {
				incr = -1
			}
			if len(br.Elems) > 2 {
				// ParseInt with bit size 64 to ensure consistent behavior on 32-bit platforms.
				n, _ := strconv.ParseInt(br.Elems[2].Lit(), 10, 64)
				if n != 0 && n > 0 == upward {
					incr = n
				}
			}
			for n := from; (upward && n <= to) || (!upward && n >= to); n += incr {
				next := *word
				lit := &syntax.Lit{}
				if chars {
					lit.Value = string(rune(n))
				} else {
					lit.Value = strings.Repeat("0", zeros) + strconv.FormatInt(n, 10)
				}
				next.Parts = append([]syntax.WordPart{lit}, rest...)
				if !expand(&next) {
					return false
				}
			}
			return true
		}
		for _, elem := range br.Elems {
			next := *word
			next.Parts = append(append([]syntax.WordPart(nil), elem.Parts...), rest...)
			if !expand(&next) {
				return false
			}
		}
		return true
	}
	return yield(&syntax.Word{Parts: left}, nil)
}

func extraLeadingZeros(s string) int {
	for i, r := range s {
		if r != '0' {
			return i
		}
	}
	return 0 // "0" has no extra leading zeros
}
