package node_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
	"ownscout/internal/node"
)

func testPacket() contract.Packet {
	return contract.Packet{
		PacketID:      "packet-1",
		SchemaVersion: contract.SchemaVersionV1,
		RepoRoot:      "/repo",
		HeadCommit:    "abc123",
		RequestID:     "request-1",
		IssuedAt:      "2026-09-05T00:00:00Z",
		Outcome:       contract.OutcomeComplete,
		Freshness:     contract.Freshness{HeadCommit: "abc123", Status: "current"},
		Authorization: contract.Authorization{
			Level: contract.AuthorizationAutonomous,
		},
		Budget: contract.Budget{MaxEvidence: 128, UsedEvidence: 2, MaxBytes: 1024, UsedBytes: 10},
		Evidence: []contract.Evidence{
			{
				EvidenceID: "e1", Kind: "file", Path: "one.go", Commit: "abc123",
				LineStart: 1, LineEnd: 2, Source: "repository",
				ContentHash: strings.Repeat("a", 64), CollectedAt: "2026-09-05T00:00:00Z",
				VerifierStatus: "verified",
			},
			{
				EvidenceID: "e2", Kind: "file", Path: "two.go", Commit: "abc123",
				LineStart: 3, LineEnd: 4, Source: "repository",
				ContentHash: strings.Repeat("b", 64), CollectedAt: "2026-09-05T00:00:00Z",
				VerifierStatus: "verified",
			},
		},
		Degradations: []string{},
		Provenance:   contract.Provenance{Collector: "test", Version: "1"},
		PacketHash:   "untrusted-producer-hash",
	}
}

func testNode(id string, dependencies ...string) node.Node {
	return node.Node{
		NodeID:      id,
		DependsOn:   append([]string{}, dependencies...),
		Verifier:    node.VerifierCurrent,
		EvidenceIDs: []string{"e1"},
	}
}

func testEnvelope(t *testing.T, packet contract.Packet, nodes ...node.Node) (node.Envelope, string) {
	t.Helper()
	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		t.Fatal(err)
	}
	return node.Envelope{
		SchemaVersion:       node.SchemaVersionV1,
		EnvelopeID:          "envelope-1",
		PacketID:            packet.PacketID,
		PacketBindingSHA256: binding,
		Nodes:               append([]node.Node{}, nodes...),
	}, binding
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func verifiedReport(ids ...string) evidence.Report {
	report := evidence.Report{Ok: true, Results: []evidence.Verification{}}
	for _, id := range ids {
		report.Results = append(report.Results, evidence.Verification{EvidenceID: id, Status: "verified"})
	}
	return report
}

