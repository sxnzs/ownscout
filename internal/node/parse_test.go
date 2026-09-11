package node_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"ownscout/internal/node"
)

func TestParseEnvelopeStrictBoundaries(t *testing.T) {
	packet := testPacket()
	env, _ := testEnvelope(t, packet, testNode("a"))
	valid := string(marshalJSON(t, env))
	tests := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"whitespace", " \n\t"},
		{"null root", "null"},
		{"array root", "[]"},
		{"string root", `"envelope"`},
		{"number root", "1"},
		{"boolean root", "true"},
		{"empty object", "{}"},
		{"truncated", valid[:len(valid)-1]},
		{"trailing object", valid + "{}"},
		{"trailing null", valid + " null"},
		{"trailing number", valid + " 1"},
		{"trailing garbage", valid + " nope"},
		{"trailing comma", strings.TrimSuffix(valid, "}") + ",}"},
		{"comment", valid + " // no"},
		{"unknown top field", strings.Replace(valid, `"envelope_id":`, `"extra":false,"envelope_id":`, 1)},
		{"unknown node field", strings.Replace(valid, `"node_id":`, `"extra":{},"node_id":`, 1)},
		{"case folded top field", strings.Replace(valid, `"envelope_id":`, `"Envelope_ID":`, 1)},
		{"case folded node field", strings.Replace(valid, `"node_id":`, `"NODE_ID":`, 1)},
		{"null schema", strings.Replace(valid, `"schema_version":"node-envelope-v1"`, `"schema_version":null`, 1)},
		{"null ID", strings.Replace(valid, `"envelope_id":"envelope-1"`, `"envelope_id":null`, 1)},
		{"number ID", strings.Replace(valid, `"node_id":"a"`, `"node_id":1`, 1)},
		{"boolean verifier", strings.Replace(valid, `"verifier":"evidence.current"`, `"verifier":true`, 1)},
		{"null node", strings.Replace(valid, string(marshalJSON(t, env.Nodes[0])), "null", 1)},
		{"array node", strings.Replace(valid, string(marshalJSON(t, env.Nodes[0])), "[]", 1)},
		{"null dependency element", strings.Replace(valid, `"depends_on":[]`, `"depends_on":[null]`, 1)},
		{"number evidence element", strings.Replace(valid, `"evidence_ids":["e1"]`, `"evidence_ids":[1]`, 1)},
		{"null evidence element", strings.Replace(valid, `"evidence_ids":["e1"]`, `"evidence_ids":[null]`, 1)},
		{"nested evidence object", strings.Replace(valid, `"evidence_ids":["e1"]`, `"evidence_ids":[{}]`, 1)},
		{"nested dependency array", strings.Replace(valid, `"depends_on":[]`, `"depends_on":[[]]`, 1)},
		{"invalid UTF-8", strings.Replace(valid, `"a"`, "\"\xff\"", 1)},
		{"BOM", "\xef\xbb\xbf" + valid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := node.ParseEnvelope([]byte(tt.data))
			if err == nil {
				t.Fatalf("accepted invalid JSON: %s", tt.data)
			}
			if !reflect.DeepEqual(got, node.Envelope{}) {
				t.Fatalf("parse error returned partial envelope: %+v", got)
			}
		})
	}
	for _, data := range []string{valid, "\n\t " + valid + " \r\n", strings.Replace(valid, `"node_id"`, `"node_\u0069d"`, 1)} {
		got, err := node.ParseEnvelope([]byte(data))
		if err != nil || !reflect.DeepEqual(got, env) {
			t.Fatalf("valid parse = %+v, err = %v", got, err)
		}
	}
}

