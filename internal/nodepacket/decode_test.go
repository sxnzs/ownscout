package nodepacket_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ownscout/internal/contract"
	"ownscout/internal/nodepacket"
)

func TestDecodeSuccess(t *testing.T) {
	packet := testPacket()
	valid := string(marshalJSON(t, packet))
	var reordered map[string]json.RawMessage
	if err := json.Unmarshal([]byte(valid), &reordered); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data string
	}{
		{"canonical fields", valid},
		{"whitespace", "\n\t " + valid + " \r\n"},
		{"reordered fields", string(marshalJSON(t, reordered))},
		{"escaped top key", replaceOne(t, valid, `"packet_id":`, `"packet_\u0069d":`)},
		{"escaped nested key", replaceOne(t, valid, `"content_hash":`, `"content_\u0068ash":`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDecoded(t, []byte(tt.data), packet)
		})
	}

	t.Run("same keys in separate evidence objects", func(t *testing.T) {
		packet := testPacket()
		second := packet.Evidence[0]
		second.EvidenceID = "e2"
		second.Path = "two.go"
		packet.Evidence = append(packet.Evidence, second)
		assertDecoded(t, marshalJSON(t, packet), packet)
	})
	t.Run("empty arrays remain nonnil", func(t *testing.T) {
		packet := testPacket()
		packet.Outcome = contract.OutcomePartial
		packet.Evidence = []contract.Evidence{}
		assertDecoded(t, marshalJSON(t, packet), packet)
	})
}

func TestDecodeExactFieldNames(t *testing.T) {
	packet := testPacket()
	valid := string(marshalJSON(t, packet))
	objects := []struct {
		path   string
		value  any
		fields []string
	}{
		{"packet", packet, []string{
			"packet_id", "schema_version", "repo_root", "head_commit", "request_id",
			"issued_at", "outcome", "freshness", "authorization", "budget",
			"evidence", "degradations", "provenance", "packet_hash",
		}},
		{"freshness", packet.Freshness, []string{
			"head_commit", "head_anchor", "status", "current", "is_current", "checked_at",
		}},
		{"authorization", packet.Authorization, []string{"level", "reason"}},
		{"budget", packet.Budget, []string{"max_evidence", "used_evidence", "max_bytes", "used_bytes"}},
		{"evidence[0]", packet.Evidence[0], []string{
			"evidence_id", "kind", "path", "commit", "line_start", "line_end",
			"source", "content_hash", "collected_at", "verifier_status",
		}},
		{"provenance", packet.Provenance, []string{"collector", "tool", "version", "tool_version"}},
	}
	for _, object := range objects {
		t.Run(object.path, func(t *testing.T) {
			original := string(marshalJSON(t, object.value))
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(original), &fields); err != nil {
				t.Fatal(err)
			}
			for _, field := range object.fields {
				t.Run(field, func(t *testing.T) {
					value, ok := fields[field]
					if !ok {
						t.Fatalf("test fixture lacks canonical field %q", field)
					}
					entry := `"` + field + `":` + string(value)
					alias := `"` + strings.ToUpper(field) + `":` + string(value)
					path := field
					if object.path != "packet" {
						path = object.path + "." + field
					}
					tests := []struct {
						name   string
						entry  string
						field  string
						reason string
					}{
						{"duplicate", entry + "," + entry, path, "duplicate key"},
						{"case alias", alias, object.path, "unknown field"},
						{"alias before canonical", alias + "," + entry, object.path, "unknown field"},
						{"alias after canonical", entry + "," + alias, object.path, "unknown field"},
					}
					for _, tt := range tests {
						t.Run(tt.name, func(t *testing.T) {
							changed := replaceOne(t, original, entry, tt.entry)
							data := replaceOne(t, valid, original, changed)
							assertRejected(t, []byte(data), tt.field, tt.reason)
						})
					}
				})
			}
			t.Run("unknown field", func(t *testing.T) {
				changed := replaceOne(t, original, "{", `{"extra":{"nested":true},`)
				data := replaceOne(t, valid, original, changed)
				assertRejected(t, []byte(data), object.path, "unknown field")
			})
		})
	}
}