func TestCanonicalPacketBinding(t *testing.T) {
	packet := testPacket()
	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		t.Fatal(err)
	}
	if packet.PacketHash != "untrusted-producer-hash" {
		t.Fatal("CanonicalPacketBinding mutated PacketHash")
	}
	cleared := packet
	cleared.PacketHash = ""
	sum := sha256.Sum256(marshalJSON(t, cleared))
	if want := hex.EncodeToString(sum[:]); binding != want {
		t.Fatalf("binding = %q, want %q", binding, want)
	}
	if len(binding) != 64 || strings.ToLower(binding) != binding {
		t.Fatalf("binding is not lowercase SHA-256: %q", binding)
	}

	t.Run("producer hash ignored", func(t *testing.T) {
		for _, hash := range []string{"", "different", binding, strings.Repeat("0", 64)} {
			copy := packet
			copy.PacketHash = hash
			got, err := node.CanonicalPacketBinding(copy)
			if err != nil || got != binding {
				t.Fatalf("hash %q: binding = %q, err = %v", hash, got, err)
			}
		}
	})
	t.Run("decoded formatting and object key order are irrelevant", func(t *testing.T) {
		// Map encoding sorts keys, unlike the Packet's declared field order.
		var object map[string]json.RawMessage
		if err := json.Unmarshal(marshalJSON(t, packet), &object); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		var decoded contract.Packet
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		got, err := node.CanonicalPacketBinding(decoded)
		if err != nil || got != binding {
			t.Fatalf("binding = %q, want %q; err = %v", got, binding, err)
		}
	})

	changes := []struct {
		name   string
		mutate func(*contract.Packet)
	}{
		{"packet ID", func(p *contract.Packet) { p.PacketID = "packet-2" }},
		{"schema", func(p *contract.Packet) { p.SchemaVersion = "v2" }},
		{"repo", func(p *contract.Packet) { p.RepoRoot = "/other" }},
		{"head", func(p *contract.Packet) { p.HeadCommit = "def456" }},
		{"request", func(p *contract.Packet) { p.RequestID = "request-2" }},
		{"issued time", func(p *contract.Packet) { p.IssuedAt = "later" }},
		{"outcome", func(p *contract.Packet) { p.Outcome = contract.OutcomeBlocked }},
		{"freshness", func(p *contract.Packet) { p.Freshness.Current = true }},
		{"authorization", func(p *contract.Packet) { p.Authorization.Level = "blocked" }},
		{"budget", func(p *contract.Packet) { p.Budget.MaxBytes++ }},
		{"evidence hash", func(p *contract.Packet) { p.Evidence[0].ContentHash = "changed" }},
		{"evidence path", func(p *contract.Packet) { p.Evidence[0].Path = "changed.go" }},
		{"evidence status", func(p *contract.Packet) { p.Evidence[0].VerifierStatus = "failed" }},
		{"evidence order", func(p *contract.Packet) {
			p.Evidence[0], p.Evidence[1] = p.Evidence[1], p.Evidence[0]
		}},
		{"degradations", func(p *contract.Packet) { p.Degradations = []string{"limited"} }},
		{"nil versus empty arrays", func(p *contract.Packet) { p.Degradations = nil }},
		{"provenance", func(p *contract.Packet) { p.Provenance.Version = "2" }},
	}
	for _, tt := range changes {
		t.Run(tt.name, func(t *testing.T) {
			changed := testPacket()
			tt.mutate(&changed)
			got, err := node.CanonicalPacketBinding(changed)
			if err != nil {
				t.Fatal(err)
			}
			if got == binding {
				t.Fatal("packet change did not change binding")
			}
		})
	}
}

func TestEvaluateEnvelopeTopologicalOrder(t *testing.T) {
	packet := testPacket()
	nodes := []node.Node{
		testNode("z"),
		testNode("d", "c", "b"),
		testNode("b", "a"),
		testNode("c", "a"),
		testNode("a"),
	}
	want := []node.Result{
		{NodeID: "a", Status: node.StatusEvidenceCurrent},
		{NodeID: "b", Status: node.StatusEvidenceCurrent},
		{NodeID: "c", Status: node.StatusEvidenceCurrent},
		{NodeID: "d", Status: node.StatusEvidenceCurrent},
		{NodeID: "z", Status: node.StatusEvidenceCurrent},
	}
	// Exercise every declaration permutation, including newly ready IDs that
	// sort before an independent node already in the ready queue.
	var permutations func(int)
	permutations = func(start int) {
		if start == len(nodes) {
			env, binding := testEnvelope(t, packet, nodes...)
			before := marshalJSON(t, env)
			got := node.EvaluateEnvelope(env, packet, binding, verifiedReport("e1"))
			if !got.OK || !reflect.DeepEqual(got.Results, want) {
				t.Fatalf("evaluation = %+v, want %+v", got, want)
			}
			if !bytes.Equal(before, marshalJSON(t, env)) {
				t.Fatal("evaluation mutated envelope")
			}
			return
		}
		for i := start; i < len(nodes); i++ {
			nodes[start], nodes[i] = nodes[i], nodes[start]
			permutations(start + 1)
			nodes[start], nodes[i] = nodes[i], nodes[start]
		}
	}
	permutations(0)
}

