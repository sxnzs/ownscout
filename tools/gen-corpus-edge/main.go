// Command gen-corpus-edge records a wider set of edge cases for hardening the
// ports: every contract-testdata fixture, the subcommand surface, synthesized
// node-envelope graph failures, raw-JSON parse failures, a graph that fails
// evidence verification, and an in-repository ledger rejection.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ownscout/internal/node"
	"ownscout/internal/nodepacket"
)

// rawEvidenceHash hashes the selected normalized lines exactly as the
// reference does: joined with "\n", plus a final "\n" for non-empty content.
// The line strings are raw bytes, so invalid UTF-8 survives unchanged.
func rawEvidenceHash(lines []string) string {
	selected := strings.Join(lines, "\n")
	if selected != "" {
		selected += "\n"
	}
	sum := sha256.Sum256([]byte(selected))
	return hex.EncodeToString(sum[:])
}

type spec struct {
	name string
	args []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-corpus-edge:", err)
		os.Exit(1)
	}
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
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func bindingFor(data []byte) (string, string, error) {
	packet, violations := nodepacket.DecodeValid(data)
	if len(violations) != 0 {
		return "", "", fmt.Errorf("packet violations: %v", violations)
	}
	binding, err := node.CanonicalPacketBinding(packet)
	return packet.PacketID, binding, err
}

func baseEnvelope(packetID, binding string) map[string]any {
	return map[string]any{
		"schema_version":        "node-envelope-v1",
		"envelope_id":           "envelope-1",
		"packet_id":             packetID,
		"packet_binding_sha256": binding,
		"nodes": []any{
			map[string]any{"node_id": "a", "depends_on": []any{}, "verifier": "evidence.current", "evidence_ids": []any{"evidence-1"}},
			map[string]any{"node_id": "b", "depends_on": []any{"a"}, "verifier": "evidence.current", "evidence_ids": []any{"evidence-1"}},
		},
	}
}

