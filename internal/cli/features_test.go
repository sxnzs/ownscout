package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/ledger"
	"ownscout/internal/node"
	"ownscout/internal/nodepacket"
)

func TestNodeBindUsesStrictPacketBoundary(t *testing.T) {
	packetPath := filepath.Join("testdata", "valid.json")
	packetBytes, err := os.ReadFile(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	packet, violations := nodepacket.DecodeValid(packetBytes)
	if len(violations) != 0 {
		t.Fatalf("fixture packet violations: %v", violations)
	}
	want, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		t.Fatal(err)
	}

	code, output := runTest(t, "node", "bind", "--packet", packetPath, "--json")
	if code != 0 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	if !result.OK || result.Command != "node bind" || result.Summary != "packet binding computed" || len(result.Details) != 1 || result.Details[0] != want {
		t.Fatalf("unexpected bind result: %+v", result)
	}
	if result.NextAction != "Use this as packet_binding_sha256 in a node-envelope-v1 document." {
		t.Fatalf("unexpected next action: %q", result.NextAction)
	}
}

func TestNodeBindDecodeAndContractFailures(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(malformed, []byte(`{"packet_id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output := runTest(t, "node", "bind", "--packet", malformed, "--json")
	if code != 2 || !strings.Contains(output, `"command":"node bind"`) || !strings.Contains(output, "packet could not be decoded") {
		t.Fatalf("decode failure: code=%d output=%q", code, output)
	}

	invalid := writePacketVariant(t, func(raw map[string]json.RawMessage) {
		raw["outcome"] = json.RawMessage(`"impossible"`)
	})
	code, output = runTest(t, "node", "bind", "--packet", invalid, "--json")
	if code != 1 {
		t.Fatalf("contract failure code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	if result.OK || result.Command != "node bind" || len(result.Details) == 0 || !strings.Contains(strings.Join(result.Details, "\n"), "outcome") {
		t.Fatalf("unexpected contract result: %+v", result)
	}
}

func TestLedgerVerifyCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	store, err := ledger.Open(path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Append(strings.Repeat("a", 64), strings.Repeat("b", 64), "0.1.0", []ledger.NodeResult{{NodeID: "build", Status: "evidence_current"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	code, output := runTest(t, "ledger", "verify", "--ledger", path, "--json")
	if code != 0 {
		t.Fatalf("valid ledger: code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	wantDetail := "1 record(s), tip " + record.RecordHash
	if !result.OK || result.Command != "ledger verify" || len(result.Details) != 1 || result.Details[0] != wantDetail || result.NextAction != "The ledger chain is intact." {
		t.Fatalf("unexpected ledger result: %+v", result)
	}

	invalid := filepath.Join(t.TempDir(), "invalid.jsonl")
	if err := os.WriteFile(invalid, []byte("not a ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output = runTest(t, "ledger", "verify", "--ledger", invalid, "--json")
	if code != 1 || !strings.Contains(output, "validate ledger") || !strings.Contains(output, "malformed") {
		t.Fatalf("invalid ledger: code=%d output=%q", code, output)
	}

	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	code, output = runTest(t, "ledger", "verify", "--ledger", missing, "--json")
	if code != 2 || !strings.Contains(output, "ledger could not be read") {
		t.Fatalf("missing ledger: code=%d output=%q", code, output)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing ledger was created: %v", err)
	}
}

func TestNodeVerifyRelocateAddsEvidenceIssuesOnlyWhenRequested(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("prefix\nalpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	packetPath := writePacketVariant(t, func(raw map[string]json.RawMessage) {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(raw["evidence"], &items); err != nil {
			t.Fatal(err)
		}
		items[0]["line_start"] = json.RawMessage("1")
		items[0]["line_end"] = json.RawMessage("2")
		raw["evidence"], _ = json.Marshal(items)
	})
	packetBytes, err := os.ReadFile(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	packet, violations := nodepacket.DecodeValid(packetBytes)
	if len(violations) != 0 {
		t.Fatalf("fixture packet violations: %v", violations)
	}
	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		t.Fatal(err)
	}
	envelopePath := writeEnvelope(t, node.Envelope{
		SchemaVersion:       node.SchemaVersionV1,
		EnvelopeID:          "envelope-relocate",
		PacketID:            packet.PacketID,
		PacketBindingSHA256: binding,
		Nodes:               []node.Node{{NodeID: "build", DependsOn: []string{}, Verifier: node.VerifierCurrent, EvidenceIDs: []string{"evidence-1"}}},
	})
	base := []string{"node", "verify", "--repo", repo, "--packet", packetPath, "--envelope", envelopePath}
	plainLedger := filepath.Join(t.TempDir(), "plain.jsonl")
	plainArgs := append(append([]string{}, base...), "--ledger", plainLedger, "--json")
	code, output := runTest(t, plainArgs...)
	if code != 1 || strings.Contains(output, "relocates") {
		t.Fatalf("without relocate: code=%d output=%q", code, output)
	}

	relocatedLedger := filepath.Join(t.TempDir(), "relocated.jsonl")
	relocatedArgs := append(append([]string{}, base...), "--ledger", relocatedLedger, "--relocate", "--json")
	code, output = runTest(t, relocatedArgs...)
	if code != 1 {
		t.Fatalf("with relocate: code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	if len(result.Details) != 2 || !strings.Contains(result.Details[1], `evidence "evidence-1" ("notes.txt")`) || !strings.Contains(result.Details[1], "content relocates to lines 2-3 (shift +1; nearest matching window)") {
		t.Fatalf("with relocate: result=%+v", result)
	}
}
