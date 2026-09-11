package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/node"
	"ownscout/internal/nodepacket"
)

func TestNodeVerifySuccessHumanAndJSON(t *testing.T) {
	packetPath, envelopePath, ledgerPath := writeNodeFixture(t, false)
	args := []string{"node", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", packetPath, "--envelope", envelopePath, "--ledger", ledgerPath}

	code, output := runTest(t, args...)
	if code != 0 || !strings.Contains(output, "all nodes are evidence_current") || !strings.Contains(output, `node "build": evidence_current`) {
		t.Fatalf("human: code=%d output=%q", code, output)
	}

	code, output = runTest(t, append(args, "--json")...)
	if code != 0 {
		t.Fatalf("json: code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	if !result.OK || result.Command != "node verify" || result.Summary != "all nodes are evidence_current" {
		t.Fatalf("unexpected JSON result: %+v", result)
	}
}

func TestNodeVerifyInvalidEnvelopeIsExitTwo(t *testing.T) {
	packetPath, _, ledgerPath := writeNodeFixture(t, false)
	envelopePath := filepath.Join(t.TempDir(), "envelope.json")
	if err := os.WriteFile(envelopePath, []byte(`{"unexpected_secret":"DO_NOT_PRINT"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"node", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", packetPath, "--envelope", envelopePath, "--ledger", ledgerPath}
	for _, mode := range []string{"human", "json"} {
		modeArgs := append([]string(nil), args...)
		if mode == "json" {
			modeArgs = append(modeArgs, "--json")
		}
		code, output := runTest(t, modeArgs...)
		if code != 2 || strings.Contains(output, "DO_NOT_PRINT") {
			t.Fatalf("%s: code=%d output=%q", mode, code, output)
		}
		if mode == "json" {
			result := assertStableNodeJSON(t, output)
			if result.OK || result.Summary != "envelope could not be parsed" {
				t.Fatalf("unexpected JSON result: %+v", result)
			}
		}
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("invalid envelope created ledger: err=%v", err)
	}
}

func TestNodeVerifyInvalidPacketContractIsExitTwo(t *testing.T) {
	packetPath, envelopePath, ledgerPath := writeNodeFixture(t, false)
	packetBytes, err := os.ReadFile(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(packetBytes, &raw); err != nil {
		t.Fatal(err)
	}
	raw["outcome"] = json.RawMessage(`"impossible"`)
	packetBytes, _ = json.Marshal(raw)
	if err := os.WriteFile(packetPath, packetBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"node", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", packetPath, "--envelope", envelopePath, "--ledger", ledgerPath}
	for _, mode := range []string{"human", "json"} {
		modeArgs := append([]string(nil), args...)
		if mode == "json" {
			modeArgs = append(modeArgs, "--json")
		}
		code, output := runTest(t, modeArgs...)
		if code != 2 || !strings.Contains(output, "packet contract failed") {
			t.Fatalf("%s: code=%d output=%q", mode, code, output)
		}
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("invalid packet created ledger: err=%v", err)
	}
}

func TestNodeVerifyFailedAndBlockedResultsAreAppendedInOrder(t *testing.T) {
	packetPath, _, ledgerPath := writeNodeFixture(t, true)
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
		EnvelopeID:          "envelope-failed",
		PacketID:            packet.PacketID,
		PacketBindingSHA256: binding,
		Nodes: []node.Node{
			{NodeID: "a", DependsOn: []string{}, Verifier: node.VerifierCurrent, EvidenceIDs: []string{"evidence-1"}},
			{NodeID: "b", DependsOn: []string{"a"}, Verifier: node.VerifierCurrent, EvidenceIDs: []string{"evidence-1"}},
		},
	})

	code, output := runTest(t, "node", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", packetPath, "--envelope", envelopePath, "--ledger", ledgerPath, "--json")
	if code != 1 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	result := assertStableNodeJSON(t, output)
	if result.OK || len(result.Details) != 2 || result.Details[0][:len(`node "a"`)] != `node "a"` || result.Details[1][:len(`node "b"`)] != `node "b"` {
		t.Fatalf("unexpected ordered details: %+v", result)
	}
	if !strings.Contains(output, "failed") || !strings.Contains(output, "blocked") {
		t.Fatalf("missing node statuses: %q", output)
	}

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		NodeResults []struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		} `json:"node_results"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.NodeResults) != 2 || record.NodeResults[0].NodeID != "a" || record.NodeResults[0].Status != "failed" || record.NodeResults[1].NodeID != "b" || record.NodeResults[1].Status != "blocked" {
		t.Fatalf("unexpected ledger results: %+v", record.NodeResults)
	}
}

func writeNodeFixture(t *testing.T, stale bool) (packetPath, envelopePath, ledgerPath string) {
	t.Helper()
	packetPath = filepath.Join(t.TempDir(), "packet.json")
	packetBytes, err := os.ReadFile(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(packetBytes, &raw); err != nil {
			t.Fatal(err)
		}
		var evidence []map[string]json.RawMessage
		if err := json.Unmarshal(raw["evidence"], &evidence); err != nil {
			t.Fatal(err)
		}
		evidence[0]["content_hash"] = json.RawMessage(`"0000000000000000000000000000000000000000000000000000000000000000"`)
		raw["evidence"], _ = json.Marshal(evidence)
		packetBytes, _ = json.Marshal(raw)
	}
	if err := os.WriteFile(packetPath, packetBytes, 0o600); err != nil {
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
	envelopePath = writeEnvelope(t, node.Envelope{
		SchemaVersion:       node.SchemaVersionV1,
		EnvelopeID:          "envelope-1",
		PacketID:            packet.PacketID,
		PacketBindingSHA256: binding,
		Nodes:               []node.Node{{NodeID: "build", DependsOn: []string{}, Verifier: node.VerifierCurrent, EvidenceIDs: []string{"evidence-1"}}},
	})
	ledgerPath = filepath.Join(t.TempDir(), "ledger.jsonl")
	return packetPath, envelopePath, ledgerPath
}

func writeEnvelope(t *testing.T, envelope node.Envelope) string {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "envelope.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertStableNodeJSON(t *testing.T, output string) resultData {
	t.Helper()
	if strings.Count(output, "\n") != 1 || !strings.HasSuffix(output, "\n") {
		t.Fatalf("expected one JSON line: %q", output)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 5 {
		t.Fatalf("unexpected JSON fields: %v", fields)
	}
	var result resultData
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