func TestEvaluateEnvelopeEvidenceStatuses(t *testing.T) {
	tests := []struct {
		name    string
		results []evidence.Verification
		want    node.Status
	}{
		{"verified", []evidence.Verification{{EvidenceID: "e1", Status: "verified"}}, node.StatusEvidenceCurrent},
		{"failed", []evidence.Verification{{EvidenceID: "e1", Status: "failed"}}, node.StatusFailed},
		{"skipped", []evidence.Verification{{EvidenceID: "e1", Status: "skipped"}}, node.StatusFailed},
		{"pending", []evidence.Verification{{EvidenceID: "e1", Status: "pending"}}, node.StatusFailed},
		{"unverified", []evidence.Verification{{EvidenceID: "e1", Status: "unverified"}}, node.StatusFailed},
		{"unknown", []evidence.Verification{{EvidenceID: "e1", Status: "other"}}, node.StatusFailed},
		{"empty", []evidence.Verification{{EvidenceID: "e1"}}, node.StatusFailed},
		{"case sensitive status", []evidence.Verification{{EvidenceID: "e1", Status: "Verified"}}, node.StatusFailed},
		{"no trimming status", []evidence.Verification{{EvidenceID: "e1", Status: "verified "}}, node.StatusFailed},
		{"missing", nil, node.StatusFailed},
		{"case sensitive ID", []evidence.Verification{{EvidenceID: "E1", Status: "verified"}}, node.StatusFailed},
		{"no trimming ID", []evidence.Verification{{EvidenceID: "e1 ", Status: "verified"}}, node.StatusFailed},
		{"failure cannot be overwritten", []evidence.Verification{
			{EvidenceID: "e1", Status: "failed"}, {EvidenceID: "e1", Status: "verified"},
		}, node.StatusFailed},
		{"failure after success", []evidence.Verification{
			{EvidenceID: "e1", Status: "verified"}, {EvidenceID: "e1", Status: "skipped"},
		}, node.StatusFailed},
		{"repeated verified results", []evidence.Verification{
			{EvidenceID: "e1", Status: "verified"}, {EvidenceID: "e1", Status: "verified"},
		}, node.StatusEvidenceCurrent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			env, binding := testEnvelope(t, packet, testNode("a"))
			// Neither an optimistic summary nor the producer's status overrides
			// the individual current verification result.
			report := evidence.Report{Ok: true, VerifiedCount: 999, Results: tt.results}
			got := node.EvaluateEnvelope(env, packet, binding, report)
			if got.OK != (tt.want == node.StatusEvidenceCurrent) || len(got.Results) != 1 {
				t.Fatalf("evaluation = %+v", got)
			}
			if result := got.Results[0]; result.NodeID != "a" || result.Status != tt.want {
				t.Fatalf("result = %+v, want status %q", result, tt.want)
			}
			if tt.want == node.StatusFailed && !strings.Contains(got.Results[0].Reason, "e1") {
				t.Fatalf("missing evidence failure reason: %+v", got.Results[0])
			}
		})
	}
}

func TestEvaluateEnvelopeRequiresEveryEvidenceReference(t *testing.T) {
	packet := testPacket()
	n := testNode("a")
	n.EvidenceIDs = []string{"e1", "e2"}
	env, binding := testEnvelope(t, packet, n)
	for _, status := range []string{"missing", "failed", "skipped", "verified"} {
		t.Run(status, func(t *testing.T) {
			report := verifiedReport("e1")
			if status != "missing" {
				report.Results = append(report.Results, evidence.Verification{EvidenceID: "e2", Status: status})
			}
			got := node.EvaluateEnvelope(env, packet, binding, report)
			if got.OK != (status == "verified") || len(got.Results) != 1 {
				t.Fatalf("evaluation = %+v", got)
			}
			want := node.StatusFailed
			if status == "verified" {
				want = node.StatusEvidenceCurrent
			}
			if got.Results[0].Status != want {
				t.Fatalf("result = %+v, want %q", got.Results[0], want)
			}
		})
	}
}

