// Command mutants keeps the mutation-evidence table in spec/parity/PORT.md
// honest by measuring it instead of trusting it.
//
// The table answers the question "why should a passing harness be believed?" -
// it records how many cases a deliberately broken reference build gets right.
// A row near the corpus total would mean the corpus is nearly blind to that
// bug. The numerators are the only part of the table that cannot be derived
// from the corpora, so tools/check-docs (which owns the denominators) leaves
// them alone, and for a long time they were pure transcription.
//
// This tool removes the transcription. It copies the buildable module into a
// temp directory - the repository working tree is never touched - applies one
// exact textual mutation, builds, replays both corpora, and prints or verifies
// the table rows. `-write` regenerates the governed region of PORT.md;
// `-check` re-measures and fails if the committed region disagrees.
//
// Two deliberate choices:
//
//   - An anchor that does not match exactly once is an error, never a no-op. A
//     silently-unapplied mutation would score a perfect run and be reported
//     as "the corpus catches this", which is the one wrong answer that would
//     make this tool worse than the hand-written table it replaces.
//   - The reference build is staged the same way as the mutants, so the
//     "unmutated reference" row proves the staging itself is faithful.
//
// Unlike corpus-check this does not use `git diff`; it compares a fresh
// measurement against the committed bytes, so it fails only for actual
// staleness and never because of unrelated uncommitted work.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	governedStart = "The corpus was mutation-tested against "
	tableHeader   = "| Mutation | Base corpus | Edge corpus |"
	edgeCorpus    = "spec/parity/corpus-edge.json"
	docPath       = "spec/parity/PORT.md"
)

// mutation is one deliberately broken reference build.
type mutation struct {
	name  string // short id, used in logs and failures
	label string // the table's first cell, verbatim
	file  string // repository-relative source file to edit
	old   string // anchor; must occur exactly once
	new   string // replacement
}

// The bugs the corpus was designed to catch - five when this list was last
// measured, and the table in spec/parity/PORT.md is the authority on how many.
// Each is a single-behavior change a reasonable implementer could ship by
// accident, and all of them are invisible to a naive reading of the spec.
var mutations = []mutation{
	{
		name:  "html",
		label: "`SetEscapeHTML(false)` in the JSON result encoder",
		file:  "internal/cli/cli.go",
		old:   "\t\tencoded, _ := json.Marshal(data)\n\t\tfmt.Fprintln(out, string(encoded))",
		new:   "\t\tencoder := json.NewEncoder(out)\n\t\tencoder.SetEscapeHTML(false)\n\t\t_ = encoder.Encode(data)",
	},
	{
		name:  "key",
		label: "`next_action` renamed to `nextAction`",
		file:  "internal/cli/cli.go",
		old:   "\tNextAction string   `json:\"next_action\"`",
		new:   "\tNextAction string   `json:\"nextAction\"`",
	},
	{
		name:  "newline",
		label: "final newline always appended to the hashed evidence range",
		file:  "internal/evidence/evidence.go",
		old:   "\tif lineEnd > lineStart || firstLineNonEmpty {\n\t\thasher.Write(newline)\n\t}",
		new:   "\t_ = firstLineNonEmpty\n\thasher.Write(newline)",
	},
	{
		name:  "trailcr",
		label: "`\\r` stripped without its `\\n` in the relocation path",
		file:  "internal/evidence/relocate.go",
		old: "\tif end > begin && data[end-1] == '\\n' {\n" +
			"\t\tend--\n" +
			"\t\tif end > begin && data[end-1] == '\\r' {\n" +
			"\t\t\tend--\n" +
			"\t\t}\n" +
			"\t}",
		new: "\tif end > begin && data[end-1] == '\\n' {\n" +
			"\t\tend--\n" +
			"\t}\n" +
			"\tif end > begin && data[end-1] == '\\r' {\n" +
			"\t\tend--\n" +
			"\t}",
	},
	{
		// This row came from a blind-spot hunt rather than from documenting
		// coverage that already existed: with it applied, both corpora used to
		// score perfectly. The uppercase- and prefixed-hash cases in the base
		// corpus exist because of it. Because the row stays in this list,
		// deleting those cases again makes mutants-check fail instead of
		// quietly weakening the corpus.
		name:  "hashcase",
		label: "`strings.ToLower` dropped from the expected-hash comparison",
		file:  "internal/evidence/evidence.go",
		old:   "\treturn strings.ToLower(hexValue), nil",
		new:   "\treturn hexValue, nil",
	},
}

