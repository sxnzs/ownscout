package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/contract"
)

// The relocation path locates lines through a precomputed start index, while
// the verification path walks the file from byte zero. The two must agree on
// every range of every awkward payload, or a relocation could point at a window
// that verification would have hashed differently. This is the differential
// reference for that equivalence.
func TestWindowHashMatchesReferenceHashing(t *testing.T) {
	payloads := []string{
		"",
		"\n",
		"\n\n",
		"a",
		"a\n",
		"a\nb\n",
		"a\n\nb\n",
		"a\r\nb\r\n",
		"a\r\nb",
		"a\r\n\r\nb\r\n",
		"a\rb\n",
		"a\rb",
		// A trailing "\r" on an unterminated final line is content, not a
		// terminator. This is the shape that a "\r"-stripping contentEnd gets
		// wrong, so it must stay in this list.
		"a\nb\r",
		"b\r",
		"\r",
		"a\r\nb\r",
		"héllo\n🎉\n",
		"x\xff\xfey\n",
		"\xff\n\xfe\n",
		"one\ntwo\nthree\nfour\nfive\n",
		"one\r\ntwo\r\nthree\r\nfour\r\nfive",
		" \n\t\n\n  \n",
		"a\n" + strings.Repeat("\n", 7) + "b\n",
		strings.Repeat("long line payload\n", 40),
	}

	for _, payload := range payloads {
		data := []byte(payload)
		starts := lineStarts(data)
		total := countNormalizedLines(data)
		if total != len(starts) {
			t.Fatalf("payload %q: countNormalizedLines = %d, lineStarts = %d", payload, total, len(starts))
		}
		for start := 1; start <= total; start++ {
			for end := start; end <= total; end++ {
				want := hashSelectedLines(data, start, end)
				got := windowHash(data, starts, start, end)
				if got != want {
					t.Fatalf("payload %q range %d-%d: windowHash = %s, hashSelectedLines = %s",
						payload, start, end, got, want)
				}
			}
		}
	}
}

func TestResolveAnchor(t *testing.T) {
	// Ten lines, so windows are easy to reason about by hand.
	ten := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"

	tests := []struct {
		name       string
		data       string
		citeStart  int
		citeEnd    int
		fromStart  int // window whose fingerprint is recorded
		fromEnd    int
		wantFound  bool
		wantStart  int
		wantEnd    int
		wantShift  int
		exhaustive bool
	}{
		{
			name:      "shifted down",
			data:      ten,
			citeStart: 2, citeEnd: 3,
			fromStart: 5, fromEnd: 6,
			wantFound: true, wantStart: 5, wantEnd: 6, wantShift: 3,
		},
		{
			name:      "shifted up",
			data:      ten,
			citeStart: 3, citeEnd: 4,
			fromStart: 1, fromEnd: 2,
			wantFound: true, wantStart: 1, wantEnd: 2, wantShift: -2,
		},
		{
			name:      "single line moved",
			data:      ten,
			citeStart: 9, citeEnd: 9,
			fromStart: 4, fromEnd: 4,
			wantFound: true, wantStart: 4, wantEnd: 4, wantShift: -5,
		},
		{
			name:      "content absent",
			data:      ten,
			citeStart: 1, citeEnd: 2,
			fromStart: 0, fromEnd: 0, // fingerprint of nothing at all
			wantFound: false, exhaustive: true,
		},
		{
			name:      "cited range past end of shrunken file",
			data:      "a\nb\nc\n",
			citeStart: 9000, citeEnd: 9001,
			fromStart: 1, fromEnd: 2,
			wantFound: true, wantStart: 1, wantEnd: 2, wantShift: -8999,
		},
		{
			name:      "cited start below the first line is clamped",
			data:      ten,
			citeStart: 0, citeEnd: 1,
			fromStart: 1, fromEnd: 2,
			wantFound: true, wantStart: 1, wantEnd: 2, wantShift: 1,
		},
		{
			name:      "nearest of several duplicate windows wins, ties to the lower line",
			data:      "same\nx\nsame\n",
			citeStart: 2, citeEnd: 2,
			fromStart: 1, fromEnd: 1,
			wantFound: true, wantStart: 1, wantEnd: 1, wantShift: -1,
		},
		{
			name:      "extent longer than the file",
			data:      "a\nb\n",
			citeStart: 1, citeEnd: 5,
			fromStart: 1, fromEnd: 5,
			wantFound: false, exhaustive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			starts := lineStarts(data)
			total := countNormalizedLines(data)

			expected := strings.Repeat("0", 64)
			if tt.fromStart > 0 && tt.fromEnd <= total {
				expected = windowHash(data, starts, tt.fromStart, tt.fromEnd)
			}

			got := resolveAnchor(data, starts, total, tt.citeStart, tt.citeEnd, expected)
			if got.found != tt.wantFound {
				t.Fatalf("found = %v, want %v (%+v)", got.found, tt.wantFound, got)
			}
			if !tt.wantFound {
				if got.exhaustive != tt.exhaustive {
					t.Fatalf("exhaustive = %v, want %v", got.exhaustive, tt.exhaustive)
				}
				return
			}
			if got.lineStart != tt.wantStart || got.lineEnd != tt.wantEnd {
				t.Fatalf("match = %d-%d, want %d-%d", got.lineStart, got.lineEnd, tt.wantStart, tt.wantEnd)
			}
			if got.shift != tt.wantShift {
				t.Fatalf("shift = %d, want %d", got.shift, tt.wantShift)
			}
			// The reported window must genuinely hash to the recorded value.
			if windowHash(data, starts, got.lineStart, got.lineEnd) != expected {
				t.Fatal("reported window does not match the recorded fingerprint")
			}
		})
	}
}

