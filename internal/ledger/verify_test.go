package ledger

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReadOnlySummaryAndNoRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	root := t.TempDir()
	store, err := Open(path, root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Append(testHash('1'), testHash('2'), "0.1.0", validResults())
	if err != nil {
		t.Fatal(err)
	}

	// Verify must not need the repository or contend for the append lock. Keep
	// the store open while auditing, then append to prove the audit did not
	// modify or consume the ledger.
	summary, err := Verify(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Records != 1 || summary.Tip != first.RecordHash {
		t.Fatalf("summary = %+v, want one record headed by %s", summary, first.RecordHash)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Verify removed ledger: %v", err)
	}

	second, err := store.Append(testHash('3'), testHash('4'), "0.1.0", validResults())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	summary, err = Verify(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Records != 2 || summary.Tip != second.RecordHash {
		t.Fatalf("summary after append = %+v, want two records headed by %s", summary, second.RecordHash)
	}
}

func TestVerifyMissingDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")
	if _, err := Verify(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Verify error = %v, want not-exist error", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing ledger was created: stat error = %v", err)
	}
}

func TestVerifyReturnsValidationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.jsonl")
	if err := os.WriteFile(path, []byte("not a ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := Verify(path)
	if err == nil || summary.Records != 0 {
		t.Fatalf("Verify = %+v, %v; want validation failure", summary, err)
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || !strings.Contains(err.Error(), "validate ledger") {
		t.Fatalf("Verify error = %v, want ValidationError", err)
	}
}
