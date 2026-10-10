package toolcounts

import "testing"

// The exact literals matter: check-docs compares a document's spelled-out
// count against this function, and mutants emits it into PORT.md. A change
// here silently changes what both tools consider correct.
func TestCountWordSpellsKnownLiterals(t *testing.T) {
	cases := map[int]string{
		0:  "zero",
		1:  "one",
		2:  "two",
		3:  "three",
		4:  "four",
		5:  "five",
		6:  "six",
		7:  "seven",
		8:  "eight",
		9:  "nine",
		10: "ten",
	}
	for n, want := range cases {
		if got := CountWord(n); got != want {
			t.Errorf("CountWord(%d) = %q, want %q", n, got, want)
		}
	}
}

// The fallback: counts the word list does not cover come back as digits, which
// is what a document restating a larger count must match.
func TestCountWordFallsBackToDigits(t *testing.T) {
	cases := map[int]string{
		11:  "11",
		12:  "12",
		190: "190",
		208: "208",
		-1:  "-1",
	}
	for n, want := range cases {
		if got := CountWord(n); got != want {
			t.Errorf("CountWord(%d) = %q, want %q", n, got, want)
		}
	}
}
