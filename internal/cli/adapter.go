package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
)

func loadPacket(path string) (contract.Packet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return contract.Packet{}, fmt.Errorf("packet file %q does not exist", path)
		}
		return contract.Packet{}, fmt.Errorf("read packet %q: %w", path, err)
	}

	trimmed := bytes.TrimSpace(data)
	if !json.Valid(trimmed) {
		return contract.Packet{}, fmt.Errorf("packet %q is not valid JSON", path)
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return contract.Packet{}, fmt.Errorf("packet %q must contain a JSON object", path)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var packet contract.Packet
	if err := decoder.Decode(&packet); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return contract.Packet{}, fmt.Errorf("packet %q contains an unknown JSON field: %v", path, err)
		}
		return contract.Packet{}, fmt.Errorf("packet %q is not valid JSON: %v", path, err)
	}
	if err := ensureEOF(decoder); err != nil {
		return contract.Packet{}, fmt.Errorf("packet %q is not valid JSON: %v", path, err)
	}
	return packet, nil
}

func readBounded(path, kind string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s file %q does not exist", kind, path)
		}
		return nil, fmt.Errorf("open %s %q: %w", kind, path, err)
	}

	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read %s %q: %w", kind, path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s %q: %w", kind, path, closeErr)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s exceeds %d byte input limit", kind, limit)
	}
	return data, nil
}

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

func validatePacket(packet contract.Packet) []string {
	return packetViolationDetails(contract.ValidatePacket(packet))
}

func packetViolationDetails(violations []contract.Violation) []string {
	if len(violations) == 0 {
		return nil
	}

	details := make([]string, 0, len(violations))
	for _, violation := range violations {
		details = append(details, fmt.Sprintf("%s: %s: %s", violation.Rule, violation.Field, violation.Message))
	}
	return details
}

func evidenceIssues(report evidence.Report) []string {
	issues := make([]string, 0, report.FailedCount+report.SkippedCount)
	for _, result := range report.Results {
		if result.Status == "verified" {
			continue
		}
		issue := result.Message
		if issue == "" {
			issue = fmt.Sprintf("status: %s", result.Status)
		}
		issues = append(issues, fmt.Sprintf("evidence %q (%q): %s", result.EvidenceID, result.Path, issue))
	}
	return issues
}
