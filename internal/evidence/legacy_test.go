package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"strings"
	"testing"
)

// legacySplitNormalizedLines is the historical implementation, kept verbatim
// as the oracle for the streaming line scanner and hasher. Any change to
// countNormalizedLines or hashSelectedLines must keep these equal.
func legacySplitNormalizedLines(data []byte) []string {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	lines := strings.Split(normalized, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// legacySelectedHash computes the historical digest of a selected line range.
func legacySelectedHash(data []byte, lineStart, lineEnd int) string {
	lines := legacySplitNormalizedLines(data)
	selected := strings.Join(lines[lineStart-1:lineEnd], "\n")
	if selected != "" {
		selected += "\n"
	}
	sum := sha256.Sum256([]byte(selected))
	return hex.EncodeToString(sum[:])
}

func TestStreamingLineScanMatchesLegacy(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"single line no terminator", "abc"},
		{"single line with terminator", "abc\n"},
		{"single empty line", "\n"},
		{"two lines", "a\nb"},
		{"two lines terminated", "a\nb\n"},
		{"trailing empty line", "a\n\n"},
		{"only terminator", "\n\n"},
		{"crlf", "a\r\nb\r\n"},
		{"crlf unterminated", "a\r\nb"},
		{"lone cr", "a\rb\nc\r"},
		{"cr before crlf", "a\r\r\nb"},
		{"empty crlf line", "a\r\n\r\nb\r\n"},
		{"line of only cr", "\r\nb"},
		{"cr at eof", "a\nb\r"},
		{"crlf only", "\r\n"},
		{"double crlf", "\r\n\r\n"},
		{"unicode", "héllo\nwörld🎉\n"},
		{"unicode crlf", "héllo\r\nwörld🎉\r\n"},
		{"long lines", strings.Repeat("x", 100000) + "\n" + strings.Repeat("y", 50000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			want := len(legacySplitNormalizedLines(data))
			got := countNormalizedLines(data)
			if got != want {
				t.Fatalf("countNormalizedLines(%q) = %d, want %d", tc.data, got, want)
			}
			for lineStart := 1; lineStart <= got+1; lineStart++ {
				for lineEnd := lineStart - 1; lineEnd <= got+1; lineEnd++ {
					if lineStart < 1 || lineEnd < lineStart || lineEnd > got {
						continue // invalid ranges are rejected before hashing
					}
					wantHash := legacySelectedHash(data, lineStart, lineEnd)
					gotHash := hashSelectedLines(data, lineStart, lineEnd)
					if gotHash != wantHash {
						t.Fatalf("hashSelectedLines(%q, %d, %d) = %s, want %s", tc.data, lineStart, lineEnd, gotHash, wantHash)
					}
				}
			}
		})
	}
}

// TestStreamingLineScanRandomized compares both streaming passes against the
// legacy implementation over randomized byte inputs, including every byte
// value around the CRLF boundary handling.
func TestStreamingLineScanRandomized(t *testing.T) {
	generator := rand.New(rand.NewSource(1))
	alphabet := []byte{'a', 'b', '\n', '\r', '\r', ' ', 'é', 0xff}
	for iteration := 0; iteration < 2000; iteration++ {
		length := generator.Intn(64)
		data := make([]byte, length)
		for i := range data {
			data[i] = alphabet[generator.Intn(len(alphabet))]
		}
		wantLines := legacySplitNormalizedLines(data)
		gotCount := countNormalizedLines(data)
		if gotCount != len(wantLines) {
			t.Fatalf("iteration %d: countNormalizedLines(%q) = %d, want %d", iteration, data, gotCount, len(wantLines))
		}
		for lineStart := 1; lineStart <= gotCount; lineStart++ {
			for lineEnd := lineStart; lineEnd <= gotCount; lineEnd++ {
				wantHash := legacySelectedHash(data, lineStart, lineEnd)
				gotHash := hashSelectedLines(data, lineStart, lineEnd)
				if gotHash != wantHash {
					t.Fatalf("iteration %d: hashSelectedLines(%q, %d, %d) = %s, want %s", iteration, data, lineStart, lineEnd, gotHash, wantHash)
				}
			}
		}
	}
}