func deepCopy(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	copied := map[string]any{}
	_ = json.Unmarshal(data, &copied)
	return copied
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "spec", "parity")
	fixtures := filepath.Join(dir, "fixtures")
	edge := filepath.Join(fixtures, "edge")
	if err := os.MkdirAll(edge, 0o755); err != nil {
		return err
	}

	contracts, err := filepath.Glob(filepath.Join(root, "internal", "contract", "testdata", "*.json"))
	if err != nil {
		return err
	}
	contractNames := []string{}
	for _, source := range contracts {
		name := strings.TrimSuffix(filepath.Base(source), ".json")
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(edge, "contract-"+name+".json"), data, 0o644); err != nil {
			return err
		}
		contractNames = append(contractNames, name)
	}

	packetValid, err := os.ReadFile(filepath.Join(fixtures, "packet-valid.json"))
	if err != nil {
		return err
	}
	packetStale, err := os.ReadFile(filepath.Join(fixtures, "packet-stale.json"))
	if err != nil {
		return err
	}
	packetID, binding, err := bindingFor(packetValid)
	if err != nil {
		return err
	}

	cases := []spec{}
	for _, name := range contractNames {
		packet := "fixtures/edge/contract-" + name + ".json"
		cases = append(cases,
			spec{"contract-edge-" + name + "-human", []string{"contract", "validate", "--packet", packet}},
			spec{"contract-edge-" + name + "-json", []string{"contract", "validate", "--packet", packet, "--json"}},
		)
	}

	cases = append(cases,
		spec{"contract-no-subcommand", []string{"contract"}},
		spec{"contract-unknown-subcommand", []string{"contract", "bogus"}},
		spec{"contract-validate-no-value", []string{"contract", "validate", "--packet"}},
		spec{"contract-validate-extra-arg", []string{"contract", "validate", "--packet", "fixtures/edge/contract-valid.json", "extra"}},
		spec{"evidence-no-subcommand", []string{"evidence"}},
		spec{"evidence-unknown-subcommand", []string{"evidence", "bogus"}},
		spec{"evidence-missing-packet", []string{"evidence", "verify", "--repo", "fixtures/repo"}},
		spec{"node-no-subcommand", []string{"node"}},
		spec{"node-unknown-subcommand", []string{"node", "bogus"}},
	)

	validEnvelope := baseEnvelope(packetID, binding)
	mutations := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"duplicate-id", func(env map[string]any) {
			env["nodes"].([]any)[1].(map[string]any)["node_id"] = "a"
		}},
		{"missing-dependency", func(env map[string]any) {
			env["nodes"].([]any)[1].(map[string]any)["depends_on"] = []any{"absent"}
		}},
		{"self-dependency", func(env map[string]any) {
			env["nodes"].([]any)[0].(map[string]any)["depends_on"] = []any{"a"}
		}},
		{"cycle", func(env map[string]any) {
			env["nodes"].([]any)[0].(map[string]any)["depends_on"] = []any{"b"}
		}},
		{"empty-nodes", func(env map[string]any) { env["nodes"] = []any{} }},
		{"unknown-verifier", func(env map[string]any) {
			env["nodes"].([]any)[0].(map[string]any)["verifier"] = "git.current"
		}},
		{"unknown-evidence", func(env map[string]any) {
			env["nodes"].([]any)[0].(map[string]any)["evidence_ids"] = []any{"evidence-999"}
		}},
		{"wrong-binding", func(env map[string]any) { env["packet_binding_sha256"] = strings.Repeat("0", 64) }},
		{"wrong-schema", func(env map[string]any) { env["schema_version"] = "node-envelope-v2" }},
		{"wrong-packet-id", func(env map[string]any) { env["packet_id"] = "packet-999" }},
		{"nodes-not-array", func(env map[string]any) { env["nodes"] = "nope" }},
		{"null-node-id", func(env map[string]any) { env["nodes"].([]any)[0].(map[string]any)["node_id"] = nil }},
		// Identifiers are validated: 1-128 bytes, alphanumeric with "._-:"
		// allowed after the first character.
		{"identifier-bad-char", func(env map[string]any) { env["envelope_id"] = "env%lope-1" }},
		{"identifier-leading", func(env map[string]any) { env["envelope_id"] = "-env-1" }},
		{"identifier-too-long", func(env map[string]any) { env["envelope_id"] = strings.Repeat("e", 129) }},
		{"identifier-node-id", func(env map[string]any) {
			env["nodes"].([]any)[0].(map[string]any)["node_id"] = "no%de"
		}},
		{"unknown-field", func(env map[string]any) { env["extra"] = true }},
		{"duplicate-key", func(env map[string]any) {}},
		{"trailing-json", func(env map[string]any) {}},
	}
	for _, mutation := range mutations {
		env := deepCopy(validEnvelope)
		mutation.mutate(env)
		data, err := json.Marshal(env)
		if err != nil {
			return err
		}
		text := string(data)
		if mutation.name == "duplicate-key" {
			text = strings.Replace(text, "{", "{\"envelope_id\":\"dup\",", 1)
		}
		if mutation.name == "trailing-json" {
			text += "{}"
		}
		path := filepath.Join(edge, "envelope-"+mutation.name+".json")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
		args := []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
			"--envelope", "fixtures/edge/envelope-" + mutation.name + ".json", "--ledger", "ledger.jsonl"}
		cases = append(cases,
			spec{"node-edge-" + mutation.name + "-human", args},
			spec{"node-edge-" + mutation.name + "-json", append(append([]string{}, args...), "--json")},
		)
	}

	packetBase := map[string]any{}
	if err := json.Unmarshal(packetValid, &packetBase); err != nil {
		return err
	}
	evidenceVariants := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"escape", func(p map[string]any) {
			p["evidence"].([]any)[0].(map[string]any)["path"] = "../outside.txt"
		}},
		{"missing", func(p map[string]any) {
			p["evidence"].([]any)[0].(map[string]any)["path"] = "absent.txt"
		}},
		{"range", func(p map[string]any) {
			item := p["evidence"].([]any)[0].(map[string]any)
			item["line_start"] = 9000
			item["line_end"] = 9001
		}},
		{"null-outcome", func(p map[string]any) { p["outcome"] = nil }},
		{"evidence-object", func(p map[string]any) { p["evidence"] = map[string]any{} }},
	}
	for _, variant := range evidenceVariants {
		payload := deepCopy(packetBase)
		variant.mutate(payload)
		if err := writeJSON(filepath.Join(edge, "packet-"+variant.name+".json"), payload); err != nil {
			return err
		}
		args := []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/edge/packet-" + variant.name + ".json"}
		cases = append(cases,
			spec{"evidence-edge-" + variant.name + "-human", args},
			spec{"evidence-edge-" + variant.name + "-json", append(append([]string{}, args...), "--json")},
		)
	}

	nonUTF8 := bytes.Replace(packetValid, []byte("\"packet-001\""), []byte("\"packet-\xff01\""), 1)
	if err := os.WriteFile(filepath.Join(edge, "packet-nonutf8.json"), nonUTF8, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(edge, "ledger-garbage.jsonl"), []byte("not a ledger record\n"), 0o644); err != nil {
		return err
	}

	// Evidence shapes the ports must reproduce byte-for-byte: hashing operates
	// on raw file bytes (not lossily decoded text), CRLF is normalized only at
	// line boundaries, a single empty line hashes the empty string, and
	// evidence files have no size limit. binary.txt pairs invalid UTF-8 with
	// CRLF terminators and an unterminated tail; blank.txt isolates the
	// single-empty-line rule; oversize.txt crosses 1 MiB.
	repoDir := filepath.Join(fixtures, "repo")
	binaryContent := "ascii line\n" + "binary \xff\xfe line\r\n" + "utf8 héllo 🎉\r\n" + "tail \x80"
	if err := os.WriteFile(filepath.Join(repoDir, "binary.txt"), []byte(binaryContent), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoDir, "blank.txt"), []byte("a\n\nb\n"), 0o644); err != nil {
		return err
	}
	oversize := strings.Builder{}
	oversizeLines := []string{}
	for oversize.Len() <= (1 << 20) {
		oversizeLines = append(oversizeLines, fmt.Sprintf("oversize line %06d: %s", len(oversizeLines)+1, strings.Repeat("0123456789abcdef", 4)))
		oversize.WriteString(oversizeLines[len(oversizeLines)-1] + "\n")
	}
	if err := os.WriteFile(filepath.Join(repoDir, "oversize.txt"), []byte(oversize.String()), 0o644); err != nil {
		return err
	}
	evidenceShapes := []struct {
		name      string
		path      string
		lines     []string
		lineStart int
		lineEnd   int
	}{
		{"binary", "binary.txt", []string{"ascii line", "binary \xff\xfe line", "utf8 héllo 🎉", "tail \x80"}, 1, 4},
		{"binary-range", "binary.txt", nil, 9000, 9001},
		{"blank-single", "blank.txt", []string{""}, 2, 2},
		{"blank-pair", "blank.txt", []string{"", "b"}, 2, 3},
		{"oversize", "oversize.txt", oversizeLines[:2], 1, 2},
	}
	for _, shape := range evidenceShapes {
		payload := deepCopy(packetBase)
		item := payload["evidence"].([]any)[0].(map[string]any)
		item["path"] = shape.path
		item["content_hash"] = "sha256:" + strings.Repeat("ab", 32)
		item["line_start"] = shape.lineStart
		item["line_end"] = shape.lineEnd
		if shape.lines != nil {
			item["content_hash"] = "sha256:" + rawEvidenceHash(shape.lines)
		}
		if err := writeJSON(filepath.Join(edge, "packet-evidence-shape-"+shape.name+".json"), payload); err != nil {
			return err
		}
		args := []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/edge/packet-evidence-shape-" + shape.name + ".json"}
		cases = append(cases,
			spec{"evidence-edge-shape-"+shape.name+"-human", args},
			spec{"evidence-edge-shape-"+shape.name+"-json", append(append([]string{}, args...), "--json")},
		)
	}
	// Anchor re-resolution (evidence verify --relocate). drift.txt records a
	// fingerprint that belongs three lines below the cited span, so the failure
	// can name where the content went; shrink.txt cites a range past the end of
	// a file that lost lines; oversize.txt is far too large to cover within the
	// relocation byte budget, so the search has to admit that it stopped rather
	// than claim the content is absent.
	driftLines := []string{"one", "two", "three", "four", "five", "six"}
	if err := os.WriteFile(filepath.Join(repoDir, "drift.txt"), []byte(strings.Join(driftLines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	shrinkLines := []string{"kept", "alpha", "beta"}
	if err := os.WriteFile(filepath.Join(repoDir, "shrink.txt"), []byte(strings.Join(shrinkLines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	// A final line with no terminator that ends in "\r": the "\r" is content,
	// not a terminator, because there is no "\n" for it to terminate. A
	// relocation path that strips it would hash this window differently from
	// the verification path and claim a false location.
	if err := os.WriteFile(filepath.Join(repoDir, "trailcr.txt"), []byte("one\ntwo\nthree\r"), 0o644); err != nil {
		return err
	}
	relocations := []struct {
		name      string
		path      string
		lines     []string // recorded fingerprint; nil records an absent hash
		lineStart int
		lineEnd   int
	}{
		{"moved", "drift.txt", driftLines[3:5], 1, 2},
		{"gone", "drift.txt", nil, 1, 2},
		{"shrink", "shrink.txt", shrinkLines[1:3], 9000, 9001},
		{"budget", "oversize.txt", nil, 1, 20},
		{"trailcr", "trailcr.txt", []string{"three\r"}, 1, 1},
	}
	for _, relocation := range relocations {
		payload := deepCopy(packetBase)
		item := payload["evidence"].([]any)[0].(map[string]any)
		item["path"] = relocation.path
		item["content_hash"] = "sha256:" + strings.Repeat("ab", 32)
		item["line_start"] = relocation.lineStart
		item["line_end"] = relocation.lineEnd
		if relocation.lines != nil {
			item["content_hash"] = "sha256:" + rawEvidenceHash(relocation.lines)
		}
		file := "packet-evidence-relocate-" + relocation.name + ".json"
		if err := writeJSON(filepath.Join(edge, file), payload); err != nil {
			return err
		}
		base := []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/edge/" + file}
		cases = append(cases,
			spec{"evidence-edge-relocate-" + relocation.name + "-human", append(append([]string{}, base...), "--relocate")},
			spec{"evidence-edge-relocate-" + relocation.name + "-json", append(append([]string{}, base...), "--relocate", "--json")},
		)
	}
	cases = append(cases,
		// The same packet without the flag, so the flag is provably the only
		// difference between the two renderings.
		spec{"evidence-edge-relocate-off-human", []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/edge/packet-evidence-relocate-moved.json"}},
		// --relocate belongs to evidence verify alone.
		spec{"contract-edge-relocate-unknown-flag-human", []string{"contract", "validate", "--packet", "fixtures/packet-valid.json", "--relocate"}},
		spec{"contract-edge-relocate-unknown-flag-json", []string{"contract", "validate", "--packet", "fixtures/packet-valid.json", "--relocate", "--json"}},
	)
	// Unknown-field reporting. Two independent behaviours are pinned here: the
	// field name is quoted exactly as Go's %q quotes it (so a control byte is
	// \xNN, not a raw byte or a host-language escape, and a printable non-ASCII
	// rune is kept), and the error reported is the first one in document order,
	// including inside a nested object. Go marshals map keys in sorted order, so
	// the unknown field's name alone decides whether it precedes or follows the
	// badly typed field.
	unknownNames := []struct {
		name string
		key  string
	}{
		{"specials", "a\"b\\c\td\ne\x7ff"},
		{"controls", "x\x01y\x1fz\rw"},
		{"nonascii", "héllo🎉\u00a0\u2028"},
		// A non-printable rune above U+FFFF, which Go's %q renders with the
		// eight-digit \U form rather than \u.
		{"astral", "astral\U0010FFFEend"},
	}
	for _, unknown := range unknownNames {
		payload := deepCopy(packetBase)
		payload[unknown.key] = 1
		file := "packet-edge-unknown-" + unknown.name + ".json"
		if err := writeJSON(filepath.Join(edge, file), payload); err != nil {
			return err
		}
		args := []string{"contract", "validate", "--packet", "fixtures/edge/" + file}
		cases = append(cases,
			spec{"contract-edge-unknown-" + unknown.name + "-human", args},
			spec{"contract-edge-unknown-" + unknown.name + "-json", append(append([]string{}, args...), "--json")},
		)
	}
	for _, order := range []struct {
		name  string
		field string
	}{
		{"unknown-first", "aaa_unknown"},
		{"unknown-last", "zzz_unknown"},
	} {
		payload := deepCopy(packetBase)
		payload["budget"] = map[string]any{
			"max_evidence":  "notanint",
			"used_evidence": 1,
			"max_bytes":     1000,
			"used_bytes":    12,
			order.field:     1,
		}
		file := "packet-edge-precedence-" + order.name + ".json"
		if err := writeJSON(filepath.Join(edge, file), payload); err != nil {
			return err
		}
		args := []string{"contract", "validate", "--packet", "fixtures/edge/" + file}
		cases = append(cases,
			spec{"contract-edge-precedence-" + order.name + "-human", args},
			spec{"contract-edge-precedence-" + order.name + "-json", append(append([]string{}, args...), "--json")},
		)
	}
	// Three more decoding behaviours the reference inherits from encoding/json
	// and the ports got wrong. A struct field matches its name case-insensitively
	// when no exact match exists; an invalid UTF-8 byte inside a field name
	// becomes its own U+FFFD; and a null element of the evidence array decodes to
	// the zero-valued struct, so the numeric rules still fire on it.
	caseTop := deepCopy(packetBase)
	caseTop["SCHEMA_VERSION"] = caseTop["schema_version"]
	delete(caseTop, "schema_version")
	if err := writeJSON(filepath.Join(edge, "packet-edge-case-top.json"), caseTop); err != nil {
		return err
	}
	caseNested := deepCopy(packetBase)
	firstEvidence := caseNested["evidence"].([]any)[0].(map[string]any)
	firstEvidence["LINE_START"] = firstEvidence["line_start"]
	delete(firstEvidence, "line_start")
	if err := writeJSON(filepath.Join(edge, "packet-edge-case-nested.json"), caseNested); err != nil {
		return err
	}
	nullElement := deepCopy(packetBase)
	nullElement["evidence"] = []any{nil}
	if err := writeJSON(filepath.Join(edge, "packet-edge-null-evidence-element.json"), nullElement); err != nil {
		return err
	}
	// writeJSON marshals a Go string, which would replace the invalid bytes with
	// U+FFFD; the fixture has to be written raw. "\xf0\x9f" is a truncated
	// four-byte sequence, so the reference emits one U+FFFD per byte.
	encoded, err := json.Marshal(packetBase)
	if err != nil {
		return err
	}
	rawUTF8 := append([]byte("{\"\xf0\x9f\":1,"), encoded[1:]...)
	if err := os.WriteFile(filepath.Join(edge, "packet-edge-unknown-badutf8.json"), rawUTF8, 0o644); err != nil {
		return err
	}
	for _, extra := range []string{"case-top", "case-nested", "null-evidence-element", "unknown-badutf8"} {
		args := []string{"contract", "validate", "--packet", "fixtures/edge/packet-edge-" + extra + ".json"}
		cases = append(cases,
			spec{"contract-edge-" + extra + "-human", args},
			spec{"contract-edge-" + extra + "-json", append(append([]string{}, args...), "--json")},
		)
	}
	// The reference has two packet decoders that disagree on case: the contract
	// and evidence path decodes with encoding/json (case-fold fallback), while
	// node verify uses the strict node packet decoder, which is exact-only. A
	// port that shares one decoder must not make the strict one case-insensitive.
	nodeCase := []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/edge/packet-edge-case-top.json",
		"--envelope", "fixtures/envelope-valid.json"}
	cases = append(cases,
		spec{"node-edge-case-exact-only-human", append(append([]string{}, nodeCase...), "--ledger", "ledger.jsonl")},
		spec{"node-edge-case-exact-only-json", append(append([]string{}, nodeCase...), "--ledger", "ledger.jsonl", "--json")},
	)
	cases = append(cases,
		// The reference checks node verify's four paths in a fixed order and
		// reports the first one missing, and its usage errors honour --json.
		// Neither was covered until a port was found diverging on both.
		spec{"node-edge-missing-flags-human", []string{"node", "verify"}},
		spec{"node-edge-missing-flags-json", []string{"node", "verify", "--json"}},
		spec{"node-edge-missing-flags-partial-json", []string{"node", "verify", "--json", "--repo", "fixtures/repo"}},
		spec{"node-edge-unknown-flag-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "ledger.jsonl", "--relocate", "--json"}},
	)
	cases = append(cases,
		spec{"contract-edge-nonutf8-human", []string{"contract", "validate", "--packet", "fixtures/edge/packet-nonutf8.json"}},
		spec{"contract-edge-nonutf8-json", []string{"contract", "validate", "--packet", "fixtures/edge/packet-nonutf8.json", "--json"}},
		spec{"evidence-edge-repo-is-file-human", []string{"evidence", "verify", "--repo", "fixtures/repo/notes.txt", "--packet", "fixtures/packet-valid.json"}},
		spec{"evidence-edge-repo-is-file-json", []string{"evidence", "verify", "--repo", "fixtures/repo/notes.txt", "--packet", "fixtures/packet-valid.json", "--json"}},
		spec{"node-edge-ledger-garbage-human", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/edge/ledger-garbage.jsonl"}},
		spec{"node-edge-ledger-garbage-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/edge/ledger-garbage.jsonl", "--json"}},
	)

	staleID, staleBinding, err := bindingFor(packetStale)
	if err != nil {
		return err
	}
	staleEnvelope := baseEnvelope(staleID, staleBinding)
	if err := writeJSON(filepath.Join(edge, "envelope-stale-evidence.json"), staleEnvelope); err != nil {
		return err
	}
	staleArgs := []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-stale.json",
		"--envelope", "fixtures/edge/envelope-stale-evidence.json"}
	cases = append(cases,
		spec{"node-edge-stale-evidence-human", append(append([]string{}, staleArgs...), "--ledger", "ledger.jsonl")},
		spec{"node-edge-stale-evidence-json", append(append([]string{}, staleArgs...), "--ledger", "ledger.jsonl", "--json")},
		spec{"node-edge-ledger-in-repo-human", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
			"--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/repo/ledger.jsonl"}},
		spec{"node-edge-ledger-in-repo-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
			"--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/repo/ledger.jsonl", "--json"}},
	)

	bin := filepath.Join(os.TempDir(), "ownscout-gen-corpus-edge")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ownscout")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build: %v: %s", err, out)
	}

	ledger := filepath.Join(dir, "ledger.jsonl")
	recorded := make([]map[string]any, 0, len(cases))
	for _, item := range cases {
		os.Remove(ledger)
		command := exec.Command(bin, item.args...)
		command.Dir = dir
		var out bytes.Buffer
		command.Stdout = &out
		command.Stderr = &out
		exitCode := 0
		if err := command.Run(); err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				return fmt.Errorf("%s: %v", item.name, err)
			}
			exitCode = exitErr.ExitCode()
		}
		entry := map[string]any{
			"name":     item.name,
			"args":     item.args,
			"exitCode": exitCode,
			"stdout":   normalize(out.String(), dir, repoDir, bin),
		}
		if data, err := os.ReadFile(ledger); err == nil {
			entry["files"] = map[string]any{"ledger.jsonl": normalize(string(data), dir, repoDir, bin)}
			os.Remove(ledger)
		}
		recorded = append(recorded, entry)
	}

	// Ledger append: the second record must chain to the first. The seed fixture
	// is restored after recording so every harness run starts from one record.
	seedPath := filepath.Join(edge, "ledger-seed.jsonl")
	os.Remove(seedPath)
	seedArgs := []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json",
		"--envelope", "fixtures/envelope-valid.json", "--ledger", "fixtures/edge/ledger-seed.jsonl"}
	seedRun := exec.Command(bin, seedArgs...)
	seedRun.Dir = dir
	if out, err := seedRun.CombinedOutput(); err != nil {
		return fmt.Errorf("ledger seed: %v: %s", err, out)
	}
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		return err
	}
	for _, mode := range []struct {
		name  string
		extra []string
	}{
		{"node-edge-ledger-append-human", nil},
		{"node-edge-ledger-append-json", []string{"--json"}},
	} {
		args := append(append([]string{}, seedArgs...), mode.extra...)
		command := exec.Command(bin, args...)
		command.Dir = dir
		var out bytes.Buffer
		command.Stdout = &out
		command.Stderr = &out
		exitCode := 0
		if err := command.Run(); err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				return fmt.Errorf("%s: %v", mode.name, err)
			}
			exitCode = exitErr.ExitCode()
		}
		content, err := os.ReadFile(seedPath)
		if err != nil {
			return err
		}
		recorded = append(recorded, map[string]any{
			"name":     mode.name,
			"args":     args,
			"exitCode": exitCode,
			"stdout":   normalize(out.String(), dir, repoDir, bin),
			"files": map[string]any{
				"fixtures/edge/ledger-seed.jsonl": normalize(string(content), dir, repoDir, bin),
			},
		})
		if err := os.WriteFile(seedPath, seed, 0o644); err != nil {
			return err
		}
	}

	return writeJSON(filepath.Join(dir, "corpus-edge.json"), map[string]any{
		"version": 1,
		"note":    "Edge cases recorded from the Go reference. Same placeholders as corpus.json.",
		"cases":   recorded,
	})
}

func normalize(text, dir, repoDir, bin string) string {
	text = strings.ReplaceAll(text, repoDir, "{{REPO}}")
	text = strings.ReplaceAll(text, dir, "{{DIR}}")
	text = strings.ReplaceAll(text, bin, "{{BIN}}")
	return text
}
