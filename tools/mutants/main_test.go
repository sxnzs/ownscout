package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnchorsMatchTheRealSources is the most important test here.
//
// The measurement itself cannot run in a unit test - it builds five binaries and
// replays both corpora twice each - so nothing else in this file would notice if
// a refactor moved one of the five anchors. And a mutation whose anchor does not
// match is the one failure that would be silent: applyMutation errors, but if
// the anchor check were ever relaxed to a no-op the mutated build would be the
// unmutated one, score a perfect run, and be reported as "the
// corpus cannot see this bug". That is precisely the wrong conclusion.
func TestAnchorsMatchTheRealSources(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot: %v", err)
	}
	if len(mutations) == 0 {
		t.Fatal("no mutations defined")
	}
	seenName := map[string]bool{}
	seenLabel := map[string]bool{}
	for _, m := range mutations {
		if m.name == "" || m.label == "" || m.file == "" {
			t.Errorf("mutation %+v has an empty field", m)
		}
		if seenName[m.name] {
			t.Errorf("duplicate mutation name %q", m.name)
		}
		seenName[m.name] = true
		if seenLabel[m.label] {
			t.Errorf("duplicate mutation label %q", m.label)
		}
		seenLabel[m.label] = true
		if m.old == m.new {
			t.Errorf("mutation %s replaces its anchor with itself", m.name)
		}

		raw, err := os.ReadFile(filepath.Join(root, m.file))
		if err != nil {
			t.Errorf("mutation %s: read %s: %v", m.name, m.file, err)
			continue
		}
		if n := strings.Count(string(raw), m.old); n != 1 {
			t.Errorf("mutation %s: anchor occurs %d times in %s, want exactly 1",
				m.name, n, m.file)
		}
	}
}

