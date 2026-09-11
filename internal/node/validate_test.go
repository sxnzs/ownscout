package node_test

import (
	"fmt"
	"strings"
	"testing"

	"ownscout/internal/contract"
	"ownscout/internal/node"
)

func TestValidateEnvelopeRejectsInvalidGraphs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*node.Envelope)
		want   string
	}{
		{"schema missing", func(e *node.Envelope) { e.SchemaVersion = "" }, "schema_version"},
		{"schema unsupported", func(e *node.Envelope) { e.SchemaVersion = "node-envelope-v2" }, "schema_version"},
		{"schema case", func(e *node.Envelope) { e.SchemaVersion = "NODE-ENVELOPE-V1" }, "schema_version"},
		{"packet mismatch", func(e *node.Envelope) { e.PacketID = "packet-2" }, "packet_id"},
		{"packet case mismatch", func(e *node.Envelope) { e.PacketID = "Packet-1" }, "packet_id"},
		{"duplicate nodes", func(e *node.Envelope) { e.Nodes[1].NodeID = "a" }, "duplicate node_id"},
		{"duplicate dependencies", func(e *node.Envelope) { e.Nodes[1].DependsOn = []string{"a", "a"} }, "duplicate depends_on"},
		{"duplicate evidence refs", func(e *node.Envelope) { e.Nodes[0].EvidenceIDs = []string{"e1", "e1"} }, "duplicate evidence_ids"},
		{"missing dependency", func(e *node.Envelope) { e.Nodes[1].DependsOn = []string{"missing"} }, "missing dependency"},
		{"dependency case mismatch", func(e *node.Envelope) { e.Nodes[1].DependsOn = []string{"A"} }, "missing dependency"},
		{"missing evidence", func(e *node.Envelope) { e.Nodes[0].EvidenceIDs = []string{"missing"} }, "not present in packet"},
		{"evidence case mismatch", func(e *node.Envelope) { e.Nodes[0].EvidenceIDs = []string{"E1"} }, "not present in packet"},
		{"self dependency", func(e *node.Envelope) { e.Nodes[0].DependsOn = []string{"a"} }, "self-dependency"},
		{"two node cycle", func(e *node.Envelope) { e.Nodes[0].DependsOn = []string{"b"} }, "cycle"},
		{"three node cycle", func(e *node.Envelope) {
			e.Nodes[0].DependsOn = []string{"c"}
			e.Nodes = append(e.Nodes, testNode("c", "b"))
		}, "cycle"},
		{"disconnected cycle", func(e *node.Envelope) {
			e.Nodes = append(e.Nodes, testNode("c", "d"), testNode("d", "c"))
		}, "cycle"},
		{"invalid verifier", func(e *node.Envelope) { e.Nodes[1].Verifier = "evidence.future" }, "verifier"},
		{"empty verifier", func(e *node.Envelope) { e.Nodes[1].Verifier = "" }, "verifier"},
		{"case sensitive verifier", func(e *node.Envelope) { e.Nodes[1].Verifier = "Evidence.Current" }, "verifier"},
		{"no trimming verifier", func(e *node.Envelope) { e.Nodes[1].Verifier = "evidence.current " }, "verifier"},
		{"nil nodes", func(e *node.Envelope) { e.Nodes = nil }, "nodes"},
		{"empty nodes", func(e *node.Envelope) { e.Nodes = []node.Node{} }, "at least one node"},
		{"nil dependencies", func(e *node.Envelope) { e.Nodes[0].DependsOn = nil }, "depends_on"},
		{"nil evidence refs", func(e *node.Envelope) { e.Nodes[0].EvidenceIDs = nil }, "evidence_ids"},
		{"empty evidence refs", func(e *node.Envelope) { e.Nodes[0].EvidenceIDs = []string{} }, "evidence_ids"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			env, binding := testEnvelope(t, packet, testNode("a"), testNode("b", "a"))
			tt.mutate(&env)
			err := node.ValidateEnvelope(env, packet, binding)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validation error = %v, want %q", err, tt.want)
			}
			got := node.EvaluateEnvelope(env, packet, binding, verifiedReport("e1", "e2"))
			if got.OK || got.Results == nil || len(got.Results) != 0 {
				t.Fatalf("invalid envelope evaluated nodes: %+v", got)
			}
		})
	}
}

