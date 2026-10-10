// Package toolcounts holds the shared wording for small counts so that a
// producer and a checker of the same document spell the number identically.
//
// It deliberately holds wording only. Parsing counts out of a document stays
// with each tool that needs it: sharing a parser between the tool that writes
// a table and the tool that checks it would give both the same blind spot.
package toolcounts

import "strconv"

// words spells small counts, so a document can say "four".
var words = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

// CountWord spells n when it is one of the small counts this package knows
// (0 through 10), and falls back to the decimal digits otherwise.
func CountWord(n int) string {
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}