func TestEvaluateEnvelopeBlockingAndIndependentBranches(t *testing.T) {
	packet := testPacket()
	failed := testNode("a-failed")
	failed.EvidenceIDs = []string{"e2"}
	blocked := testNode("b-blocked", "a-failed")
	blocked.EvidenceIDs = []string{"e2"} // Blocking takes precedence over failure.
	env, binding := testEnvelope(t, packet,
		testNode("z-independent-child", "y-independent"),
		testNode("d-join", "a-failed", "c-current"),
		testNode("c-current"),
		blocked,
		testNode("e-blocked-again", "b-blocked"),
		testNode("y-independent"),
		failed,
	)
	report := verifiedReport("e1")
	report.Ok = false
	report.Results = append(report.Results, evidence.Verification{EvidenceID: "e2", Status: "failed"})
	got := node.EvaluateEnvelope(env, packet, binding, report)
	want := []struct {
		id     string
		status node.Status
	}{
		{"a-failed", node.StatusFailed},
		{"b-blocked", node.StatusBlocked},
		{"c-current", node.StatusEvidenceCurrent},
		{"d-join", node.StatusBlocked},
		{"e-blocked-again", node.StatusBlocked},
		{"y-independent", node.StatusEvidenceCurrent},
		{"z-independent-child", node.StatusEvidenceCurrent},
	}
	if got.OK || len(got.Results) != len(want) {
		t.Fatalf("evaluation = %+v", got)
	}
	for i, expected := range want {
		result := got.Results[i]
		if result.NodeID != expected.id || result.Status != expected.status {
			t.Errorf("result %d = %+v, want %+v", i, result, expected)
		}
		if expected.status != node.StatusEvidenceCurrent && result.Reason == "" {
			t.Errorf("result %d lacks failure/blocking reason", i)
		}
	}
}

func TestEvaluateEnvelopeIgnoresUnrelatedFailuresAndProducerStatus(t *testing.T) {
	packet := testPacket()
	packet.Evidence[0].VerifierStatus = "failed"
	env, binding := testEnvelope(t, packet, testNode("a"))
	report := verifiedReport("e1")
	report.Ok = false
	report.FailedCount = 100
	report.Results = append(report.Results, evidence.Verification{EvidenceID: "e2", Status: "failed"})
	got := node.EvaluateEnvelope(env, packet, binding, report)
	if !got.OK || len(got.Results) != 1 || got.Results[0].Status != node.StatusEvidenceCurrent {
		t.Fatalf("evaluation = %+v", got)
	}
}

func TestEvaluateEnvelopeEmptyGraph(t *testing.T) {
	packet := testPacket()
	env, binding := testEnvelope(t, packet)
	if err := node.ValidateEnvelope(env, packet, binding); err == nil || !strings.Contains(err.Error(), "at least one node") {
		t.Fatalf("empty graph error = %v, want at least one node", err)
	}
}

func TestPublicJSONShape(t *testing.T) {
	if node.SchemaVersionV1 != "node-envelope-v1" || node.VerifierCurrent != "evidence.current" {
		t.Fatal("public schema/verifier constants changed")
	}
	got := marshalJSON(t, node.Evaluation{OK: false, Results: []node.Result{
		{NodeID: "a", Status: node.StatusEvidenceCurrent},
		{NodeID: "b", Status: node.StatusFailed, Reason: "failure"},
		{NodeID: "c", Status: node.StatusBlocked, Reason: "dependency"},
	}})
	want := `{"ok":false,"results":[{"node_id":"a","status":"evidence_current"},{"node_id":"b","status":"failed","reason":"failure"},{"node_id":"c","status":"blocked","reason":"dependency"}]}`
	if string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func evidenceIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("e%d", i+1)
	}
	return ids
}
