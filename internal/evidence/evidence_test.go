package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/contract"
)

func TestVerifyPacket(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		contents   string
		lineStart  int
		lineEnd    int
		expected   string
		wantStatus string
		wantOK     bool
		wantMsg    string
		makePath   func(t *testing.T, root string) string
	}{
		{
			name:       "verified span",
			path:       "notes.txt",
			contents:   "alpha\nbeta\ngamma\n",
			lineStart:  1,
			lineEnd:    2,
			expected:   spanHash("alpha\nbeta\n"),
			wantStatus: "verified",
			wantOK:     true,
		},
		{
			name:       "mismatch",
			path:       "notes.txt",
			contents:   "alpha\nbeta\n",
			lineStart:  1,
			lineEnd:    2,
			expected:   strings.Repeat("0", sha256.Size*2),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "content hash mismatch",
		},
		{
			name:       "missing file",
			path:       "missing.txt",
			lineStart:  1,
			lineEnd:    1,
			expected:   strings.Repeat("0", sha256.Size*2),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "cannot access evidence path",
		},
		{
			name:       "invalid range",
			path:       "notes.txt",
			contents:   "alpha\nbeta\n",
			lineStart:  2,
			lineEnd:    1,
			expected:   spanHash("beta\n"),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "invalid line range",
		},
		{
			name:       "absolute path",
			path:       "/etc/hosts",
			lineStart:  1,
			lineEnd:    1,
			expected:   strings.Repeat("0", sha256.Size*2),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "repository-relative",
		},
		{
			name:       "parent path",
			path:       "../outside.txt",
			lineStart:  1,
			lineEnd:    1,
			expected:   strings.Repeat("0", sha256.Size*2),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "parent component",
		},
		{
			name:       "symlink escape",
			path:       "escape.txt",
			lineStart:  1,
			lineEnd:    1,
			expected:   strings.Repeat("0", sha256.Size*2),
			wantStatus: "failed",
			wantOK:     false,
			wantMsg:    "symlink",
			makePath: func(t *testing.T, root string) string {
				outside := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(root, "escape.txt")
				if err := os.Symlink(outside, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return link
			},
		},
		{
			name:       "CRLF normalization",
			path:       "notes.txt",
			contents:   "alpha\r\nbeta\r\n",
			lineStart:  1,
			lineEnd:    2,
			expected:   "sha256:" + spanHash("alpha\nbeta\n"),
			wantStatus: "verified",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.makePath == nil && tt.contents != "" {
				if err := os.WriteFile(filepath.Join(root, tt.path), []byte(tt.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if tt.makePath != nil {
				tt.makePath(t, root)
			}

			item := contract.Evidence{
				EvidenceID:     "evidence-1",
				Path:           tt.path,
				LineStart:      tt.lineStart,
				LineEnd:        tt.lineEnd,
				ContentHash:    tt.expected,
				VerifierStatus: "failed",
			}
			packet := contract.Packet{Evidence: []contract.Evidence{item}}
			report, err := VerifyPacket(root, packet)
			if err != nil {
				t.Fatal(err)
			}
			if report.Ok != tt.wantOK {
				t.Fatalf("Ok = %v, want %v", report.Ok, tt.wantOK)
			}
			if len(report.Results) != 1 {
				t.Fatalf("got %d results, want 1", len(report.Results))
			}
			result := report.Results[0]
			if result.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if tt.wantMsg != "" && !strings.Contains(result.Message, tt.wantMsg) {
				t.Fatalf("Message = %q, want substring %q", result.Message, tt.wantMsg)
			}
			if packet.Evidence[0].VerifierStatus != "failed" {
				t.Fatal("VerifyPacket mutated the packet evidence status")
			}
			if tt.wantStatus == "verified" && result.ActualHash != spanHash("alpha\nbeta\n") {
				t.Fatalf("ActualHash = %q, want %q", result.ActualHash, spanHash("alpha\nbeta\n"))
			}
		})
	}
}

func TestVerifyPacketEmptyEvidence(t *testing.T) {
	report, err := VerifyPacket(t.TempDir(), contract.Packet{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ok {
		t.Fatal("empty evidence should be OK")
	}
	if len(report.Results) != 0 || report.VerifiedCount != 0 || report.FailedCount != 0 || report.SkippedCount != 0 {
		t.Fatalf("unexpected empty report: %+v", report)
	}
}

func spanHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
