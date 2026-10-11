package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
)

func runTest(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := Run(args, &out)
	return code, out.String()
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"contract", "validate", "--help"}, {"evidence", "verify", "--help"}, {"node", "verify", "--help"}} {
		code, output := runTest(t, args...)
		if code != 0 || !strings.Contains(output, "Usage:") {
			t.Fatalf("args %v: code=%d output=%q", args, code, output)
		}
	}
}

func TestVersion(t *testing.T) {
	original := version
	version = "9.9.9-test"
	t.Cleanup(func() { version = original })

	code, output := runTest(t, "version")
	if code != 0 || output != "ownscout 9.9.9-test\n" {
		t.Fatalf("code=%d output=%q", code, output)
	}
}

func TestDoctor(t *testing.T) {
	code, output := runTest(t, "doctor")
	if code != 0 || !strings.Contains(output, "OwnScout doctor: ok") || !strings.Contains(output, "local-only mode") {
		t.Fatalf("code=%d output=%q", code, output)
	}
}

// TestInvalidUsage pins the exact usage failures and help text, including
// branches the recorded trace corpus never executes (found with `make coverage`).
// Expected output is written out literally so the pins hold the contract rather
// than echo the code.
func TestInvalidUsage(t *testing.T) {
	emptyLedger := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(emptyLedger, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	usage := func(message, next string) string {
		return "error: " + message + "\nNext action: run '" + next + "'.\n"
	}
	usageJSON := func(message, next string) string {
		return `{"command":"usage","ok":false,"summary":"` + message + `","details":["usage: ` + next + `"],"next_action":"Run '` + next + `'."}` + "\n"
	}

	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"unknown command", []string{"nope"}, 2,
			usage("unknown command 'nope'", "ownscout --help")},
		{"contract validate missing packet", []string{"contract", "validate"}, 2,
			usage("missing required --packet <file>", "ownscout contract validate --help")},
		{"contract validate unknown flag", []string{"contract", "validate", "--packet", "x", "--wat"}, 2,
			usage("unknown flag or argument '--wat'", "ownscout contract validate --help")},
		{"evidence verify missing packet", []string{"evidence", "verify", "--repo", "x"}, 2,
			usage("both --repo <dir> and --packet <file> are required", "ownscout evidence verify --help")},
		{"doctor extra argument", []string{"doctor", "x"}, 2,
			usage("doctor does not accept arguments", "ownscout doctor --help")},
		{"version extra argument", []string{"version", "x"}, 2,
			usage("version does not accept arguments", "ownscout version --help")},
		{"doctor help", []string{"doctor", "--help"}, 0,
			"Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments.\n"},
		{"version help", []string{"version", "--help"}, 0,
			"Usage: ownscout version\n\nPrints the OwnScout version.\n"},
		{"node bind unknown flag", []string{"node", "bind", "--bogus"}, 2,
			usage("unknown flag or argument '--bogus'", "ownscout node bind --help")},
		{"evidence verify unknown flag", []string{"evidence", "verify", "--bogus"}, 2,
			usage("unknown flag or argument '--bogus'", "ownscout evidence verify --help")},
		{"ledger verify unknown flag", []string{"ledger", "verify", "--bogus"}, 2,
			usage("unknown flag or argument '--bogus'", "ownscout ledger verify --help")},
		{"ledger verify missing ledger", []string{"ledger", "verify"}, 2,
			usage("missing required --ledger value", "ownscout ledger verify --help")},
		{"ledger rotate unknown flag", []string{"ledger", "rotate", "--bogus"}, 2,
			usage("unknown flag or argument '--bogus'", "ownscout ledger rotate --help")},
		{"ledger rotate missing ledger", []string{"ledger", "rotate"}, 2,
			usage("missing required --ledger value", "ownscout ledger rotate --help")},
		{"ledger verify unknown flag as JSON", []string{"ledger", "verify", "--bogus", "--json"}, 2,
			usageJSON("unknown flag or argument '--bogus'", "ownscout ledger verify --help")},
		{"ledger rotate missing ledger as JSON", []string{"ledger", "rotate", "--json"}, 2,
			usageJSON("missing required --ledger value", "ownscout ledger rotate --help")},
		{"ledger verify empty ledger", []string{"ledger", "verify", "--ledger", emptyLedger}, 0,
			"OK: ledger is intact\n  - 0 record(s), tip none\nNext action: The ledger chain is intact.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, got := runTest(t, tc.args...)
			if code != tc.code || got != tc.want {
				t.Fatalf("args %v:\n code=%d want %d\n got  %q\n want %q", tc.args, code, tc.code, got, tc.want)
			}
		})
	}
}