// row is one measured line of the table.
type row struct {
	label string
	base  int
	edge  int
}

type result struct {
	baseTotal int
	edgeTotal int
	rows      []row
}

func main() {
	write := false
	check := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-write":
			write = true
		case "-check":
			check = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: go run ./tools/mutants [-write|-check]")
			return
		default:
			fmt.Fprintf(os.Stderr, "mutants: unknown argument %q\n", arg)
			os.Exit(2)
		}
	}
	if write && check {
		fmt.Fprintln(os.Stderr, "mutants: -write and -check are mutually exclusive")
		os.Exit(2)
	}

	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutants:", err)
		os.Exit(2)
	}

	res, err := measure(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutants:", err)
		os.Exit(2)
	}
	region := renderRegion(res)

	switch {
	case write:
		if err := writeRegion(filepath.Join(root, docPath), region); err != nil {
			fmt.Fprintln(os.Stderr, "mutants:", err)
			os.Exit(1)
		}
		fmt.Printf("mutants: rewrote the table in %s from a fresh measurement\n", docPath)
	case check:
		changed, err := checkRegion(filepath.Join(root, docPath), region)
		if err != nil {
			fmt.Fprintln(os.Stderr, "mutants:", err)
			os.Exit(2)
		}
		if changed {
			fmt.Fprintf(os.Stderr, "mutants: %s is stale; run `make mutants` and commit the result\n", docPath)
			os.Exit(1)
		}
		fmt.Printf("mutants: %s matches a fresh measurement of %d mutations\n", docPath, len(mutations))
	default:
		fmt.Print(region)
	}
}

// measure stages the module, mutates it once per mutation, and replays both
// corpora against every build.
func measure(root string) (result, error) {
	var res result

	base, err := stage(root)
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(base)

	bin, err := build(base)
	if err != nil {
		return res, fmt.Errorf("unmutated reference: %w", err)
	}
	basePassed, baseTotal, edgePassed, edgeTotal, err := replay(root, bin)
	if err != nil {
		return res, fmt.Errorf("unmutated reference: %w", err)
	}
	res.baseTotal, res.edgeTotal = baseTotal, edgeTotal
	res.rows = append(res.rows, row{"unmutated reference", basePassed, edgePassed})

	for _, m := range mutations {
		dir, err := stage(root)
		if err != nil {
			return res, err
		}
		if err := applyMutation(filepath.Join(dir, m.file), m); err != nil {
			os.RemoveAll(dir)
			return res, fmt.Errorf("mutation %s: %w", m.name, err)
		}
		bin, err := build(dir)
		if err != nil {
			os.RemoveAll(dir)
			return res, fmt.Errorf("mutation %s: %w", m.name, err)
		}
		b, _, e, _, err := replay(root, bin)
		os.RemoveAll(dir)
		if err != nil {
			return res, fmt.Errorf("mutation %s: %w", m.name, err)
		}
		if b == baseTotal && e == edgeTotal {
			// Not fatal: a mutation the corpus cannot see is a finding worth
			// recording, and the row will show it plainly.
			fmt.Fprintf(os.Stderr,
				"mutants: warning: mutation %s is invisible to both corpora (%d/%d, %d/%d)\n",
				m.name, b, baseTotal, e, edgeTotal)
		}
		res.rows = append(res.rows, row{m.label, b, e})
	}
	return res, nil
}

// applyMutation replaces m.old with m.new, requiring exactly one occurrence.
func applyMutation(path string, m mutation) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(raw)
	switch n := strings.Count(text, m.old); {
	case n == 0:
		return fmt.Errorf("anchor not found in %s (the source moved; update tools/mutants)", m.file)
	case n > 1:
		return fmt.Errorf("anchor occurs %d times in %s; it must be unique", n, m.file)
	}
	return os.WriteFile(path, []byte(strings.Replace(text, m.old, m.new, 1)), 0o644)
}

