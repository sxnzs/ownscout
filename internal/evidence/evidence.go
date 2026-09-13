// Package evidence verifies packet evidence against the current working tree.
package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ownscout/internal/contract"
)

// Verification is the result of checking one evidence span.
type Verification struct {
	EvidenceID   string `json:"evidence_id"`
	Path         string `json:"path"`
	Status       string `json:"status"` // verified | failed | skipped
	ExpectedHash string `json:"expected_hash"`
	ActualHash   string `json:"actual_hash,omitempty"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`
	Message      string `json:"message,omitempty"`
}

// Report summarizes verification of all evidence in a packet.
type Report struct {
	RepoRoot      string         `json:"repo_root"`
	Ok            bool           `json:"ok"`
	Results       []Verification `json:"results"`
	VerifiedCount int            `json:"verified_count"`
	FailedCount   int            `json:"failed_count"`
	SkippedCount  int            `json:"skipped_count"`
}

// Options selects optional verification behaviour. The zero value is the
// historical, always-on behaviour: a span is verified, or it is not.
type Options struct {
	// Relocate re-resolves an evidence span whose content hash does not match
	// at the cited lines, and annotates the failure with where the same content
	// now lives. It is diagnostic only: relocation never changes a span's
	// status, the report counters, or the exit code.
	Relocate bool
}

// VerifyPacket verifies each evidence span against the repository's current
// working tree. The packet is treated as input only; in particular, its
// verifier_status fields are not updated or trusted.
func VerifyPacket(repoRoot string, packet contract.Packet) (Report, error) {
	return VerifyPacketWithOptions(repoRoot, packet, Options{})
}

// VerifyPacketWithOptions is VerifyPacket with optional behaviour enabled. See
// Options for the only option that exists.
func VerifyPacketWithOptions(repoRoot string, packet contract.Packet, options Options) (Report, error) {
	root, err := repositoryRoot(repoRoot)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		RepoRoot: root,
		Ok:       true,
		Results:  make([]Verification, 0, len(packet.Evidence)),
	}
	for _, item := range packet.Evidence {
		result := verifyOne(root, item, options)
		report.Results = append(report.Results, result)
		switch result.Status {
		case "verified":
			report.VerifiedCount++
		case "failed":
			report.FailedCount++
		case "skipped":
			report.SkippedCount++
		}
	}
	// A skipped span is not evidence that can be consumed.  Overall success
	// therefore requires every declared span to be verified.
	report.Ok = report.FailedCount == 0 && report.SkippedCount == 0
	return report, nil
}

func repositoryRoot(repoRoot string) (string, error) {
	if repoRoot == "" {
		return "", fmt.Errorf("repository root is empty")
	}

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root %q: %w", repoRoot, err)
	}
	root = filepath.Clean(root)

	// Resolve links in the root itself so that the path walk below starts from
	// a real directory. Symlinks in an evidence path are still rejected.
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root %q: %w", repoRoot, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("repository %q: %w", repoRoot, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository %q is not a directory", repoRoot)
	}
	return filepath.Clean(root), nil
}

func verifyOne(root string, item contract.Evidence, options Options) Verification {
	result := Verification{
		EvidenceID:   item.EvidenceID,
		Path:         item.Path,
		Status:       "failed",
		ExpectedHash: item.ContentHash,
		LineStart:    item.LineStart,
		LineEnd:      item.LineEnd,
	}

	path, err := safeEvidencePath(root, item.Path)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	data, err := readRegularFile(path)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	lines := countNormalizedLines(data)
	if item.LineStart < 1 || item.LineEnd < item.LineStart || item.LineEnd > lines {
		result.Message = fmt.Sprintf("invalid line range %d-%d for %d line(s)", item.LineStart, item.LineEnd, lines)
		result.Message += locationClause(data, lines, item.LineStart, item.LineEnd, item.ContentHash, options)
		return result
	}

	expected, err := normalizedExpectedHash(item.ContentHash)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	result.ActualHash = hashSelectedLines(data, item.LineStart, item.LineEnd)
	if result.ActualHash != expected {
		result.Message = fmt.Sprintf("content hash mismatch: expected %s, got %s", item.ContentHash, result.ActualHash)
		result.Message += locationClause(data, lines, item.LineStart, item.LineEnd, item.ContentHash, options)
		return result
	}

	result.Status = "verified"
	return result
}

