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
	// mutationValue means "the spelled-out number of mutation rows in the port
	// contract's table" - a quantity the corpus cannot supply, but one the
	// documents still disagree about unless something pins them.
	mutationValue
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
	mutants, err := mutationCount(root)
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
		{"README.md", "mutation prose", regexp.MustCompile(`base\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+);\s*edge\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+)`),
			[]int{anyValue, baseValue, anyValue, baseValue, anyValue, baseValue, anyValue, baseValue,
				anyValue, baseValue, anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue,
				anyValue, edgeValue, anyValue, edgeValue}},
		{"README.md", "mutation corpus label", regexp.MustCompile(`against the (\d+)\+(\d+) corpus`),
			[]int{baseValue, edgeValue}},

		// Status table.
		{"docs/PORTS.md", "port table", regexp.MustCompile(`\| (\d+)/(\d+) \| (\d+)/(\d+) \| (\d+) \|`),
			[]int{baseValue, baseValue, edgeValue, edgeValue, anyValue}},
		{"docs/PORTS.md", "edge corpus line", regexp.MustCompile(`Edge trace corpus: (\d+) cases`), []int{edgeValue}},
		{"docs/PORTS.md", "all ports line", regexp.MustCompile(`All three ports pass (\d+)/(\d+)`), []int{edgeValue, edgeValue}},
		// docs/PORTS.md carries its mutation numbers in prose, not in a table.
		// This pattern matches the port status table, whose first cell is a
		// binary path; it was labelled "mutation table" until that mislabel
		// sent a reader looking for a table that does not exist.
		{"docs/PORTS.md", "port status denominators", regexp.MustCompile(`\| ([^|]+?) \| (\d+)/(\d+) \| (\d+)/(\d+) \|`),
			[]int{anyValue, anyValue, baseValue, anyValue, edgeValue}},
		{"docs/PORTS.md", "mutation prose", regexp.MustCompile(`base\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+);\s*edge\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+),\s+(\d+)/(\d+)\s+and\s+(\d+)/(\d+)`),
			[]int{anyValue, baseValue, anyValue, baseValue, anyValue, baseValue, anyValue, baseValue,
				anyValue, baseValue, anyValue, edgeValue, anyValue, edgeValue, anyValue, edgeValue,
				anyValue, edgeValue, anyValue, edgeValue}},

		// Port contract.
		{"spec/parity/PORT.md", "edge corpus size", regexp.MustCompile("`corpus-edge.json` \\((\\d+) cases\\)"), []int{edgeValue}},
		{"spec/parity/PORT.md", "mutation table", regexp.MustCompile(`\| ([^|]+?) \| (\d+)/(\d+) \| (\d+)/(\d+) \|`),
			[]int{anyValue, anyValue, baseValue, anyValue, edgeValue}},
		// The gate section a port author reads first. It claimed "reports
		// 28/28" for several corpus revisions while every other copy of the
		// counts was checked, so the one statement a porter acts on was the one
		// number nobody watched.
		{"spec/parity/PORT.md", "gate base corpus", regexp.MustCompile("harness\\.py --bin <binary>` reports (\\d+)/(\\d+)"),
			[]int{baseValue, baseValue}},
		{"spec/parity/PORT.md", "gate edge corpus", regexp.MustCompile("corpus-edge\\.json` reports (\\d+)/(\\d+)"),
			[]int{edgeValue, edgeValue}},

		// The mutation count itself. The table's rows are the only source for
		// it, and every document that spells it out must agree with them. This
		// guards a drift that actually shipped: the port contract announced
		// "three deliberately broken reference builds" above a table listing
		// four, and no gate noticed.
		{"spec/parity/PORT.md", "mutation count", regexp.MustCompile(`([A-Za-z]+)\s+deliberately broken reference builds`),
			[]int{mutationValue}},
		{"docs/PORTS.md", "mutation count", regexp.MustCompile(`([A-Za-z]+)\s+deliberately broken reference builds`),
			[]int{mutationValue}},
		{"README.md", "mutation count", regexp.MustCompile(`([A-Za-z]+)\s+deliberately broken reference builds`),
			[]int{mutationValue}},
		{"ports/README.md", "mutation count", regexp.MustCompile(`([A-Za-z]+)\s+deliberately broken reference builds`),
			[]int{mutationValue}},

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
		found, err := evaluate(root, e, base, edge, mutants)
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
func evaluate(root string, e expectation, base, edge, mutants int) ([]failure, error) {
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
			want := strconv.Itoa(base)
			name := "base"
			switch e.want[group] {
			case edgeValue:
				want, name = strconv.Itoa(edge), "edge"
			case mutationValue:
				want, name = countWord(mutants), "mutation"
			}
			got := text[start:end]
			if !strings.EqualFold(got, want) {
				failures = append(failures, failure{e.file, line, e.label,
					fmt.Sprintf("%s count is %q, expected %q", name, got, want)})
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

// mutationCount derives how many deliberately broken builds the port contract
// documents, from the contract's own table. Everything else that names this
// quantity is compared against it.
//
// The table's last row is the unmutated baseline, which is not a mutation and
// must not be counted; the spelled-out numbers in the prose mean "how many
// broken builds", not "how many rows".
func mutationCount(root string) (int, error) {
	data, err := os.ReadFile(filepath.Join(root, "spec", "parity", "PORT.md"))
	if err != nil {
		return 0, fmt.Errorf("read PORT.md: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "| Mutation |") {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, fmt.Errorf("PORT.md: no mutation table found")
	}
	mutations := 0
	for _, line := range lines[start+1:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		if strings.Contains(line, "---") || strings.Contains(line, "unmutated reference") {
			continue
		}
		mutations++
	}
	if mutations == 0 {
		return 0, fmt.Errorf("PORT.md: the mutation table has no mutation rows")
	}
	return mutations, nil
}

// countWord spells small counts, so a document can say "four".
func countWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
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
