package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func band() expectation {
	return expectation{
		file:  "diagram.svg",
		label: "corpus band",
		re:    regexp.MustCompile(`(\d+) BASE \+ (\d+) EDGE`),
		want:  []int{baseValue, edgeValue},
	}
}

func TestEvaluateAcceptsMatchingCounts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "diagram.svg", "<text>31 BASE + 247 EDGE</text>\n")

	failures, err := evaluate(root, band(), 31, 247)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %v", failures)
	}
}

func TestEvaluateCatchesMismatchAndNamesTheFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "diagram.svg", "line one\n<text>28 BASE + 213 EDGE</text>\n")

	failures, err := evaluate(root, band(), 31, 247)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 {
		t.Fatalf("expected two failures, got %d: %v", len(failures), failures)
	}
	joined := failures[0].String() + "|" + failures[1].String()
	for _, want := range []string{"diagram.svg:2", "base count is 28, corpus says 31", "edge count is 213, corpus says 247"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("failure output %q missing %q", joined, want)
		}
	}
}

// A deleted line must fail loudly rather than pass because nothing matched.
func TestEvaluateFailsWhenThePatternIsGone(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "diagram.svg", "<text>nothing to see</text>\n")

	failures, err := evaluate(root, band(), 31, 247)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].detail, "no longer appears") {
		t.Fatalf("expected a missing-pattern failure, got %v", failures)
	}
}

// The class this tool exists for: one file stating the same quantity twice with
// different values. Both occurrences are checked, so the file cannot agree with
// itself while disagreeing with the corpus.
func TestEvaluateCatchesOneFileContradictingItself(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "diagram.svg", "31 BASE + 247 EDGE\n31 BASE + 245 EDGE\n")

	failures, err := evaluate(root, band(), 31, 247)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 {
		t.Fatalf("expected exactly the contradictory occurrence to fail, got %v", failures)
	}
	if failures[0].line != 2 || !strings.Contains(failures[0].detail, "edge count is 245, corpus says 247") {
		t.Fatalf("unexpected failure: %v", failures[0])
	}
}

// A quantity the corpus cannot supply is checked for internal consistency: the
// README total must equal the sum of the per-language counts in the diagram.
func TestTestTotalConsistencyComparesBadgeToPerLanguageCounts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/assets/parity.svg", strings.Join([]string{
		`<text x="820" y="194">77</text>`,
		`<text x="820" y="232">149</text>`,
		`<text x="820" y="270">38</text>`,
		`<text x="820" y="308">37</text>`,
	}, "\n"))
	writeFile(t, root, "README.md", `<img src="badge/tests-301%20passing">`)

	failures, err := testTotalConsistency(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("expected the badge to agree with the sum, got %v", failures)
	}

	writeFile(t, root, "README.md", `<img src="badge/tests-290%20passing">`)
	failures, err = testTotalConsistency(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].detail, "sum to 301") {
		t.Fatalf("expected a badge/sum disagreement, got %v", failures)
	}
}

func TestCorpusCountsReadsTheCaseArrays(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "spec/parity/corpus.json", `{"cases":[{"name":"a"},{"name":"b"}]}`)
	writeFile(t, root, "spec/parity/corpus-edge.json", `{"cases":[{"name":"c"},{"name":"d"},{"name":"e"}]}`)

	base, edge, err := corpusCounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if base != 2 || edge != 3 {
		t.Fatalf("got %d+%d, want 2+3", base, edge)
	}
}

func TestCorpusCountsFailsOnAMissingCorpus(t *testing.T) {
	if _, _, err := corpusCounts(t.TempDir()); err == nil {
		t.Fatal("expected an error when the corpus is missing")
	}
}
