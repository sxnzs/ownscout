package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ownscout/internal/evidence"
)

func TestStrictPacketBoundaryModes(t *testing.T) {
	valid, err := os.ReadFile(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, input, want string
	}{
		// Every shape fails at the single strict boundary with the same generic
		// report; packet contents and decode internals never leak into output.
		{"malformed", `{"packet_id":`, "strict packet decoding failed"},
		{"null", `null`, "strict packet decoding failed"},
		{"array", `[]`, "strict packet decoding failed"},
		{"scalar", `"DO_NOT_PRINT"`, "strict packet decoding failed"},
		{"trailing value", string(valid) + `{}`, "strict packet decoding failed"},
		{"unknown root field", strings.Replace(string(valid), `"packet_id":`, `"unexpected": "DO_NOT_PRINT", "packet_id":`, 1), "strict packet decoding failed"},
		{"unknown nested field", strings.Replace(string(valid), `"kind":`, `"unexpected": "DO_NOT_PRINT", "kind":`, 1), "strict packet decoding failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "packet.json")
			if err := os.WriteFile(path, []byte(tt.input), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"contract", "evidence"} {
				t.Run(command, func(t *testing.T) {
					args := []string{"contract", "validate", "--packet", path}
					if command == "evidence" {
						args = []string{"evidence", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", path}
					}
					assertBoundaryModes(t, args, 2, tt.want)
				})
			}
		})
	}
}

func TestEvidenceBoundaryModes(t *testing.T) {
	tests := []struct {
		name, path, contents, linkName, linkTarget, want string
		code                                             int
	}{
		{name: "valid", path: "notes.txt", contents: "alpha\nbeta\n", code: 0, want: "verified 1 evidence span(s)"},
		{name: "stale content", path: "notes.txt", contents: "alpha\nchanged\n", code: 1, want: "content hash mismatch"},
		{name: "missing file", path: "missing.txt", code: 1, want: "cannot access evidence path"},
		{name: "absolute path", path: "/outside.txt", code: 1, want: "repository-relative"},
		{name: "parent path", path: "../outside.txt", code: 1, want: "parent component"},
		{name: "symlink file inside repo", path: "linked.txt", contents: "alpha\nbeta\n", linkName: "linked.txt", linkTarget: "notes.txt", code: 1, want: "symlink"},
		{name: "symlink directory inside repo", path: "linked/notes.txt", contents: "alpha\nbeta\n", linkName: "linked", linkTarget: ".", code: 1, want: "symlink"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.linkName != "" {
				if err := os.Symlink(tt.linkTarget, filepath.Join(root, tt.linkName)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			packetPath := writePacketVariant(t, func(raw map[string]json.RawMessage) {
				var items []map[string]json.RawMessage
				if err := json.Unmarshal(raw["evidence"], &items); err != nil {
					t.Fatal(err)
				}
				items[0]["path"], _ = json.Marshal(tt.path)
				raw["evidence"], _ = json.Marshal(items)
			})
			wants := []string{tt.want}
			if tt.code == 1 {
				wants = append(wants, `evidence "evidence-1"`, tt.path)
			}
			assertBoundaryModes(t, []string{"evidence", "verify", "--repo", root, "--packet", packetPath}, tt.code, wants...)
		})
	}
}

func TestDegradationBoundaryModes(t *testing.T) {
	for _, outcome := range []string{"complete", "partial_degraded"} {
		t.Run(outcome, func(t *testing.T) {
			path := writePacketVariant(t, func(raw map[string]json.RawMessage) {
				raw["outcome"], _ = json.Marshal(outcome)
				raw["degradations"] = json.RawMessage(`["limited scope"]`)
			})
			code := 0
			want := "default action: autonomous_proceed"
			if outcome == "complete" {
				code = 1
				want = "complete packet cannot contain degradations"
			}
			assertBoundaryModes(t, []string{"contract", "validate", "--packet", path}, code, want)
			if code == 0 {
				want = "verified 1 evidence span(s)"
			}
			assertBoundaryModes(t, []string{"evidence", "verify", "--repo", filepath.Join("testdata", "repo"), "--packet", path}, code, want)
		})
	}
}

func TestEvidenceIssuesIdentifyNonverifiedSpans(t *testing.T) {
	report := evidence.Report{
		FailedCount:  1,
		SkippedCount: 1,
		Results: []evidence.Verification{
			{EvidenceID: "good", Path: "good.txt", Status: "verified"},
			{EvidenceID: "bad", Path: "bad.txt", Status: "failed", Message: "content hash mismatch"},
			{EvidenceID: "skip", Path: "skip.txt", Status: "skipped"},
		},
	}
	want := []string{
		`evidence "bad" ("bad.txt"): content hash mismatch`,
		`evidence "skip" ("skip.txt"): status: skipped`,
	}
	if got := evidenceIssues(report); !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %q, want %q", got, want)
	}
}

func assertBoundaryModes(t *testing.T, args []string, wantCode int, wants ...string) {
	t.Helper()
	for _, mode := range []string{"human", "json"} {
		t.Run(mode, func(t *testing.T) {
			modeArgs := append([]string(nil), args...)
			if mode == "json" {
				modeArgs = append(modeArgs, "--json")
			}
			code, output := runTest(t, modeArgs...)
			if code != wantCode {
				t.Fatalf("code=%d, want %d; output=%q", code, wantCode, output)
			}
			if strings.Contains(output, "DO_NOT_PRINT") {
				t.Fatalf("packet value leaked: %q", output)
			}
			text := output
			if mode == "json" {
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
				for _, key := range []string{"command", "ok", "summary", "details", "next_action"} {
					if _, ok := fields[key]; !ok {
						t.Fatalf("missing JSON field %q", key)
					}
				}
				var data resultData
				if err := json.Unmarshal([]byte(output), &data); err != nil {
					t.Fatal(err)
				}
				if data.OK != (wantCode == 0) || data.Command != strings.Join(args[:2], " ") || data.Summary == "" || data.NextAction == "" || len(data.Details) == 0 {
					t.Fatalf("unexpected JSON result: %+v", data)
				}
				text = data.Summary + "\n" + strings.Join(data.Details, "\n") + "\n" + data.NextAction
			} else if !strings.Contains(output, "Next action:") {
				t.Fatalf("missing human next action: %q", output)
			}
			for _, want := range wants {
				if !strings.Contains(text, want) {
					t.Errorf("output %q does not contain %q", text, want)
				}
			}
		})
	}
}