// An unresolvable anchor in a file too large to cover must not claim the content
// is absent, and must stop rather than scan without bound.
func TestResolveAnchorStopsOnByteBudget(t *testing.T) {
	line := strings.Repeat("x", 63) + "\n"
	data := []byte(strings.Repeat(line, 40000))
	starts := lineStarts(data)
	total := countNormalizedLines(data)

	got := resolveAnchor(data, starts, total, 20000, 20019, strings.Repeat("0", 64))
	if got.found {
		t.Fatal("a fingerprint of zeros should not match")
	}
	if got.exhaustive {
		t.Fatal("a 2.5 MiB file cannot be covered within the byte budget")
	}
}

// Relocation is diagnostic. It must never change a status, a counter, or the
// overall verdict.
func TestRelocationDoesNotChangeTheVerdict(t *testing.T) {
	root := t.TempDir()
	contents := "alpha\nbeta\ngamma\ndelta\n"
	writeEvidenceFile(t, root, "notes.txt", contents)

	// The recorded fingerprint is of lines 3-4, but the span cites lines 1-2.
	recorded := spanHash("gamma\ndelta\n")
	item := contract.Evidence{
		EvidenceID:  "evidence-1",
		Path:        "notes.txt",
		LineStart:   1,
		LineEnd:     2,
		ContentHash: "sha256:" + recorded,
	}
	packet := contract.Packet{Evidence: []contract.Evidence{item}}

	plain, err := VerifyPacket(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	annotated, err := VerifyPacketWithOptions(root, packet, Options{Relocate: true})
	if err != nil {
		t.Fatal(err)
	}

	if plain.Ok != annotated.Ok || plain.Ok {
		t.Fatalf("Ok = %v and %v, want both false", plain.Ok, annotated.Ok)
	}
	if annotated.VerifiedCount != plain.VerifiedCount ||
		annotated.FailedCount != plain.FailedCount ||
		annotated.SkippedCount != plain.SkippedCount {
		t.Fatalf("counters changed: %+v vs %+v", plain, annotated)
	}
	if annotated.Results[0].Status != "failed" {
		t.Fatalf("status = %q, want failed", annotated.Results[0].Status)
	}
	if annotated.Results[0].ActualHash != plain.Results[0].ActualHash {
		t.Fatal("relocation changed the reported actual hash")
	}
	if !strings.Contains(annotated.Results[0].Message, "content relocates to lines 3-4 (shift +2; nearest matching window)") {
		t.Fatalf("message = %q", annotated.Results[0].Message)
	}
	if strings.Contains(plain.Results[0].Message, "relocates") {
		t.Fatalf("relocation ran without the option: %q", plain.Results[0].Message)
	}
}

func TestRelocationClauseDetails(t *testing.T) {
	data := []byte("alpha\nbeta\ngamma\ndelta\n")
	starts := lineStarts(data)
	total := countNormalizedLines(data)

	// Found: nearest matching window is reported with a signed shift.
	found := relocationClause(data, total, 1, 2, windowHash(data, starts, 3, 4))
	if found != "; content relocates to lines 3-4 (shift +2; nearest matching window)" {
		t.Fatalf("found clause = %q", found)
	}

	// Not found, whole file covered: absence is earned, not assumed.
	exhaustive := relocationClause(data, total, 1, 2, strings.Repeat("0", 64))
	if exhaustive != "; content not found elsewhere in this file" {
		t.Fatalf("exhaustive clause = %q", exhaustive)
	}

	// A range that cannot describe a window yields no clause at all, which
	// keeps the message byte-identical to the pre-relocation wording.
	if clause := relocationClause(data, total, 2, 1, strings.Repeat("0", 64)); clause != "" {
		t.Fatalf("reversed range clause = %q, want empty", clause)
	}
}

// A file whose final line is unterminated and ends with "\r" must be hashed by
// the relocation path exactly as verification hashes it: the "\r" is content,
// because there is no "\n" for it to terminate.
func TestWindowHashKeepsTrailingCarriageReturnOnUnterminatedLine(t *testing.T) {
	data := []byte("a\nb\r")
	starts := lineStarts(data)
	if total := countNormalizedLines(data); total != 2 {
		t.Fatalf("countNormalizedLines = %d, want 2", total)
	}
	got := windowHash(data, starts, 2, 2)
	want := hashSelectedLines(data, 2, 2)
	if got != want {
		t.Fatalf("windowHash = %s, hashSelectedLines = %s", got, want)
	}
	// The kept form is "b\r\n"; the stripped form would be "b\n" and must not
	// be what the relocation path produces.
	if got != spanHash("b\r\n") {
		t.Fatalf("windowHash = %s, want the CR-keeping hash %s", got, spanHash("b\r\n"))
	}
	if got == spanHash("b\n") {
		t.Fatal("windowHash stripped a trailing CR from an unterminated line")
	}
}

func writeEvidenceFile(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
