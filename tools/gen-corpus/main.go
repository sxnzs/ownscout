// Command gen-corpus records the OwnScout CLI's exact behavior over committed
// fixtures, so every language port can be checked for byte parity.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ownscout/internal/node"
	"ownscout/internal/nodepacket"
)

type spec struct {
	name string
	args []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-corpus:", err)
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
			return "", fmt.Errorf("go.mod not found above %s", dir)
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

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "spec", "parity")
	fixtures := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(filepath.Join(fixtures, "repo"), 0o755); err != nil {
		return err
	}

	packetValid, err := os.ReadFile(filepath.Join(root, "internal", "cli", "testdata", "valid.json"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fixtures, "packet-valid.json"), packetValid, 0o644); err != nil {
		return err
	}
	for _, source := range []string{"invalid.json", "malformed.json"} {
		data, err := os.ReadFile(filepath.Join(root, "internal", "cli", "testdata", source))
		if err != nil {
			return err
		}
		name := "packet-" + strings.TrimSuffix(source, ".json") + ".json"
		if err := os.WriteFile(filepath.Join(fixtures, name), data, 0o644); err != nil {
			return err
		}
	}
	notes, err := os.ReadFile(filepath.Join(root, "internal", "cli", "testdata", "repo", "notes.txt"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fixtures, "repo", "notes.txt"), notes, 0o644); err != nil {
		return err
	}

	packet, violations := nodepacket.DecodeValid(packetValid)
	if len(violations) != 0 {
		return fmt.Errorf("fixture packet violations: %v", violations)
	}
	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(fixtures, "envelope-valid.json"), map[string]any{
		"schema_version":        "node-envelope-v1",
		"envelope_id":           "envelope-1",
		"packet_id":             packet.PacketID,
		"packet_binding_sha256": binding,
		"nodes": []any{map[string]any{
			"node_id": "build", "depends_on": []any{}, "verifier": "evidence.current",
			"evidence_ids": []any{"evidence-1"},
		}},
	}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fixtures, "envelope-invalid.json"),
		[]byte("{\"unexpected_secret\":\"DO_NOT_PRINT\"}"), 0o644); err != nil {
		return err
	}

	stale := map[string]any{}
	if err := json.Unmarshal(packetValid, &stale); err != nil {
		return err
	}
	if evidence, ok := stale["evidence"].([]any); ok && len(evidence) > 0 {
		if first, ok := evidence[0].(map[string]any); ok {
			first["content_hash"] = strings.Repeat("0", 64)
		}
	}
	if err := writeJSON(filepath.Join(fixtures, "packet-stale.json"), stale); err != nil {
		return err
	}

	bin := filepath.Join(os.TempDir(), "ownscout-gen-corpus")
	build := exec.Command("go", "build", "-o", bin, "./cmd/ownscout")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build: %v: %s", err, out)
	}

	cases := []spec{
		{"help-root", []string{"--help"}},
		{"help-contract", []string{"contract", "validate", "--help"}},
		{"help-evidence", []string{"evidence", "verify", "--help"}},
		{"help-node", []string{"node", "verify", "--help"}},
		{"version", []string{"version"}},
		{"doctor", []string{"doctor"}},
		{"no-command", []string{}},
		{"unknown-command", []string{"nope"}},
		{"contract-valid-human", []string{"contract", "validate", "--packet", "fixtures/packet-valid.json"}},
		{"contract-valid-json", []string{"contract", "validate", "--packet", "fixtures/packet-valid.json", "--json"}},
		{"contract-invalid-human", []string{"contract", "validate", "--packet", "fixtures/packet-invalid.json"}},
		{"contract-invalid-json", []string{"contract", "validate", "--packet", "fixtures/packet-invalid.json", "--json"}},
		{"contract-malformed-human", []string{"contract", "validate", "--packet", "fixtures/packet-malformed.json"}},
		{"contract-malformed-json", []string{"contract", "validate", "--packet", "fixtures/packet-malformed.json", "--json"}},
		{"contract-missing-file", []string{"contract", "validate", "--packet", "fixtures/absent.json"}},
		{"contract-missing-flag", []string{"contract", "validate"}},
		{"evidence-valid-human", []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json"}},
		{"evidence-valid-json", []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--json"}},
		{"evidence-stale-human", []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-stale.json"}},
		{"evidence-stale-json", []string{"evidence", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-stale.json", "--json"}},
		{"evidence-missing-repo", []string{"evidence", "verify", "--repo", "fixtures/absent", "--packet", "fixtures/packet-valid.json"}},
		{"node-valid-human", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "ledger.jsonl"}},
		{"node-valid-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "ledger.jsonl", "--json"}},
		{"node-invalid-envelope-human", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-invalid.json", "--ledger", "ledger.jsonl"}},
		{"node-invalid-envelope-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-invalid.json", "--ledger", "ledger.jsonl", "--json"}},
		{"node-invalid-packet-human", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-invalid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "ledger.jsonl"}},
		{"node-invalid-packet-json", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-invalid.json", "--envelope", "fixtures/envelope-valid.json", "--ledger", "ledger.jsonl", "--json"}},
		{"node-missing-ledger", []string{"node", "verify", "--repo", "fixtures/repo", "--packet", "fixtures/packet-valid.json", "--envelope", "fixtures/envelope-valid.json"}},
	}

	ledger := filepath.Join(dir, "ledger.jsonl")
	repoDir := filepath.Join(dir, "fixtures", "repo")
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
			var exitErr *exec.ExitError
			if !errorsAs(err, &exitErr) {
				return fmt.Errorf("%s: %v", item.name, err)
			}
			exitCode = exitErr.ExitCode()
		}
		stdout := normalize(out.String(), dir, repoDir, bin)
		entry := map[string]any{
			"name":     item.name,
			"args":     item.args,
			"exitCode": exitCode,
			"stdout":   stdout,
		}
		if data, err := os.ReadFile(ledger); err == nil {
			entry["files"] = map[string]any{
				"ledger.jsonl": normalize(string(data), dir, repoDir, bin),
			}
			os.Remove(ledger)
		}
		recorded = append(recorded, entry)
	}
	return writeJSON(filepath.Join(dir, "corpus.json"), map[string]any{
		"version": 1,
		"note":    "Recorded from the Go reference. {{DIR}} is the spec/parity directory, {{REPO}} is its fixtures/repo child.",
		"cases":   recorded,
	})
}

func errorsAs(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func normalize(text, dir, repoDir, bin string) string {
	text = strings.ReplaceAll(text, repoDir, "{{REPO}}")
	text = strings.ReplaceAll(text, dir, "{{DIR}}")
	text = strings.ReplaceAll(text, bin, "{{BIN}}")
	return text
}