func TestContractValidation(t *testing.T) {
	valid := filepath.Join("testdata", "valid.json")
	packet, violations, err := loadPacket(valid)
	if err != nil {
		t.Fatalf("canonical packet could not be loaded: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("canonical packet has violations: %v", violations)
	}
	evaluation := contract.EvaluatePacket(packet)
	if !evaluation.Valid || evaluation.Action != "autonomous_proceed" {
		t.Fatalf("canonical evaluation = %+v", evaluation)
	}

	code, output := runTest(t, "contract", "validate", "--packet", valid)
	if code != 0 || !strings.Contains(output, "packet is valid") {
		t.Fatalf("valid: code=%d output=%q", code, output)
	}
	invalid := filepath.Join("testdata", "invalid.json")
	code, output = runTest(t, "contract", "validate", "--packet", invalid)
	if code != 1 || !strings.Contains(output, "packet is invalid") {
		t.Fatalf("invalid: code=%d output=%q", code, output)
	}
	code, output = runTest(t, "contract", "validate", "--packet", filepath.Join("testdata", "malformed.json"))
	if code != 2 || !strings.Contains(output, "strict packet decoding failed") {
		t.Fatalf("malformed: code=%d output=%q", code, output)
	}
}

func TestEvidenceVerification(t *testing.T) {
	valid := filepath.Join("testdata", "valid.json")
	packet, _, err := loadPacket(valid)
	if err != nil {
		t.Fatalf("canonical packet could not be loaded: %v", err)
	}
	report, err := evidence.VerifyPacket(filepath.Join("testdata", "repo"), packet)
	if err != nil || !report.Ok {
		t.Fatalf("canonical evidence report = %+v, err=%v", report, err)
	}

	code, output := runTest(t, "evidence", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", valid)
	if code != 0 || !strings.Contains(output, "evidence verified") {
		t.Fatalf("code=%d output=%q", code, output)
	}
}

func TestUnknownJSONFieldRejected(t *testing.T) {
	packetPath := writePacketVariant(t, func(raw map[string]json.RawMessage) {
		raw["unexpected_secret"] = json.RawMessage(`"DO_NOT_PRINT"`)
	})
	code, output := runTest(t, "contract", "validate", "--packet", packetPath)
	if code != 2 || !strings.Contains(output, "strict packet decoding failed") {
		t.Fatalf("code=%d output=%q", code, output)
	}
	if strings.Contains(output, "DO_NOT_PRINT") || strings.Contains(output, "packet-1") {
		t.Fatalf("packet contents leaked in output: %q", output)
	}
}

func TestEvidenceVerificationFailureExitCode(t *testing.T) {
	packetPath := writePacketVariant(t, func(raw map[string]json.RawMessage) {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(raw["evidence"], &items); err != nil {
			t.Fatalf("decode evidence: %v", err)
		}
		items[0]["content_hash"] = json.RawMessage(`"0000000000000000000000000000000000000000000000000000000000000000"`)
		raw["evidence"], _ = json.Marshal(items)
	})
	code, output := runTest(t, "evidence", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", packetPath)
	if code != 1 || !strings.Contains(output, "evidence verification failed") {
		t.Fatalf("code=%d output=%q", code, output)
	}
}

func TestJSONOutputContract(t *testing.T) {
	code, output := runTest(t, "contract", "validate", "--packet", filepath.Join("testdata", "valid.json"), "--json")
	if code != 0 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	for _, key := range []string{"command", "ok", "summary", "details", "next_action"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
	if result["ok"] != true || result["command"] != "contract validate" {
		t.Errorf("unexpected result: %#v", result)
	}
}

func writePacketVariant(t *testing.T, mutate func(map[string]json.RawMessage)) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatalf("read valid packet: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode valid packet: %v", err)
	}
	mutate(raw)
	data, err = json.Marshal(raw)
	if err != nil {
		t.Fatalf("encode packet variant: %v", err)
	}
	path := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write packet variant: %v", err)
	}
	return path
}