func TestDecodeEscapedDuplicatesAndAliases(t *testing.T) {
	valid := string(marshalJSON(t, testPacket()))
	tests := []struct {
		name   string
		old    string
		new    string
		field  string
		reason string
	}{
		{"different duplicate value", `"packet_id":`, `"packet_id":"other","packet_id":`, "packet_id", "duplicate key"},
		{"escaped top duplicate", `"packet_id":`, `"packet_\u0069d":"other","packet_id":`, "packet_id", "duplicate key"},
		{"escaped second key", `"packet_id":`, `"packet_id":"other","packet_\u0069d":`, "packet_id", "duplicate key"},
		{"escaped nested duplicate", `"content_hash":`, `"content_\u0068ash":"other","content_hash":`, "evidence[0].content_hash", "duplicate key"},
		{"escaped case alias", `"schema_version":`, `"\u0053chema_version":`, "packet", "unknown field"},
		{"Unicode case alias", `"schema_version":`, `"\u017fchema_version":`, "packet", "unknown field"},
		{"Go field name", `"packet_id":`, `"PacketID":`, "packet", "unknown field"},
		{"nested Go field name", `"content_hash":`, `"ContentHash":`, "evidence[0]", "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := replaceOne(t, valid, tt.old, tt.new)
			assertRejected(t, []byte(data), tt.field, tt.reason)
		})
	}
}

