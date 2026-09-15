// Command check-docs fails when a document restates a corpus count that no
// longer matches the corpus.
//
// The case counts appear in a dozen places - two badges, several prose
// sentences, a status table, the port contract, and two diagrams - and every one
// of them is a hand-maintained copy. They drift: a diagram once said "93 EDGE"
// beside a card that said 208, and a mutation table carried /190 denominators
// under a 208/208 total. Neither was caught by any gate, because the corpora
// themselves were perfectly consistent.
//
// This tool derives the counts from the corpora and asserts every restatement
// agrees. It is deliberately explicit rather than clever: each expectation names
// the file, the pattern, and which capture groups must equal which count, so a
// failure says exactly which document drifted and to what.
//
// It reads only. Counts that cannot be derived from the corpora - per-language
// test totals and mutant numerators - are checked for internal consistency
// instead, which is what catches a table disagreeing with itself.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Which corpus count a capture group must equal.
const (
	anyValue = iota
	baseValue
	edgeValue
)

type expectation struct {
	file  string
	label string
	re    *regexp.Regexp
	want  []int // one entry per capture group, or anyValue
}

type failure struct {
	file   string
	line   int
	label  string
	detail string
}

func (f failure) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.file, f.line, f.label, f.detail)
}

func main() {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs-check:", err)
		os.Exit(2)
	}
	base, edge, err := corpusCounts(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs-check:", err)
		os.Exit(2)
	}

	expectations := []expectation{
		// README badges and prose.
		{"README.md", "parity badge", regexp.MustCompile(`badge/parity-(\d+)%2F(\d+)%20%2B%20(\d+)%2F(\d+)-`),
			[]int{baseValue, baseValue, edgeValue, edgeValue}},
		{"README.md", "parity badge alt", regexp.MustCompile(`alt="Parity (\d+)/(\d+) \+ (\d+)/(\d+)"`),
			[]int{baseValue, baseValue, edgeValue, edgeValue}},
		{"README.md", "tests badge", regexp.MustCompile(`tests-(\d+)%20passing`), []int{anyValue}},
		{"README.md", "parity matrix alt", regexp.MustCompile(`passes (\d+) base and (\d+) edge cases`),
			[]int{baseValue, edgeValue}},
		{"README.md", "recorded oracle", regexp.MustCompile(`records (\d+) base cases and (\d+) hardening`),
			[]int{baseValue, edgeValue}},
		{"README.md", "mutation prose", regexp.MustCompile(`base\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+);\s*edge\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+)`),
			[]int{anyValue, baseValue, anyValue, baseValue, anyValue, baseValue, anyValue, baseValue,
				anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue}},
		{"README.md", "mutation corpus label", regexp.MustCompile(`against the (\d+)\+(\d+) corpus`),
			[]int{baseValue, edgeValue}},

		// Status table.
		{"docs/PORTS.md", "port table", regexp.MustCompile(`\| (\d+)/(\d+) \| (\d+)/(\d+) \| (\d+) \|`),
			[]int{baseValue, baseValue, edgeValue, edgeValue, anyValue}},
		{"docs/PORTS.md", "edge corpus line", regexp.MustCompile(`Edge trace corpus: (\d+) cases`), []int{edgeValue}},
		{"docs/PORTS.md", "all ports line", regexp.MustCompile(`All three ports pass (\d+)/(\d+)`), []int{edgeValue, edgeValue}},
		{"docs/PORTS.md", "mutation table", regexp.MustCompile(`\| ([^|]+?) \| (\d+)/(\d+) \| (\d+)/(\d+) \|`),
			[]int{anyValue, anyValue, baseValue, anyValue, edgeValue}},
		{"docs/PORTS.md", "mutation prose", regexp.MustCompile(`base\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+);\s*edge\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+)`),
			[]int{anyValue, baseValue, anyValue, baseValue, anyValue, baseValue, anyValue, baseValue,
				anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue}},

		// Port contract.
		{"spec/parity/PORT.md", "edge corpus size", regexp.MustCompile("`corpus-edge.json` \\((\\d+) cases\\)"), []int{edgeValue}},
		{"spec/parity/PORT.md", "mutation table", regexp.MustCompile(`\| ([^|]+?) \| (\d+)/(\d+) \| (\d+)/(\d+) \|`),
			[]int{anyValue, anyValue, baseValue, anyValue, edgeValue}},

		// Port guide.
		{"ports/README.md", "corpus sizes", regexp.MustCompile(`\((\d+) base cases, (\d+) edge\s+cases\)`),
			[]int{baseValue, edgeValue}},

		// Diagrams.
		{"docs/assets/architecture.svg", "corpus band", regexp.MustCompile(`(\d+) BASE \+ (\d+) EDGE`),
			[]int{baseValue, edgeValue}},
		{"docs/assets/architecture.svg", "corpus card", regexp.MustCompile(`(\d+) base \+ (\d+) edge`),
			[]int{baseValue, edgeValue}},
		{"docs/assets/parity.svg", "aria label", regexp.MustCompile(`replays (\d+) base and (\d+) edge cases`), []int{baseValue, edgeValue}},
		{"docs/assets/parity.svg", "base column", regexp.MustCompile(`x="560"[^>]*>(\d+)/(\d+)<`), []int{baseValue, baseValue}},
		{"docs/assets/parity.svg", "edge column", regexp.MustCompile(`x="690"[^>]*>(\d+)/(\d+)<`), []int{edgeValue, edgeValue}},
	}

	var failures []failure
	for _, e := range expectations {
		found, err := evaluate(root, e, base, edge)
		if err != nil {
			fmt.Fprintln(os.Stderr, "docs-check:", err)
			os.Exit(2)
		}
		failures = append(failures, found...)
	}
	total, err := testTotalConsistency(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs-check:", err)
		os.Exit(2)
	}
	failures = append(failures, total...)

	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Println(f)
		}
		fmt.Printf("docs-check: %d mismatch(es) against the %d+%d corpus\n", len(failures), base, edge)
		os.Exit(1)
	}
	fmt.Printf("docs-check: %d+%d consistent across %d expectations\n", base, edge, len(expectations)+1)
}