func TestValidateEnvelopeRecomputesBinding(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*node.Envelope, *contract.Packet, *string)
		valid  bool
	}{
		{"valid", func(*node.Envelope, *contract.Packet, *string) {}, true},
		{"producer hash ignored", func(_ *node.Envelope, p *contract.Packet, _ *string) {
			p.PacketHash = "changed"
		}, true},
		{"producer hash absent", func(_ *node.Envelope, p *contract.Packet, _ *string) {
			p.PacketHash = ""
		}, true},
		{"supplied binding missing", func(_ *node.Envelope, _ *contract.Packet, b *string) { *b = "" }, false},
		{"envelope binding missing", func(e *node.Envelope, _ *contract.Packet, _ *string) {
			e.PacketBindingSHA256 = ""
		}, false},
		{"supplied binding mismatch", func(_ *node.Envelope, _ *contract.Packet, b *string) {
			*b = strings.Repeat("0", 64)
		}, false},
		{"envelope binding mismatch", func(e *node.Envelope, _ *contract.Packet, _ *string) {
			e.PacketBindingSHA256 = strings.Repeat("0", 64)
		}, false},
		{"matching forged bindings", func(e *node.Envelope, _ *contract.Packet, b *string) {
			*b = strings.Repeat("0", 64)
			e.PacketBindingSHA256 = *b
		}, false},
		{"producer hash is not canonical binding", func(e *node.Envelope, p *contract.Packet, b *string) {
			*b = p.PacketHash
			e.PacketBindingSHA256 = *b
		}, false},
		{"uppercase binding", func(e *node.Envelope, _ *contract.Packet, b *string) {
			*b = strings.ToUpper(*b)
			e.PacketBindingSHA256 = *b
		}, false},
		{"prefixed binding", func(e *node.Envelope, _ *contract.Packet, b *string) {
			*b = "sha256:" + *b
			e.PacketBindingSHA256 = *b
		}, false},
		{"binding whitespace", func(e *node.Envelope, _ *contract.Packet, b *string) {
			*b += " "
			e.PacketBindingSHA256 = *b
		}, false},
		{"malformed binding", func(e *node.Envelope, _ *contract.Packet, b *string) {
			*b = strings.Repeat("g", 64)
			e.PacketBindingSHA256 = *b
		}, false},
		{"packet content changed", func(_ *node.Envelope, p *contract.Packet, _ *string) {
			p.Evidence[0].ContentHash = "changed"
		}, false},
		{"packet producer status changed", func(_ *node.Envelope, p *contract.Packet, _ *string) {
			p.Evidence[0].VerifierStatus = "failed"
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			env, binding := testEnvelope(t, packet, testNode("a"))
			tt.mutate(&env, &packet, &binding)
			err := node.ValidateEnvelope(env, packet, binding)
			if (err == nil) != tt.valid {
				t.Fatalf("validation error = %v, want valid %v", err, tt.valid)
			}
			if !tt.valid {
				got := node.EvaluateEnvelope(env, packet, binding, verifiedReport("e1", "e2"))
				if got.OK || len(got.Results) != 0 {
					t.Fatalf("invalid binding evaluated nodes: %+v", got)
				}
			}
		})
	}
}

func TestValidateEnvelopePacketEvidenceIDs(t *testing.T) {
	for _, referenced := range []bool{true, false} {
		t.Run(fmt.Sprintf("duplicate referenced=%v", referenced), func(t *testing.T) {
			packet := testPacket()
			index := 1
			if referenced {
				index = 0
			}
			duplicate := packet.Evidence[index]
			duplicate.Path = "different-source.go"
			packet.Evidence = append(packet.Evidence, duplicate)
			env, binding := testEnvelope(t, packet, testNode("a"))
			err := node.ValidateEnvelope(env, packet, binding)
			if err == nil || !strings.Contains(err.Error(), "duplicate packet evidence_id") {
				t.Fatalf("duplicate packet evidence error = %v", err)
			}
			got := node.EvaluateEnvelope(env, packet, binding, verifiedReport("e1", "e2"))
			if got.OK || len(got.Results) != 0 {
				t.Fatalf("duplicate packet evidence evaluated: %+v", got)
			}
		})
	}
}

func TestValidateEnvelopeIdentifierSyntax(t *testing.T) {
	valid := []string{"a", "Z", "0", "Ab9._-:z", strings.Repeat("a", 128)}
	for _, id := range valid {
		t.Run("valid "+id, func(t *testing.T) {
			packet := testPacket()
			packet.PacketID = id
			packet.Evidence[0].EvidenceID = id
			target := testNode(id)
			target.EvidenceIDs = []string{id}
			consumer := testNode("consumer", id)
			consumer.EvidenceIDs = []string{id}
			env, binding := testEnvelope(t, packet, target, consumer)
			env.EnvelopeID = id
			if err := node.ValidateEnvelope(env, packet, binding); err != nil {
				t.Fatalf("valid identifier %q rejected: %v", id, err)
			}
		})
	}
	invalid := []string{
		"", strings.Repeat("a", 129), ".a", "_a", "-a", ":a", " a", "a ",
		"a b", "a/b", "a\\b", "a@", "a\n", "a\t", "a\x00", "é", "aé", "🦉",
	}
	for _, field := range []string{"envelope_id", "packet_id", "node_id", "depends_on", "evidence_ids", "packet evidence_id"} {
		for _, id := range invalid {
			t.Run(field+"/"+fmt.Sprintf("%q", id), func(t *testing.T) {
				packet := testPacket()
				env, _ := testEnvelope(t, packet, testNode("a"))
				switch field {
				case "envelope_id":
					env.EnvelopeID = id
				case "packet_id":
					env.PacketID, packet.PacketID = id, id
				case "node_id":
					env.Nodes[0].NodeID = id
				case "depends_on":
					env.Nodes[0].DependsOn = []string{id}
				case "evidence_ids":
					env.Nodes[0].EvidenceIDs = []string{id}
				case "packet evidence_id":
					packet.Evidence[1].EvidenceID = id // Check unreferenced IDs too.
				}
				binding, err := node.CanonicalPacketBinding(packet)
				if err != nil {
					t.Fatal(err)
				}
				env.PacketBindingSHA256 = binding
				err = node.ValidateEnvelope(env, packet, binding)
				if err == nil || !strings.Contains(err.Error(), field) {
					t.Fatalf("identifier %q validation = %v, want field %q", id, err, field)
				}
			})
		}
	}
}