func TestDecodeStrictBoundaries(t *testing.T) {
	valid := string(marshalJSON(t, testPacket()))
	tests := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"whitespace only", " \n\t"},
		{"null root", "null"},
		{"array root", "[]"},
		{"string root", `"packet"`},
		{"number root", "1"},
		{"boolean root", "true"},
		{"truncated object", valid[:len(valid)-1]},
		{"truncated key", `{"packet_id`},
		{"truncated value", `{"packet_id":"packet`},
		{"missing value", `{"packet_id":}`},
		{"missing colon", `{"packet_id" "packet-1"}`},
		{"unquoted key", `{packet_id:"packet-1"}`},
		{"leading zero", `{"budget":{"max_evidence":01}}`},
		{"leading plus", `{"budget":{"max_evidence":+1}}`},
		{"trailing comma", strings.TrimSuffix(valid, "}") + ",}"},
		{"trailing object", valid + "{}"},
		{"second packet", valid + "\n" + valid},
		{"trailing array", valid + " []"},
		{"trailing string", valid + ` "extra"`},
		{"trailing number", valid + " 1"},
		{"trailing boolean", valid + " true"},
		{"trailing null", valid + " null"},
		{"trailing malformed value", valid + " {"},
		{"trailing garbage", valid + " nope"},
		{"comment", valid + " // comment"},
		{"BOM", "\xef\xbb\xbf" + valid},
		{"invalid UTF-8", replaceOne(t, valid, "producer-hash", "\xff")},
		{"embedded control byte", replaceOne(t, valid, "producer-hash", "\x00")},
		{"unexpected nesting", `{"freshness":{"head_commit":[[[[[[[]]]]]]]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRejected(t, []byte(tt.data), "", "")
		})
	}
}

func TestDecodeMalformedTypes(t *testing.T) {
	packet := testPacket()
	valid := string(marshalJSON(t, packet))
	integerFailures := []string{
		"null", `"1"`, "true", "[]", "{}", "1.5", "1.0", "1e0",
		"9223372036854775808", "-9223372036854775809", "1e9999",
	}
	intFailures := append([]string{strconv.FormatUint(uint64(^uint(0)>>1)+1, 10)}, integerFailures...)
	tests := []struct {
		field   string
		key     string
		value   any
		invalid []string
	}{
		{"packet_id", "packet_id", packet.PacketID, []string{"null", "1", "true", "[]", "{}"}},
		{"freshness", "freshness", packet.Freshness, []string{"null", `"current"`, "1", "false", "[]"}},
		{"authorization", "authorization", packet.Authorization, []string{"null", `"autonomous"`, "true", "[]"}},
		{"budget", "budget", packet.Budget, []string{"null", "1", "false", "[]"}},
		{"provenance", "provenance", packet.Provenance, []string{"null", `"collector"`, "true", "[]"}},
		{"evidence", "evidence", packet.Evidence, []string{"null", "{}", `""`, "false"}},
		{"degradations", "degradations", packet.Degradations, []string{"null", "{}", `""`, "false"}},
		{"freshness.current", "current", true, []string{"null", `"true"`, "0", "1", "[]", "{}"}},
		{"freshness.is_current", "is_current", true, []string{"null", `"false"`, "0", "[]", "{}"}},
		{"authorization.level", "level", packet.Authorization.Level, []string{"null", "1", "true", "[]", "{}"}},
		{"budget.max_evidence", "max_evidence", packet.Budget.MaxEvidence, intFailures},
		{"budget.max_bytes", "max_bytes", packet.Budget.MaxBytes, integerFailures},
		{"evidence[0].line_start", "line_start", packet.Evidence[0].LineStart, intFailures},
		{"evidence[0].content_hash", "content_hash", packet.Evidence[0].ContentHash, []string{"null", "1", "true", "[]", "{}"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			old := `"` + tt.key + `":` + string(marshalJSON(t, tt.value))
			for _, invalid := range tt.invalid {
				t.Run(invalid, func(t *testing.T) {
					data := replaceOne(t, valid, old, `"`+tt.key+`":`+invalid)
					assertRejected(t, []byte(data), tt.field, "")
				})
			}
		})
	}
	for _, invalid := range []string{"null", "1", "false", `""`, "[]"} {
		t.Run("evidence element "+invalid, func(t *testing.T) {
			data := replaceOne(t, valid, string(marshalJSON(t, packet.Evidence[0])), invalid)
			assertRejected(t, []byte(data), "evidence[0]", "")
		})
	}
	for _, invalid := range []string{"null", "1", "false", "{}", "[]"} {
		t.Run("degradation element "+invalid, func(t *testing.T) {
			data := replaceOne(t, valid, `"degradations":[]`, `"degradations":[`+invalid+`]`)
			assertRejected(t, []byte(data), "degradations[0]", "")
		})
	}
}

func TestDecodeUnicodeAndHashPreservation(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want string
	}{
		{"opaque hash", `" \tSHA256:AbCdEf\n"`, " \tSHA256:AbCdEf\n"},
		{"escaped hash", `"\u0041b\u0043"`, "AbC"},
		{"surrogate pair", `"hash-\ud83e\udd89"`, "hash-🦉"},
		{"literal Unicode", `"hash-é-�"`, "hash-é-�"},
		{"escaped backslash", `"\\ud800"`, `\ud800`},
		{"escaped quote", `"hash-\"\\uDC00"`, `hash-"\uDC00`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			valid := string(marshalJSON(t, packet))
			data := replaceOne(t, valid, `"packet_hash":"producer-hash"`, `"packet_hash":`+tt.wire)
			data = replaceOne(t, data, `"content_hash":"evidence-hash"`, `"content_hash":`+tt.wire)
			packet.PacketHash = tt.want
			packet.Evidence[0].ContentHash = tt.want
			assertDecoded(t, []byte(data), packet)
		})
	}
	for _, invalid := range []string{
		`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, `"\ud800\ud800"`,
		`"\udfff\ud800"`, `"\ud800\\udc00"`, `"\u12"`, `"\uXXXX"`,
	} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			valid := string(marshalJSON(t, testPacket()))
			data := replaceOne(t, valid, `"packet_hash":"producer-hash"`, `"packet_hash":`+invalid)
			assertRejected(t, []byte(data), "", "")
		})
	}
}

func TestDecodeInputLimit(t *testing.T) {
	if nodepacket.MaxInputBytes != 1<<20 {
		t.Fatalf("MaxInputBytes = %d, want 1 MiB to match internal/node", nodepacket.MaxInputBytes)
	}
	for _, padding := range []string{"whitespace", "UTF-8 string"} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(padding+"/"+strconv.Itoa(delta), func(t *testing.T) {
				packet := testPacket()
				data := marshalJSON(t, packet)
				size := nodepacket.MaxInputBytes + delta
				if padding == "whitespace" {
					data = append(data, bytes.Repeat([]byte(" "), size-len(data))...)
				} else {
					length := size - len(data) + len(packet.PacketHash)
					packet.PacketHash = strings.Repeat("é", length/2) + strings.Repeat("x", length%2)
					data = marshalJSON(t, packet)
				}
				if len(data) != size {
					t.Fatalf("test input has %d bytes, want %d", len(data), size)
				}
				if delta > 0 {
					assertRejected(t, data, "packet", "input exceeds 1 MiB")
				} else {
					assertDecoded(t, data, packet)
				}
			})
		}
	}
}

func testPacket() contract.Packet {
	return contract.Packet{
		PacketID: "packet-1", SchemaVersion: contract.SchemaVersionV1,
		RepoRoot: "/repo", HeadCommit: "abc123", RequestID: "request-1",
		IssuedAt: "2026-09-05T00:00:00Z", Outcome: contract.OutcomeComplete,
		Freshness: contract.Freshness{
			HeadCommit: "abc123", HeadAnchor: "abc123", Status: "current",
			Current: true, IsCurrent: true, CheckedAt: "2026-09-05T00:00:00Z",
		},
		Authorization: contract.Authorization{Level: contract.AuthorizationAutonomous, Reason: "verified"},
		Budget:        contract.Budget{MaxEvidence: 10, UsedEvidence: 1, MaxBytes: 1024, UsedBytes: 10},
		Evidence: []contract.Evidence{{
			EvidenceID: "e1", Kind: "file", Path: "one.go", Commit: "abc123",
			LineStart: 1, LineEnd: 2, Source: "repository", ContentHash: "evidence-hash",
			CollectedAt: "2026-09-05T00:00:00Z", VerifierStatus: "verified",
		}},
		Degradations: []string{},
		Provenance: contract.Provenance{
			Collector: "collector", Tool: "compat-tool", Version: "1", ToolVersion: "compat-1",
		},
		PacketHash: "producer-hash",
	}
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func replaceOne(t *testing.T, data, old, replacement string) string {
	t.Helper()
	if !strings.Contains(data, old) {
		t.Fatalf("test fixture lacks %q", old)
	}
	return strings.Replace(data, old, replacement, 1)
}

func assertDecoded(t *testing.T, data []byte, want contract.Packet) {
	t.Helper()
	before := bytes.Clone(data)
	got, err := nodepacket.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("Decode changed packet fields or array order")
	}
	got, violations := nodepacket.DecodeValid(data)
	if len(violations) != 0 {
		t.Fatalf("DecodeValid violations: %+v", violations)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("DecodeValid changed packet fields or array order")
	}
	if !bytes.Equal(data, before) {
		t.Fatal("decoder mutated input bytes")
	}
}

func assertRejected(t *testing.T, data []byte, field, reason string) {
	t.Helper()
	before := bytes.Clone(data)
	got, err := nodepacket.Decode(data)
	if err == nil {
		t.Fatal("Decode accepted invalid input")
	}
	if !reflect.DeepEqual(got, contract.Packet{}) {
		t.Fatal("Decode returned a partial packet")
	}
	if reason != "" && !strings.Contains(err.Error(), reason) {
		t.Fatalf("Decode error = %v, want %q", err, reason)
	}
	got, violations := nodepacket.DecodeValid(data)
	if !reflect.DeepEqual(got, contract.Packet{}) {
		t.Fatal("DecodeValid returned a partial packet")
	}
	if len(violations) != 1 || violations[0].Rule != nodepacket.RuleDecode {
		t.Fatalf("wire failure should not trigger contract validation: %+v", violations)
	}
	if field != "" && violations[0].Field != field {
		t.Fatalf("violation field = %q, want %q", violations[0].Field, field)
	}
	if violations[0].Message != err.Error() {
		t.Fatalf("wire error and violation disagree: %v, %+v", err, violations)
	}
	if !bytes.Equal(data, before) {
		t.Fatal("decoder mutated rejected input bytes")
	}
}