func TestApplyMutationRefusesAmbiguousAnchors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		m       mutation
		wantErr string
	}{
		{
			name:    "absent",
			content: "nothing to see here\n",
			m:       mutation{name: "x", file: "f.go", old: "MISSING", new: "PRESENT"},
			wantErr: "anchor not found",
		},
		{
			name:    "ambiguous",
			content: "TWICE\nTWICE\n",
			m:       mutation{name: "x", file: "f.go", old: "TWICE", new: "ONCE"},
			wantErr: "occurs 2 times",
		},
		{
			name:    "unique",
			content: "before ANCHOR after\n",
			m:       mutation{name: "x", file: "f.go", old: "ANCHOR", new: "PATCHED"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.go")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			err := applyMutation(path, tc.m)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("applyMutation: %v", err)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "before PATCHED after\n" {
					t.Errorf("content = %q", got)
				}
				return
			}
			if err == nil {
				t.Fatal("applyMutation accepted a bad anchor")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestRenderRegion(t *testing.T) {
	res := result{
		baseTotal: 31,
		edgeTotal: 247,
		rows: []row{
			{"unmutated reference", 31, 247},
			{"first mutant", 29, 205},
			{"second mutant", 23, 123},
		},
	}
	// The intro's count comes from the mutation list, not from the rows passed
	// in, so this reads "five" even though the sample renders two mutants.
	got := renderRegion(res)
	want := "The corpus was mutation-tested against five deliberately broken reference builds:\n" +
		"\n" +
		"| Mutation | Base corpus | Edge corpus |\n" +
		"|---|---|---|\n" +
		"| first mutant | 29/31 | 205/247 |\n" +
		"| second mutant | 23/31 | 123/247 |\n" +
		"| unmutated reference | 31/31 | 247/247 |\n"
	if got != want {
		t.Errorf("renderRegion:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	// The baseline must be last: a reader scans mutations first.
	if i, j := strings.Index(got, "first mutant"), strings.Index(got, "unmutated reference"); i > j {
		t.Error("the unmutated baseline is not the last row")
	}
	// An empty measurement must not panic on rows[1:].
	if got := renderRegion(result{}); got != "" {
		t.Errorf("renderRegion(empty) = %q", got)
	}
}

func TestParseHarnessTotals(t *testing.T) {
	// Exit 0 (all passed) and exit 1 (a mutant failing cases) both carry a
	// usable score.
	for _, tc := range []struct {
		exit, passed int
		report       string
	}{
		{0, 31, `{"total":31,"passed":31,"failed":0}`},
		{1, 12, `{"total":31,"passed":12,"failed":19}`},
	} {
		passed, total, err := parseHarnessTotals(tc.exit, []byte(tc.report), "the base corpus")
		if err != nil {
			t.Fatalf("exit %d: parseHarnessTotals: %v", tc.exit, err)
		}
		if passed != tc.passed || total != 31 {
			t.Errorf("exit %d: got %d/%d, want %d/31", tc.exit, passed, total, tc.passed)
		}
	}

	// Anything else is infrastructure, not a caught mutation.
	for _, tc := range []struct {
		name string
		exit int
		out  []byte
	}{
		{"crash", 2, []byte("harness: binary is missing")},
		{"unparseable", 1, []byte("harness: 12/31 cases passed")},
		{"empty", 1, nil},
		{"zero total", 1, []byte(`{"total":0,"passed":0}`)},
		{"passed over total", 1, []byte(`{"total":3,"passed":4}`)},
		{"negative passed", 1, []byte(`{"total":3,"passed":-1}`)},
		{"success with failures", 0, []byte(`{"total":3,"passed":2,"failed":1}`)},
		{"failure with all passed", 1, []byte(`{"total":3,"passed":3,"failed":0}`)},
		{"inconsistent failed", 1, []byte(`{"total":3,"passed":2,"failed":2}`)},
		{"missing failed", 0, []byte(`{"total":3,"passed":3}`)},
		{"missing passed", 1, []byte(`{"total":3,"failed":3}`)},
	} {
		if _, _, err := parseHarnessTotals(tc.exit, tc.out, "the base corpus"); err == nil {
			t.Errorf("%s: parseHarnessTotals accepted bad input", tc.name)
		}
	}
}

const sampleDoc = `Intro prose.

The corpus was mutation-tested against four deliberately broken reference builds:

| Mutation | Base corpus | Edge corpus |
|---|---|---|
| a | 9/31 | 2/247 |
| unmutated reference | 31/31 | 247/247 |

Hand-written prose that the tool must not touch.
`

func TestFindRegionBounds(t *testing.T) {
	start, end, err := findRegion(sampleDoc)
	if err != nil {
		t.Fatalf("findRegion: %v", err)
	}
	lines := strings.Split(sampleDoc, "\n")
	if !strings.HasPrefix(lines[start], governedStart) {
		t.Errorf("region starts at %q", lines[start])
	}
	if !strings.Contains(lines[end], "unmutated reference") {
		t.Errorf("region ends at %q, want the last table row", lines[end])
	}
	if strings.Contains(strings.Join(lines[start:end+1], "\n"), "Hand-written prose") {
		t.Error("the region swallowed the prose that follows the table")
	}
}

func TestFindRegionRejectsBadDocuments(t *testing.T) {
	if _, _, err := findRegion("no table here\n"); err == nil {
		t.Error("findRegion accepted a document with no intro sentence")
	}
	duplicated := sampleDoc + "\n" + governedStart + "five builds:\n"
	if _, _, err := findRegion(duplicated); err == nil {
		t.Error("findRegion accepted two intro sentences")
	}
	noTable := governedStart + "four builds:\n\nno table\n"
	if _, _, err := findRegion(noTable); err == nil {
		t.Error("findRegion accepted an intro sentence with no table")
	}
}

func TestCheckAndWriteRegion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PORT.md")
	if err := os.WriteFile(path, []byte(sampleDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := renderRegion(result{baseTotal: 31, edgeTotal: 247, rows: []row{
		{"unmutated reference", 31, 247},
		{"a", 1, 2},
	}})

	stale, err := checkRegion(path, fresh)
	if err != nil {
		t.Fatalf("checkRegion: %v", err)
	}
	if !stale {
		t.Error("checkRegion called a divergent table fresh")
	}
	if err := writeRegion(path, fresh); err != nil {
		t.Fatalf("writeRegion: %v", err)
	}
	stale, err = checkRegion(path, fresh)
	if err != nil {
		t.Fatalf("checkRegion after write: %v", err)
	}
	if stale {
		t.Error("checkRegion still reports staleness after writeRegion")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rewriting must preserve everything outside the governed region, including
	// the prose above and below it.
	for _, keep := range []string{"Intro prose.", "Hand-written prose that the tool must not touch."} {
		if !strings.Contains(string(raw), keep) {
			t.Errorf("writeRegion dropped %q", keep)
		}
	}
}