func TestParseEnvelopeDuplicateKeys(t *testing.T) {
	env, _ := testEnvelope(t, testPacket(), testNode("a"))
	valid := string(marshalJSON(t, env))
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{"schema", `"schema_version":`, `"schema_version":"node-envelope-v1","schema_version":`},
		{"envelope ID", `"envelope_id":`, `"envelope_id":"other","envelope_id":`},
		{"packet ID", `"packet_id":`, `"packet_id":"packet-1","packet_id":`},
		{"binding", `"packet_binding_sha256":`, `"packet_binding_sha256":"other","packet_binding_sha256":`},
		{"nodes", `"nodes":`, `"nodes":[],"nodes":`},
		{"node ID", `"node_id":`, `"node_id":"a","node_id":`},
		{"dependencies", `"depends_on":`, `"depends_on":[],"depends_on":`},
		{"verifier", `"verifier":`, `"verifier":"evidence.current","verifier":`},
		{"evidence IDs", `"evidence_ids":`, `"evidence_ids":["e1"],"evidence_ids":`},
		{"escaped top key", `"packet_id":`, `"packet_\u0069d":"packet-1","packet_id":`},
		{"escaped nested key", `"node_id":`, `"node_\u0069d":"a","node_id":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := strings.Replace(valid, tt.old, tt.new, 1)
			_, err := node.ParseEnvelope([]byte(data))
			if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("duplicate key error = %v", err)
			}
		})
	}
	// Identical field names in separate objects are not duplicate keys.
	env.Nodes = append(env.Nodes, testNode("b"))
	if _, err := node.ParseEnvelope(marshalJSON(t, env)); err != nil {
		t.Fatal(err)
	}
}

func TestParseEnvelopeRequiredFieldsAndArrays(t *testing.T) {
	env, _ := testEnvelope(t, testPacket(), testNode("a"))
	root := map[string]any{
		"schema_version": env.SchemaVersion, "envelope_id": env.EnvelopeID,
		"packet_id": env.PacketID, "packet_binding_sha256": env.PacketBindingSHA256,
		"nodes": env.Nodes,
	}
	nested := map[string]any{
		"node_id": "a", "depends_on": []string{}, "verifier": node.VerifierCurrent,
		"evidence_ids": []string{"e1"},
	}
	for _, field := range []string{"schema_version", "envelope_id", "packet_id", "packet_binding_sha256", "nodes"} {
		t.Run("missing "+field, func(t *testing.T) {
			saved := root[field]
			delete(root, field)
			_, err := node.ParseEnvelope(marshalJSON(t, root))
			root[field] = saved
			if err == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
	for _, field := range []string{"node_id", "depends_on", "verifier", "evidence_ids"} {
		t.Run("missing nested "+field, func(t *testing.T) {
			saved := nested[field]
			delete(nested, field)
			root["nodes"] = []any{nested}
			_, err := node.ParseEnvelope(marshalJSON(t, root))
			nested[field] = saved
			root["nodes"] = env.Nodes
			if err == nil {
				t.Fatal("missing nested field accepted")
			}
		})
	}
	for _, field := range []string{"nodes", "depends_on", "evidence_ids"} {
		for _, invalid := range []any{nil, "", map[string]any{}, false} {
			t.Run(field+" invalid array "+string(marshalJSON(t, invalid)), func(t *testing.T) {
				if field == "nodes" {
					root[field] = invalid
				} else {
					saved := nested[field]
					nested[field] = invalid
					defer func() { nested[field] = saved }()
					root["nodes"] = []any{nested}
				}
				_, err := node.ParseEnvelope(marshalJSON(t, root))
				root["nodes"] = env.Nodes
				if err == nil {
					t.Fatal("invalid array accepted")
				}
			})
		}
	}
	env.Nodes[0].EvidenceIDs = []string{}
	if _, err := node.ParseEnvelope(marshalJSON(t, env)); err == nil {
		t.Fatal("empty evidence_ids accepted")
	}
	env.Nodes = []node.Node{}
	if _, err := node.ParseEnvelope(marshalJSON(t, env)); err == nil || !strings.Contains(err.Error(), "at least one node") {
		t.Fatalf("empty nodes error = %v, want at least one node", err)
	}
}

func TestParseEnvelopeInputLimit(t *testing.T) {
	env, _ := testEnvelope(t, testPacket(), testNode("a"))
	data := marshalJSON(t, env)
	data = append(data, bytes.Repeat([]byte(" "), (1<<20)-len(data))...)
	if _, err := node.ParseEnvelope(data); err != nil {
		t.Fatalf("exactly 1 MiB rejected: %v", err)
	}
	if _, err := node.ParseEnvelope(append(data, ' ')); err == nil {
		t.Fatal("input over 1 MiB accepted")
	}
}
