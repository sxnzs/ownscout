package ledger

import (
	"errors"
	"fmt"
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

// Unwrap is never reached by the corpus: both call sites in internal/cli pass
// the error straight out of Verify or Rotate to errors.As, which matches the
// concrete *ValidationError without walking a chain. It stays because that is
// only true as long as nothing wraps it, and the CLI's error classification
// depends on staying able to see through a wrapper. This pins both halves so
// the method cannot be removed as "dead" without this failing.
func TestValidationErrorUnwrapSurvivesWrapping(t *testing.T) {
	inner := &ValidationError{Err: errors.New("bad chain")}

	var direct *ValidationError
	if !errors.As(inner, &direct) {
		t.Fatal("errors.As did not match an unwrapped ValidationError")
	}

	var wrapped *ValidationError
	if !errors.As(fmt.Errorf("rotate %q: %w", "ledger.jsonl", inner), &wrapped) {
		t.Fatal("errors.As did not unwrap to ValidationError; the CLI would misreport this as an I/O error")
	}
	if wrapped != inner {
		t.Fatalf("errors.As unwrapped to %p, want %p", wrapped, inner)
	}
	if got := errors.Unwrap(inner); got == nil || got.Error() != "bad chain" {
		t.Fatalf("Unwrap = %v, want the wrapped cause", got)
	}
}