func TestValidateEnvelopeCaseSensitiveDistinctIDs(t *testing.T) {
	packet := testPacket()
	packet.Evidence[1].EvidenceID = "E1"
	a, upper := testNode("a"), testNode("A")
	a.EvidenceIDs = []string{"e1", "E1"}
	upper.EvidenceIDs = []string{"E1"}
	joined := testNode("join", "a", "A")
	env, binding := testEnvelope(t, packet, joined, a, upper)
	if err := node.ValidateEnvelope(env, packet, binding); err != nil {
		t.Fatal(err)
	}
	got := node.EvaluateEnvelope(env, packet, binding, verifiedReport("e1", "E1"))
	if !got.OK || len(got.Results) != 3 {
		t.Fatalf("evaluation = %+v", got)
	}
	for i, id := range []string{"A", "a", "join"} {
		if got.Results[i].NodeID != id {
			t.Fatalf("result %d = %+v, want ID %q", i, got.Results[i], id)
		}
	}
}

func TestEnvelopeLimits(t *testing.T) {
	makeNodes := func(count, refs int) []node.Node {
		nodes := make([]node.Node, count)
		for i := range nodes {
			nodes[i] = testNode(fmt.Sprintf("n%03d", i))
			nodes[i].EvidenceIDs = evidenceIDs(refs)
		}
		return nodes
	}
	withDependencies := func(count, evidenceRefs int) []node.Node {
		nodes := makeNodes(count+1, 1)
		for i := 0; i < count; i++ {
			nodes[count].DependsOn = append(nodes[count].DependsOn, nodes[i].NodeID)
		}
		nodes[count].EvidenceIDs = evidenceIDs(evidenceRefs)
		return nodes
	}
	combinedTotal := func(over bool) []node.Node {
		nodes := makeNodes(32, 128)
		nodes[0].DependsOn = []string{nodes[1].NodeID}
		if !over {
			nodes[0].EvidenceIDs = nodes[0].EvidenceIDs[:127]
		}
		return nodes
	}
	tests := []struct {
		name  string
		nodes []node.Node
		valid bool
		want  string
	}{
		{"zero nodes", []node.Node{}, false, "at least one node"},
		{"256 nodes", makeNodes(256, 1), true, ""},
		{"257 nodes", makeNodes(257, 1), false, "256"},
		{"128 dependencies", withDependencies(128, 1), true, ""},
		{"129 dependencies", withDependencies(129, 1), false, "128"},
		{"128 evidence refs", makeNodes(1, 128), true, ""},
		{"129 evidence refs", makeNodes(1, 129), false, "128"},
		{"128 refs in each array", withDependencies(128, 128), true, ""},
		{"4096 evidence refs", makeNodes(32, 128), true, ""},
		{"4096 combined refs", combinedTotal(false), true, ""},
		{"4097 combined refs", combinedTotal(true), false, "4096"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			base := packet.Evidence[0]
			packet.Evidence = nil
			for _, id := range evidenceIDs(129) {
				item := base
				item.EvidenceID = id
				packet.Evidence = append(packet.Evidence, item)
			}
			env, binding := testEnvelope(t, packet, tt.nodes...)
			_, parseErr := node.ParseEnvelope(marshalJSON(t, env))
			validateErr := node.ValidateEnvelope(env, packet, binding)
			for name, err := range map[string]error{"parse": parseErr, "validate": validateErr} {
				if (err == nil) != tt.valid {
					t.Errorf("%s error = %v, want valid=%v", name, err, tt.valid)
				}
				if !tt.valid && (err == nil || !strings.Contains(err.Error(), tt.want)) {
					t.Errorf("%s error = %v, want limit %q", name, err, tt.want)
				}
			}
			got := node.EvaluateEnvelope(env, packet, binding, verifiedReport(evidenceIDs(129)...))
			if got.OK != tt.valid {
				t.Fatalf("evaluation OK = %v, want %v", got.OK, tt.valid)
			}
			if tt.valid && len(got.Results) != len(tt.nodes) {
				t.Fatalf("got %d results, want %d", len(got.Results), len(tt.nodes))
			}
			if !tt.valid && len(got.Results) != 0 {
				t.Fatalf("out-of-bounds envelope evaluated %d nodes", len(got.Results))
			}
		})
	}
}
