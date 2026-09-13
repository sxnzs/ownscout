package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
	"ownscout/internal/nodepacket"
)

// loadPacket decodes a packet at the single strict boundary shared by every
// command: the 1 MiB input cap plus nodepacket.DecodeValid. Contract
// violations are returned alongside a decodable packet; a hard decode failure
// surfaces as exactly one RuleDecode violation.
func loadPacket(path string) (contract.Packet, []contract.Violation, error) {
	data, err := readBounded(path, "packet", nodepacket.MaxInputBytes)
	if err != nil {
		return contract.Packet{}, nil, err
	}
	packet, violations := nodepacket.DecodeValid(data)
	return packet, violations, nil
}

// isDecodeFailure reports whether the violation list is the single strict
// decode rejection rather than contract findings on a decoded packet.
func isDecodeFailure(violations []contract.Violation) bool {
	return len(violations) == 1 && violations[0].Rule == nodepacket.RuleDecode
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