// stage copies the buildable module into a fresh temp directory.
func stage(root string) (string, error) {
	dir, err := os.MkdirTemp("", "ownscout-mutants-")
	if err != nil {
		return "", err
	}
	for _, entry := range []string{"go.mod", "go.sum", "cmd", "internal"} {
		src := filepath.Join(root, entry)
		info, err := os.Stat(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		dst := filepath.Join(dir, entry)
		if info.IsDir() {
			if err := copyTree(src, dst); err != nil {
				os.RemoveAll(dir)
				return "", err
			}
			continue
		}
		if err := copyFile(src, dst); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

func build(dir string) (string, error) {
	bin := filepath.Join(dir, "ownscout")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/ownscout")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return bin, nil
}

// replay runs one corpus and reports the number of passing cases.
//
// --verbose makes the harness print one "ok   <name>" line per passing case.
// Counting those lines is exact; counting the mismatch blocks is not, because a
// single failing case can emit several.
func replay(root, bin string) (base, baseTotal, edge, edgeTotal int, err error) {
	base, baseTotal, err = replayCorpus(root, bin, "")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	edge, edgeTotal, err = replayCorpus(root, bin, filepath.Join(root, edgeCorpus))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return base, baseTotal, edge, edgeTotal, nil
}

func replayCorpus(root, bin, corpus string) (passed, total int, err error) {
	args := []string{"spec/parity/harness.py", "--bin", bin, "--verbose"}
	if corpus != "" {
		args = append(args, "--corpus", corpus)
	}
	cmd := exec.Command("python3", args...)
	cmd.Dir = root
	out, _ := cmd.Output() // a nonzero exit is expected: mutations must fail cases
	passed = 0
	total = -1
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ok   ") {
			passed++
			continue
		}
		if strings.HasPrefix(line, "harness: ") {
			if n, err := parseSummary(line); err == nil {
				total = n
			}
		}
	}
	if total < 0 {
		return 0, 0, fmt.Errorf("harness produced no summary line for %s", corpusLabel(corpus))
	}
	return passed, total, nil
}

func corpusLabel(corpus string) string {
	if corpus == "" {
		return "the base corpus"
	}
	return filepath.Base(corpus)
}

// parseSummary reads "harness: 12/31 cases passed".
func parseSummary(line string) (int, error) {
	fields := strings.Fields(strings.TrimPrefix(line, "harness: "))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty summary")
	}
	parts := strings.SplitN(fields[0], "/", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("malformed case count %q", fields[0])
	}
	return strconv.Atoi(parts[1])
}

// renderRegion builds the governed text: the intro sentence, the table, and
// nothing else. Everything else in that section is hand-written prose and stays
// that way.
func renderRegion(res result) string {
	if len(res.rows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s deliberately broken reference builds:\n\n", governedStart, countWord(len(mutations)))
	// The measured rows are emitted in mutation order, then the reference,
	// matching the order a reader expects: bugs first, baseline last.
	rows := append([]row{}, res.rows[1:]...)
	rows = append(rows, res.rows[0])
	b.WriteString(tableHeader + "\n")
	b.WriteString("|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d/%d | %d/%d |\n", r.label, r.base, res.baseTotal, r.edge, res.edgeTotal)
	}
	return b.String()
}

// countWord spells small counts, so the sentence reads "five ... builds".
func countWord(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// region bounds in PORT.md: the intro line and the contiguous table under it.
func findRegion(doc string) (start, end int, err error) {
	lines := strings.Split(doc, "\n")
	start = -1
	for i, line := range lines {
		if strings.HasPrefix(line, governedStart) {
			if start >= 0 {
				return 0, 0, fmt.Errorf("%s: %q appears more than once", docPath, governedStart)
			}
			start = i
		}
	}
	if start < 0 {
		return 0, 0, fmt.Errorf("%s: no line beginning %q", docPath, governedStart)
	}
	end = -1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "|") {
			end = i
			continue
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return 0, 0, fmt.Errorf("%s: no table follows %q", docPath, governedStart)
	}
	return start, end, nil
}

func checkRegion(path, want string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := string(raw)
	start, end, err := findRegion(doc)
	if err != nil {
		return false, err
	}
	lines := strings.Split(doc, "\n")
	got := strings.Join(lines[start:end+1], "\n") + "\n"
	return got != want, nil
}

func writeRegion(path, want string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := string(raw)
	start, end, err := findRegion(doc)
	if err != nil {
		return err
	}
	lines := strings.Split(doc, "\n")
	next := append([]string{}, lines[:start]...)
	next = append(next, strings.Split(strings.TrimSuffix(want, "\n"), "\n")...)
	next = append(next, lines[end+1:]...)
	if err := os.WriteFile(path, []byte(strings.Join(next, "\n")), 0o644); err != nil {
		return err
	}
	return nil
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod, so the tool works from anywhere in the tree.
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
			return "", errors.New("no go.mod found above the working directory")
		}
		dir = parent
	}
}
