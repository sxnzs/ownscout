package nodepacket_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ownscout/internal/contract"
	"ownscout/internal/nodepacket"
)

func TestDecodeValidContractFixtures(t *testing.T) {
	for _, file := range []string{
		"valid.json", "invalid-missing-required.json", "invalid-schema.json",
		"invalid-outcome.json", "invalid-freshness.json", "invalid-budget.json",
		"invalid-degradations.json", "invalid-unverified-evidence.json",
		"invalid-forbidden-autonomy.json", "invalid-evidence.json",
	} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "contract", "testdata", file))
			if err != nil {
				t.Fatal(err)
			}
			assertContractValidation(t, data, file != "valid.json")
		})
	}
}

func TestDecodeValidMissingFieldsAreContractViolations(t *testing.T) {
	packet := testPacket()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(marshalJSON(t, packet), &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"packet_id", "schema_version", "repo_root", "head_commit", "request_id",
		"issued_at", "outcome", "freshness", "authorization", "budget",
		"evidence", "degradations", "provenance", "packet_hash",
	} {
		t.Run(field, func(t *testing.T) {
			saved := fields[field]
			delete(fields, field)
			data := marshalJSON(t, fields)
			fields[field] = saved
			assertContractValidation(t, data, true)
		})
	}
	for _, data := range []string{
		`{}`,
		`{"freshness":{},"authorization":{},"budget":{},"provenance":{}}`,
		`{"evidence":[{}],"degradations":[""]}`,
	} {
		t.Run(data, func(t *testing.T) {
			assertContractValidation(t, []byte(data), true)
		})
	}
}

func TestDecodeCompatibilityFields(t *testing.T) {
	tests := []struct {
		name       string
		freshness  string
		provenance string
	}{
		{"head_anchor", `{"head_anchor":"abc123","status":"current"}`, ""},
		{"current", `{"head_commit":"abc123","current":true}`, ""},
		{"is_current", `{"head_commit":"abc123","is_current":true}`, ""},
		{"false booleans", `{"head_commit":"abc123","status":"current","current":false,"is_current":false}`, ""},
		{"distinct booleans", `{"head_anchor":"abc123","current":false,"is_current":true}`, ""},
		{"fresh status", `{"head_commit":"abc123","status":" FRESH "}`, ""},
		{"tool and tool_version", "", `{"tool":"legacy-tool","tool_version":"2"}`},
		{"collector and tool_version", "", `{"collector":"collector","tool_version":"2"}`},
		{"tool and version", "", `{"tool":"legacy-tool","version":"2"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := testPacket()
			data := string(marshalJSON(t, packet))
			if tt.freshness != "" {
				data = replaceOne(t, data, string(marshalJSON(t, packet.Freshness)), tt.freshness)
				packet.Freshness = contract.Freshness{}
				if err := json.Unmarshal([]byte(tt.freshness), &packet.Freshness); err != nil {
					t.Fatal(err)
				}
			}
			if tt.provenance != "" {
				data = replaceOne(t, data, string(marshalJSON(t, packet.Provenance)), tt.provenance)
				packet.Provenance = contract.Provenance{}
				if err := json.Unmarshal([]byte(tt.provenance), &packet.Provenance); err != nil {
					t.Fatal(err)
				}
			}
			assertDecoded(t, []byte(data), packet)
		})
	}
	t.Run("conflicting anchors remain separate fields", func(t *testing.T) {
		packet := testPacket()
		packet.Freshness.HeadAnchor = "other"
		assertContractValidation(t, marshalJSON(t, packet), true)
	})
}

func TestDecodeIntegerPrecision(t *testing.T) {
	t.Run("integers above float64 precision", func(t *testing.T) {
		packet := testPacket()
		packet.Budget.MaxBytes = 1<<53 + 1
		packet.Budget.UsedBytes = packet.Budget.MaxBytes
		assertDecoded(t, marshalJSON(t, packet), packet)
	})
	t.Run("maximum integers", func(t *testing.T) {
		packet := testPacket()
		packet.Budget.MaxEvidence = int(^uint(0) >> 1)
		packet.Budget.UsedEvidence = packet.Budget.MaxEvidence
		packet.Budget.MaxBytes = 1<<63 - 1
		packet.Budget.UsedBytes = packet.Budget.MaxBytes
		packet.Evidence[0].LineStart = packet.Budget.MaxEvidence
		packet.Evidence[0].LineEnd = packet.Budget.MaxEvidence
		assertDecoded(t, marshalJSON(t, packet), packet)
	})
	t.Run("minimum integers are semantic violations", func(t *testing.T) {
		packet := testPacket()
		packet.Budget.MaxEvidence = -int(^uint(0)>>1) - 1
		packet.Budget.UsedEvidence = packet.Budget.MaxEvidence
		packet.Budget.MaxBytes = -1 << 63
		packet.Budget.UsedBytes = packet.Budget.MaxBytes
		packet.Evidence[0].LineStart = packet.Budget.MaxEvidence
		packet.Evidence[0].LineEnd = packet.Budget.MaxEvidence
		assertContractValidation(t, marshalJSON(t, packet), true)
	})
	t.Run("negative zero", func(t *testing.T) {
		packet := testPacket()
		data := replaceOne(t, string(marshalJSON(t, packet)), `"used_bytes":10`, `"used_bytes":-0`)
		packet.Budget.UsedBytes = 0
		assertDecoded(t, []byte(data), packet)
	})
}

func TestDecodeDoesNotRetainUntrustedErrorValues(t *testing.T) {
	const secret = "untrusted-secret-marker"
	for _, data := range []string{
		`{"` + secret + `":true}`,
		`{"budget":{"max_bytes":"` + secret + `"}}`,
		`{"evidence":[{"` + secret + `":true}]}`,
		`{"packet_hash":"` + secret + `" ` + secret + `}`,
		`{} ` + secret,
	} {
		t.Run(data, func(t *testing.T) {
			_, err := nodepacket.Decode([]byte(data))
			if err == nil {
				t.Fatal("malformed input accepted")
			}
			_, violations := nodepacket.DecodeValid([]byte(data))
			if strings.Contains(err.Error(), secret) || strings.Contains(string(marshalJSON(t, violations)), secret) {
				t.Fatal("boundary error retained untrusted input")
			}
		})
	}
}

func assertContractValidation(t *testing.T, data []byte, invalid bool) {
	t.Helper()
	var want contract.Packet
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("test fixture is not a typed packet: %v", err)
	}
	wantViolations := contract.ValidatePacket(want)
	if (len(wantViolations) != 0) != invalid {
		t.Fatalf("unexpected fixture validity: %+v", wantViolations)
	}
	got, err := nodepacket.Decode(data)
	if err != nil {
		t.Fatalf("contract violation conflated with wire error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("Decode changed the semantically invalid packet")
	}
	got, violations := nodepacket.DecodeValid(data)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("DecodeValid changed packet fields")
	}
	if !reflect.DeepEqual(violations, wantViolations) {
		t.Fatalf("violations = %+v, want exactly contract.ValidatePacket: %+v", violations, wantViolations)
	}
}