// locationClause renders the relocation diagnostic for a failed span, or "" when
// relocation is disabled or the recorded fingerprint is unusable.
func locationClause(data []byte, totalLines, lineStart, lineEnd int, contentHash string, options Options) string {
	if !options.Relocate {
		return ""
	}
	expected, err := normalizedExpectedHash(contentHash)
	if err != nil {
		return ""
	}
	return relocationClause(data, totalLines, lineStart, lineEnd, expected)
}

func safeEvidencePath(root, rawPath string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("evidence path is empty")
	}
	if filepath.IsAbs(rawPath) || filepath.VolumeName(rawPath) != "" {
		return "", fmt.Errorf("evidence path %q must be repository-relative", rawPath)
	}
	for _, component := range strings.Split(rawPath, string(filepath.Separator)) {
		if component == ".." {
			return "", fmt.Errorf("evidence path %q contains a parent component", rawPath)
		}
	}

	fullPath := filepath.Join(root, rawPath)
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve evidence path %q: %w", rawPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence path %q is outside the repository", rawPath)
	}

	// Lstat every component instead of relying on EvalSymlinks: even a link
	// whose target is inside the repository is not an acceptable evidence path.
	current := root
	for _, component := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("cannot access evidence path %q: %w", rawPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("evidence path %q contains a symlink", rawPath)
		}
	}
	return fullPath, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read evidence file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("evidence path %q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read evidence file %q: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat evidence file %q: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("evidence path %q changed to a non-regular or different file", path)
	}
	// Presize from Stat so large evidence files are read without the repeated
	// growth copies of a naive ReadAll.
	data := make([]byte, 0, readCapacity(openedInfo.Size()))
	buffer := make([]byte, 32*1024)
	for {
		n, err := file.Read(buffer)
		data = append(data, buffer[:n]...)
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read evidence file %q: %w", path, err)
		}
	}
}

func readCapacity(size int64) int {
	const min, max = 512, 8 << 20
	if size < min {
		return min
	}
	if size > max {
		return max
	}
	return int(size) + 1
}

// countNormalizedLines counts lines the way the historical
// splitNormalizedLines did: a "\r\n" pair is one terminator, a final line
// terminator does not introduce a trailing empty line, and empty input has no
// lines. It scans bytes in place and allocates nothing.
func countNormalizedLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := 0
	for offset := 0; offset < len(data); {
		index := bytes.IndexByte(data[offset:], '\n')
		if index < 0 {
			break
		}
		count++
		offset += index + 1
	}
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

// hashSelectedLines hashes the selected line range exactly as joining the
// normalized lines with "\n" and appending a final "\n" for non-empty content
// did historically. It scans in place and allocates only the digest.
func hashSelectedLines(data []byte, lineStart, lineEnd int) string {
	hasher := sha256.New()
	newline := []byte("\n")
	firstLineNonEmpty := false
	lineNo := 1
	for offset := 0; lineNo <= lineEnd && offset < len(data); {
		index := bytes.IndexByte(data[offset:], '\n')
		contentEnd := 0
		if index < 0 {
			// The final line has no terminator; a lone "\r" stays in content.
			contentEnd = len(data)
		} else {
			contentEnd = offset + index
			// A "\r" directly before the "\n" is part of the terminator.
			if contentEnd > offset && data[contentEnd-1] == '\r' {
				contentEnd--
			}
		}
		if lineNo >= lineStart {
			if lineNo > lineStart {
				hasher.Write(newline)
			}
			hasher.Write(data[offset:contentEnd])
			if lineNo == lineStart {
				firstLineNonEmpty = contentEnd > offset
			}
		}
		if index < 0 {
			break
		}
		offset += index + 1
		lineNo++
	}
	// Joining non-empty content appends the final terminator; a single empty
	// line joins to "" and stays un-terminated.
	if lineEnd > lineStart || firstLineNonEmpty {
		hasher.Write(newline)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func normalizedExpectedHash(value string) (string, error) {
	hexValue := value
	if strings.HasPrefix(hexValue, "sha256:") {
		hexValue = strings.TrimPrefix(hexValue, "sha256:")
	}
	if len(hexValue) != sha256.Size*2 {
		return "", fmt.Errorf("invalid SHA-256 content hash %q", value)
	}
	decoded, err := hex.DecodeString(hexValue)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("invalid SHA-256 content hash %q", value)
	}
	return strings.ToLower(hexValue), nil
}
