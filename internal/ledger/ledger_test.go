package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreationAndAppendContinuity(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(t.TempDir(), "ledger.jsonl")
	s, err := Open(ledgerPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.Stat(ledgerPath); err != nil {
		t.Fatal(err)
	} else if got.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", got.Mode().Perm())
	}
	first, err := s.Append(testHash('1'), testHash('2'), "0.1.0", []NodeResult{{NodeID: "build", Status: "evidence_current"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append(testHash('3'), testHash('4'), "0.1.0", []NodeResult{{NodeID: "deploy:prod", Status: "blocked", Reason: "approval"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 || first.PrevRecordHash != zeroHash {
		t.Fatalf("first record = %+v", first)
	}
	if second.Seq != 2 || second.PrevRecordHash != first.RecordHash {
		t.Fatalf("second record = %+v", second)
	}
	if first.RecordHash != mustRecordHash(first) || second.RecordHash != mustRecordHash(second) {
		t.Fatal("record hash does not match canonical record")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := s.Append(testHash('5'), testHash('6'), "0.1.0", validResults()); err == nil {
		t.Fatal("append after close succeeded")
	}

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte{'\n'}) != 2 || !bytes.HasSuffix(data, []byte{'\n'}) {
		t.Fatalf("ledger bytes = %q", data)
	}
	if reopened, err := Open(ledgerPath, root); err != nil {
		t.Fatal(err)
	} else {
		_ = reopened.Close()
	}
}

func TestAppendCopiesCallerResults(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(t.TempDir(), "ledger"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	results := []NodeResult{{NodeID: "a", Status: "failed", Reason: "before"}}
	record, err := s.Append(testHash('1'), testHash('2'), "v", results)
	if err != nil {
		t.Fatal(err)
	}
	results[0].NodeID = "changed"
	results[0].Reason = "changed"
	if record.NodeResults[0].NodeID != "a" || record.NodeResults[0].Reason != "before" {
		t.Fatalf("record changed by caller mutation: %+v", record)
	}
}

func TestOpenRejectsInvalidLedgerLines(t *testing.T) {
	valid := mustJSON(validRecord(1, zeroHash, testHash('1'), testHash('2'), "v", validResults()))
	tests := []struct {
		name string
		data string
		want string
	}{
		{"malformed JSON", `{"schema_version":`, "malformed"},
		{"unknown field", strings.Replace(valid, `"seq":1`, `"extra":1,"seq":1`, 1), "unknown"},
		{"duplicate field", strings.Replace(valid, `"seq":1`, `"seq":1,"seq":1`, 1), "duplicate"},
		{"non-canonical field alias", strings.Replace(valid, `"seq":1`, `"Seq":1`, 1), "non-canonical"},
		{"bad sequence", strings.Replace(valid, `"seq":1`, `"seq":2`, 1), "seq"},
		{"bad hash chain", strings.Replace(valid, `"prev_record_hash":"`+zeroHash+`"`, `"prev_record_hash":"`+testHash('9')+`"`, 1), "prev_record_hash"},
		{"trailing data", valid + "{}", "trailing"},
		{"blank line", valid + "\n\n", "blank"},
		{"missing final LF", strings.TrimSuffix(valid, "\n"), "end with LF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(t.TempDir(), "ledger")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("Open error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeRecordRejectsInvalidNestedJSONKeys(t *testing.T) {
	valid := mustJSON(validRecord(1, zeroHash, testHash('1'), testHash('2'), "v", []NodeResult{{
		NodeID: "node",
		Status: "failed",
		Reason: "because",
	}}))
	tests := []struct {
		name string
		data string
		want string
	}{
		{"non-canonical nested field alias", strings.Replace(valid, `"node_id":"node"`, `"Node_ID":"node"`, 1), "non-canonical"},
		{"unknown nested field", strings.Replace(valid, `"status":"failed"`, `"extra":true,"status":"failed"`, 1), "unknown"},
		{"duplicate nested field", strings.Replace(valid, `"node_id":"node"`, `"node_id":"node","node_id":"node"`, 1), "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeRecord([]byte(strings.TrimSuffix(tt.data, "\n"))); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("decodeRecord error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAppendRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*string, *string, *string, *[]NodeResult)
		want   string
	}{
		{"invalid envelope hash", func(a, _, _ *string, _ *[]NodeResult) { *a = strings.Repeat("A", 64) }, "envelope_sha256"},
		{"invalid packet hash", func(_, b, _ *string, _ *[]NodeResult) { *b = strings.Repeat("g", 64) }, "packet_binding_sha256"},
		{"empty version", func(_, _, v *string, _ *[]NodeResult) { *v = "" }, "ownscout_version"},
		{"nil results", func(_, _, _ *string, r *[]NodeResult) { *r = nil }, "node_results"},
		{"empty results", func(_, _, _ *string, r *[]NodeResult) { *r = []NodeResult{} }, "node_results"},
		{"invalid status", func(_, _, _ *string, r *[]NodeResult) { (*r)[0].Status = "pending" }, "status"},
		{"invalid node ID", func(_, _, _ *string, r *[]NodeResult) { (*r)[0].NodeID = ".bad" }, "node_id"},
		{"duplicate node ID", func(_, _, _ *string, r *[]NodeResult) { *r = append(*r, (*r)[0]) }, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "ledger"), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			a, b, v, r := testHash('1'), testHash('2'), "v", validResults()
			tt.mutate(&a, &b, &v, &r)
			if _, err := s.Append(a, b, v, r); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Append error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOversizedLedgerAndRecord(t *testing.T) {
	t.Run("ledger", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(t.TempDir(), "ledger")
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, maxLedgerSize+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path, root); err == nil || !strings.Contains(err.Error(), "ledger exceeds") {
			t.Fatalf("Open error = %v", err)
		}
	})
	t.Run("record", func(t *testing.T) {
		s, err := Open(filepath.Join(t.TempDir(), "ledger"), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		results := []NodeResult{{NodeID: "node", Status: "failed", Reason: strings.Repeat("x", maxRecordSize)}}
		if _, err := s.Append(testHash('1'), testHash('2'), "v", results); err == nil || !strings.Contains(err.Error(), "record exceeds") {
			t.Fatalf("Append error = %v", err)
		}
	})
}

func TestAppendRejectsLedgerCeilingIncludingNewline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "ledger")
	s, err := Open(path, root)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()
	results := []NodeResult{{NodeID: "node", Status: "failed", Reason: strings.Repeat("x", 63000)}}
	for {
		if _, err := s.Append(testHash('1'), testHash('2'), "v", results); err != nil {
			if !strings.Contains(err.Error(), "ledger exceeds") {
				t.Fatalf("Append error = %v, want ledger ceiling error", err)
			}
			break
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxLedgerSize {
		t.Fatalf("ledger size = %d, exceeds %d", info.Size(), maxLedgerSize)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, root)
	if err != nil {
		t.Fatalf("Open after ceiling rejection: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerPathSafety(t *testing.T) {
	t.Run("inside repository", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Open(filepath.Join(root, "ledger"), root); err == nil || !strings.Contains(err.Error(), "inside repository") {
			t.Fatalf("Open error = %v", err)
		}
	})
	t.Run("final symlink", func(t *testing.T) {
		root := t.TempDir()
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "ledger")
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Open(path, root); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Open error = %v", err)
		}
	})
	t.Run("ancestor symlink", func(t *testing.T) {
		root := t.TempDir()
		dir := t.TempDir()
		target := t.TempDir()
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Open(filepath.Join(link, "ledger"), root); err == nil || !strings.Contains(err.Error(), "symlink ancestor") {
			t.Fatalf("Open error = %v, want symlink ancestor rejection", err)
		}
	})
}

func validResults() []NodeResult {
	return []NodeResult{{NodeID: "node", Status: "evidence_current"}}
}

func validRecord(seq uint64, prev, envelope, binding, version string, results []NodeResult) Record {
	return Record{
		SchemaVersion:       SchemaVersionV1,
		Seq:                 seq,
		PrevRecordHash:      prev,
		RecordHash:          testHash('0'),
		EnvelopeSHA256:      envelope,
		PacketBindingSHA256: binding,
		OwnscoutVersion:     version,
		NodeResults:         results,
	}
}

func mustRecordHash(record Record) string {
	hash, err := hashRecord(record)
	if err != nil {
		panic(err)
	}
	return hash
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func testHash(char byte) string {
	return hex.EncodeToString(bytes.Repeat([]byte{char}, sha256.Size))
}