// evaluate checks one expectation, returning a failure for every match whose
// capture groups disagree and one for a pattern that no longer appears at all.
// A deleted line must fail loudly rather than silently pass.
func evaluate(root string, e expectation, base, edge int) ([]failure, error) {
	data, err := os.ReadFile(filepath.Join(root, e.file))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", e.file, err)
	}
	text := string(data)
	matches := e.re.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return []failure{{e.file, 0, e.label, fmt.Sprintf("pattern %s no longer appears", e.re)}}, nil
	}
	var failures []failure
	for _, m := range matches {
		line := 1 + strings.Count(text[:m[0]], "\n")
		for group := range e.want {
			if e.want[group] == anyValue {
				continue
			}
			start, end := m[2*(group+1)], m[2*(group+1)+1]
			if start < 0 {
				continue
			}
			want := base
			name := "base"
			if e.want[group] == edgeValue {
				want, name = edge, "edge"
			}
			got := text[start:end]
			if got != strconv.Itoa(want) {
				failures = append(failures, failure{e.file, line, e.label,
					fmt.Sprintf("%s count is %s, corpus says %d", name, got, want)})
			}
		}
	}
	return failures, nil
}

// testTotalConsistency checks a quantity the corpus cannot supply: the total in
// the README badge must equal the sum of the per-language counts in the parity
// diagram. That is what catches one document contradicting another about a
// number neither of them derives.
func testTotalConsistency(root string) ([]failure, error) {
	svg, err := os.ReadFile(filepath.Join(root, "docs/assets/parity.svg"))
	if err != nil {
		return nil, fmt.Errorf("read parity.svg: %w", err)
	}
	cells := regexp.MustCompile(`x="820"[^>]*>(\d+)<`).FindAllStringSubmatch(string(svg), -1)
	if len(cells) == 0 {
		return []failure{{"docs/assets/parity.svg", 0, "tests column", "no test counts found"}}, nil
	}
	sum := 0
	for _, c := range cells {
		n, err := strconv.Atoi(c[1])
		if err != nil {
			return nil, fmt.Errorf("parse test count %q: %w", c[1], err)
		}
		sum += n
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return nil, fmt.Errorf("read README.md: %w", err)
	}
	badge := regexp.MustCompile(`tests-(\d+)%20passing`).FindStringSubmatch(string(readme))
	if badge == nil {
		return []failure{{"README.md", 0, "tests badge", "tests badge no longer appears"}}, nil
	}
	if badge[1] != strconv.Itoa(sum) {
		return []failure{{"README.md", 0, "tests badge",
			fmt.Sprintf("badge says %s, the parity diagram's per-language counts sum to %d", badge[1], sum)}}, nil
	}
	return nil, nil
}

func corpusCounts(root string) (int, int, error) {
	count := func(name string) (int, error) {
		data, err := os.ReadFile(filepath.Join(root, "spec", "parity", name))
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", name, err)
		}
		var corpus struct {
			Cases []json.RawMessage `json:"cases"`
		}
		if err := json.Unmarshal(data, &corpus); err != nil {
			return 0, fmt.Errorf("parse %s: %w", name, err)
		}
		return len(corpus.Cases), nil
	}
	base, err := count("corpus.json")
	if err != nil {
		return 0, 0, err
	}
	edge, err := count("corpus-edge.json")
	if err != nil {
		return 0, 0, err
	}
	return base, edge, nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
