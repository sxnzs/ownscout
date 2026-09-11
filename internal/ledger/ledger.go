// Package ledger provides a local, append-only JSONL record ledger.
package ledger

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const SchemaVersionV1 = "ownscout-ledger-v1"

const (
	maxLedgerSize  = 1 << 20
	maxRecordSize  = 64 << 10
	maxNodeResults = 4096
	zeroHash       = "0000000000000000000000000000000000000000000000000000000000000000"
)

type NodeResult struct {
	NodeID string `json:"node_id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Record struct {
	SchemaVersion       string       `json:"schema_version"`
	Seq                 uint64       `json:"seq"`
	PrevRecordHash      string       `json:"prev_record_hash"`
	RecordHash          string       `json:"record_hash"`
	EnvelopeSHA256      string       `json:"envelope_sha256"`
	PacketBindingSHA256 string       `json:"packet_binding_sha256"`
	OwnscoutVersion     string       `json:"ownscout_version"`
	NodeResults         []NodeResult `json:"node_results"`
}

type Store struct {
	mu       sync.Mutex
	file     *os.File
	nextSeq  uint64
	prevHash string
	closed   bool
	broken   bool
}

func Open(path, repoRoot string) (*Store, error) {
	resolvedRepo, err := resolveRepoRoot(repoRoot)
	if err != nil {
		return nil, err
	}

	ledgerPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve ledger path: %w", err)
	}
	ledgerPath = filepath.Clean(ledgerPath)
	if err := rejectSymlinkAncestors(ledgerPath); err != nil {
		return nil, err
	}

	var expectedFileInfo os.FileInfo
	if info, err := os.Lstat(ledgerPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ledger path %q is a symlink", ledgerPath)
		}
		expectedFileInfo = info
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect ledger %q: %w", ledgerPath, err)
	}
	resolvedLedgerPath, err := resolveLedgerPath(ledgerPath)
	if err != nil {
		return nil, err
	}
	if err := rejectLedgerInsideRepo(resolvedLedgerPath, resolvedRepo); err != nil {
		return nil, err
	}

	parentDir, err := os.Open(filepath.Dir(ledgerPath))
	if err != nil {
		return nil, fmt.Errorf("open ledger parent %q: %w", filepath.Dir(ledgerPath), err)
	}
	parentInfo, err := parentDir.Stat()
	if err != nil {
		_ = parentDir.Close()
		return nil, fmt.Errorf("stat ledger parent %q: %w", filepath.Dir(ledgerPath), err)
	}
	defer parentDir.Close()

	file, err := os.OpenFile(ledgerPath, os.O_RDWR|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open ledger %q: %w", ledgerPath, err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = unlockFile(file)
			_ = file.Close()
		}
	}()

	if err := lockFile(file); err != nil {
		return nil, fmt.Errorf("lock ledger %q: %w", ledgerPath, err)
	}
	if err := rejectOpenedFile(ledgerPath, resolvedLedgerPath, resolvedRepo, expectedFileInfo, parentInfo, file); err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat ledger %q: %w", ledgerPath, err)
	}
	if info.Size() > maxLedgerSize {
		return nil, fmt.Errorf("ledger exceeds %d bytes", maxLedgerSize)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek ledger %q: %w", ledgerPath, err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read ledger %q: %w", ledgerPath, err)
	}
	if len(data) > maxLedgerSize {
		return nil, fmt.Errorf("ledger exceeds %d bytes", maxLedgerSize)
	}

	last, count, err := validateLedger(data)
	if err != nil {
		return nil, fmt.Errorf("validate ledger %q: %w", ledgerPath, err)
	}

	store := &Store{file: file, nextSeq: uint64(count) + 1, prevHash: zeroHash}
	if count != 0 {
		store.prevHash = last.RecordHash
	}
	closeOnError = false
	return store, nil
}

func (s *Store) Append(envelopeSHA256, packetBindingSHA256, ownscoutVersion string, results []NodeResult) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return Record{}, errors.New("ledger is closed")
	}
	if s.broken {
		return Record{}, errors.New("ledger is unusable after a previous append failure")
	}
	if err := validateSHA256("envelope_sha256", envelopeSHA256); err != nil {
		return Record{}, err
	}
	if err := validateSHA256("packet_binding_sha256", packetBindingSHA256); err != nil {
		return Record{}, err
	}
	if ownscoutVersion == "" {
		return Record{}, errors.New("ownscout_version must be non-empty")
	}
	if containsNUL(ownscoutVersion) {
		return Record{}, errors.New("ownscout_version contains a NUL byte")
	}
	if len(results) == 0 {
		return Record{}, errors.New("node_results must be non-empty")
	}
	if len(results) > maxNodeResults {
		return Record{}, fmt.Errorf("node_results exceeds %d results", maxNodeResults)
	}
	copiedResults := append([]NodeResult(nil), results...)

	record := Record{
		SchemaVersion:       SchemaVersionV1,
		Seq:                 s.nextSeq,
		PrevRecordHash:      s.prevHash,
		RecordHash:          zeroHash,
		EnvelopeSHA256:      envelopeSHA256,
		PacketBindingSHA256: packetBindingSHA256,
		OwnscoutVersion:     ownscoutVersion,
		NodeResults:         copiedResults,
	}
	if err := validateRecord(record, s.nextSeq, s.prevHash); err != nil {
		return Record{}, err
	}

	hash, err := hashRecord(record)
	if err != nil {
		return Record{}, err
	}
	record.RecordHash = hash
	encoded, err := json.Marshal(record)
	if err != nil {
		return Record{}, fmt.Errorf("marshal ledger record: %w", err)
	}
	if len(encoded) > maxRecordSize {
		return Record{}, fmt.Errorf("record exceeds %d bytes", maxRecordSize)
	}
	encoded = append(encoded, '\n')

	info, err := s.file.Stat()
	if err != nil {
		s.broken = true
		return Record{}, fmt.Errorf("stat ledger before append: %w", err)
	}
	if info.Size() < 0 || info.Size() > maxLedgerSize || int64(len(encoded)) > maxLedgerSize-info.Size() {
		return Record{}, fmt.Errorf("ledger exceeds %d bytes", maxLedgerSize)
	}

	if err := writeAll(s.file, encoded); err != nil {
		s.broken = true
		return Record{}, fmt.Errorf("append ledger record: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		s.broken = true
		return Record{}, fmt.Errorf("sync ledger: %w", err)
	}
	s.nextSeq++
	s.prevHash = hash
	return record, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	unlockErr := unlockFile(s.file)
	closeErr := s.file.Close()
	return errors.Join(unlockErr, closeErr)
}

func resolveRepoRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve repository path %q: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat repository path %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository path %q is not a directory", path)
	}
	return filepath.Clean(resolved), nil
}

func rejectSymlinkAncestors(path string) error {
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, volume)
	current := volume
	if strings.HasPrefix(rest, string(filepath.Separator)) {
		current += string(filepath.Separator)
		rest = strings.TrimPrefix(rest, string(filepath.Separator))
	}
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect ledger path ancestor %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if permittedSystemSymlink(current) {
				continue
			}
			return fmt.Errorf("ledger path %q has symlink ancestor %q", path, current)
		}
	}
	return nil
}

func permittedSystemSymlink(path string) bool {
	if filepath.Clean(path) != string(filepath.Separator)+"var" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && filepath.Clean(resolved) == string(filepath.Separator)+"private"+string(filepath.Separator)+"var"
}

func isPathPrefix(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func rejectLedgerInsideRepo(ledgerPath, repoPath string) error {
	rel, err := filepath.Rel(repoPath, ledgerPath)
	if err != nil {
		return fmt.Errorf("compare ledger and repository paths: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("ledger path %q is inside repository %q", ledgerPath, repoPath)
	}
	return nil
}

func resolveLedgerPath(path string) (string, error) {
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("resolve ledger path %q: %w", path, err)
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func rejectOpenedFile(path, expectedResolvedPath, resolvedRepo string, expectedFileInfo, expectedParentInfo os.FileInfo, file *os.File) error {
	if err := rejectSymlinkAncestors(path); err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect ledger %q after opening: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ledger path %q is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ledger %q is not a regular file", path)
	}
	if expectedFileInfo != nil && !os.SameFile(expectedFileInfo, info) {
		return fmt.Errorf("ledger %q changed while opening", path)
	}

	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat ledger %q: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() {
		return fmt.Errorf("ledger %q is not a regular file", path)
	}
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("ledger %q changed while opening", path)
	}
	parentPath := filepath.Dir(path)
	currentParentInfo, err := os.Stat(parentPath)
	if err != nil {
		return fmt.Errorf("stat ledger parent %q after opening: %w", parentPath, err)
	}
	if !os.SameFile(expectedParentInfo, currentParentInfo) {
		return fmt.Errorf("ledger parent %q changed while opening", parentPath)
	}
	stat, ok := openedInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Nlink) != 1 {
		return fmt.Errorf("ledger %q must have link count 1", path)
	}

	resolvedPath, err := resolveLedgerPath(path)
	if err != nil {
		return err
	}
	if resolvedPath != expectedResolvedPath {
		return fmt.Errorf("ledger path %q changed while opening", path)
	}
	if err := rejectLedgerInsideRepo(resolvedPath, resolvedRepo); err != nil {
		return err
	}
	return nil
}

func lockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func validateLedger(data []byte) (Record, int, error) {
	if len(data) != 0 && data[len(data)-1] != '\n' {
		return Record{}, 0, errors.New("nonempty ledger must end with LF")
	}

	var last Record
	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), maxRecordSize+1)
	for scanner.Scan() {
		line := scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			return Record{}, 0, errors.New("empty or blank line")
		}
		record, err := decodeRecord(trimmed)
		if err != nil {
			return Record{}, 0, fmt.Errorf("line %d: %w", count+1, err)
		}
		expectedSeq := uint64(count + 1)
		expectedPrev := zeroHash
		if count != 0 {
			expectedPrev = last.RecordHash
		}
		if err := validateRecord(record, expectedSeq, expectedPrev); err != nil {
			return Record{}, 0, fmt.Errorf("line %d: %w", count+1, err)
		}
		expectedHash, err := hashRecord(record)
		if err != nil {
			return Record{}, 0, fmt.Errorf("line %d: %w", count+1, err)
		}
		if record.RecordHash != expectedHash {
			return Record{}, 0, fmt.Errorf("line %d: record_hash does not match canonical record", count+1)
		}
		last = record
		count++
	}
	if err := scanner.Err(); err != nil {
		return Record{}, 0, fmt.Errorf("read line: %w", err)
	}
	return last, count, nil
}

func decodeRecord(data []byte) (Record, error) {
	if err := validateJSONObject(data); err != nil {
		return Record{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("malformed record: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Record{}, fmt.Errorf("trailing data: %w", err)
	}
	return record, nil
}

func validateJSONObject(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("malformed JSON: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("record must be a JSON object")
	}
	if err := consumeObject(decoder, recordFields); err != nil {
		return fmt.Errorf("malformed JSON: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	return nil
}

func consumeValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		return consumeObject(decoder, nil)
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("invalid array closing delimiter")
		}
		return nil
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
}

func consumeObject(decoder *json.Decoder, allowed map[string]struct{}) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("object key is not a string")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate JSON field %q", key)
		}
		seen[key] = struct{}{}
		if allowed != nil {
			if _, ok := allowed[key]; !ok {
				if canonical, ok := caseInsensitiveField(key, allowed); ok {
					return fmt.Errorf("non-canonical JSON field %q; use %q", key, canonical)
				}
				return fmt.Errorf("unknown JSON field %q", key)
			}
		}

		if allowed != nil && key == "node_results" {
			err = consumeNodeResults(decoder)
		} else {
			err = consumeValue(decoder)
		}
		if err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != json.Delim('}') {
		return errors.New("invalid object closing delimiter")
	}
	return nil
}

func consumeNodeResults(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return errors.New("node_results must be an array")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); !ok || delim != '{' {
			return errors.New("node result must be a JSON object")
		}
		if err := consumeObject(decoder, nodeResultFields); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != json.Delim(']') {
		return errors.New("invalid array closing delimiter")
	}
	return nil
}

func caseInsensitiveField(key string, allowed map[string]struct{}) (string, bool) {
	for canonical := range allowed {
		if strings.EqualFold(key, canonical) {
			return canonical, true
		}
	}
	return "", false
}

var (
	recordFields = map[string]struct{}{
		"schema_version":        {},
		"seq":                   {},
		"prev_record_hash":      {},
		"record_hash":           {},
		"envelope_sha256":       {},
		"packet_binding_sha256": {},
		"ownscout_version":      {},
		"node_results":          {},
	}
	nodeResultFields = map[string]struct{}{
		"node_id": {},
		"status":  {},
		"reason":  {},
	}
)

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateRecord(record Record, expectedSeq uint64, expectedPrev string) error {
	if record.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("schema_version must be %q", SchemaVersionV1)
	}
	if record.Seq != expectedSeq {
		return fmt.Errorf("seq %d does not follow expected sequence %d", record.Seq, expectedSeq)
	}
	if record.PrevRecordHash != expectedPrev {
		return errors.New("prev_record_hash does not match hash chain")
	}
	if err := validateSHA256("prev_record_hash", record.PrevRecordHash); err != nil {
		return err
	}
	if err := validateSHA256("record_hash", record.RecordHash); err != nil {
		return err
	}
	if err := validateSHA256("envelope_sha256", record.EnvelopeSHA256); err != nil {
		return err
	}
	if err := validateSHA256("packet_binding_sha256", record.PacketBindingSHA256); err != nil {
		return err
	}
	if record.OwnscoutVersion == "" {
		return errors.New("ownscout_version must be non-empty")
	}
	if len(record.NodeResults) == 0 {
		return errors.New("node_results must be non-empty")
	}
	if len(record.NodeResults) > maxNodeResults {
		return fmt.Errorf("node_results exceeds %d results", maxNodeResults)
	}
	seen := make(map[string]struct{}, len(record.NodeResults))
	for i, result := range record.NodeResults {
		if containsNUL(record.SchemaVersion) || containsNUL(record.PrevRecordHash) || containsNUL(record.RecordHash) || containsNUL(record.EnvelopeSHA256) || containsNUL(record.PacketBindingSHA256) || containsNUL(record.OwnscoutVersion) {
			return errors.New("record contains a NUL byte")
		}
		if err := validateNodeResult(result); err != nil {
			return fmt.Errorf("node_results[%d]: %w", i, err)
		}
		if _, exists := seen[result.NodeID]; exists {
			return fmt.Errorf("duplicate node_id %q", result.NodeID)
		}
		seen[result.NodeID] = struct{}{}
	}
	return nil
}

func validateNodeResult(result NodeResult) error {
	if err := validateIdentifier(result.NodeID); err != nil {
		return fmt.Errorf("node_id: %w", err)
	}
	switch result.Status {
	case "evidence_current", "failed", "blocked":
	default:
		return fmt.Errorf("status %q is invalid", result.Status)
	}
	if containsNUL(result.NodeID) || containsNUL(result.Status) || containsNUL(result.Reason) {
		return errors.New("contains a NUL byte")
	}
	return nil
}

func validateIdentifier(value string) error {
	if len(value) < 1 || len(value) > 128 {
		return errors.New("must contain 1-128 ASCII identifier characters")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if i == 0 {
			if !alphanumeric {
				return errors.New("must start with an ASCII alphanumeric character")
			}
			continue
		}
		if !alphanumeric && c != '.' && c != '_' && c != '-' && c != ':' {
			return errors.New("contains an invalid character")
		}
	}
	return nil
}

func validateSHA256(field, value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%s must be exactly 64 lowercase hexadecimal characters", field)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("%s must be exactly 64 lowercase hexadecimal characters", field)
		}
	}
	return nil
}

func containsNUL(value string) bool {
	return strings.IndexByte(value, 0) >= 0
}

func hashRecord(record Record) (string, error) {
	record.RecordHash = ""
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("marshal canonical record: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func writeAll(file *os.File, data []byte) error {
	for len(data) != 0 {
		written, err := file.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
