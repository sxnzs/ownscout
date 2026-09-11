// Package node parses, validates, and evaluates node-envelope-v1 evidence graphs.
package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
)

const (
	SchemaVersionV1 = "node-envelope-v1"
	VerifierCurrent = "evidence.current"
)

type Envelope struct {
	SchemaVersion       string `json:"schema_version"`
	EnvelopeID          string `json:"envelope_id"`
	PacketID            string `json:"packet_id"`
	PacketBindingSHA256 string `json:"packet_binding_sha256"`
	Nodes               []Node `json:"nodes"`
}

type Node struct {
	NodeID      string   `json:"node_id"`
	DependsOn   []string `json:"depends_on"`
	Verifier    string   `json:"verifier"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type Status string

const (
	StatusEvidenceCurrent Status = "evidence_current"
	StatusFailed          Status = "failed"
	StatusBlocked         Status = "blocked"
)

type Result struct {
	NodeID string `json:"node_id"`
	Status Status `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Evaluation struct {
	OK      bool     `json:"ok"`
	Results []Result `json:"results"`
}

// CanonicalPacketBinding hashes the JSON encoding of a strictly decoded packet,
// with its producer-supplied PacketHash cleared. Callers own strict packet
// decoding; the typed packet cannot retain its original JSON spelling.
// The packet is not mutated, and array order remains significant.
func CanonicalPacketBinding(packet contract.Packet) (string, error) {
	packet.PacketHash = ""
	data, err := json.Marshal(packet)
	if err != nil {
		return "", fmt.Errorf("marshal packet binding: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// EvaluateEnvelope validates before evaluating any node. Invalid envelopes
// return OK=false and an empty results array; use ValidateEnvelope for details.
// Report summaries and producer verifier statuses are not evidence of success.
func EvaluateEnvelope(env Envelope, packet contract.Packet, binding string, report evidence.Report) Evaluation {
	order, err := validateEnvelope(env, packet, binding)
	if err != nil {
		return Evaluation{Results: []Result{}}
	}

	current := make(map[string]bool, len(report.Results))
	for _, result := range report.Results {
		previous, exists := current[result.EvidenceID]
		verified := result.Status == "verified"
		// A later verified entry must not mask an earlier nonverified entry.
		current[result.EvidenceID] = verified && (!exists || previous)
	}

	evaluation := Evaluation{OK: true, Results: make([]Result, 0, len(order))}
	statuses := make(map[string]Status, len(order))
	for _, index := range order {
		n := env.Nodes[index]
		result := Result{NodeID: n.NodeID, Status: StatusEvidenceCurrent}
		for _, dependency := range n.DependsOn {
			if statuses[dependency] != StatusEvidenceCurrent {
				result.Status = StatusBlocked
				result.Reason = fmt.Sprintf("dependency %q is not evidence_current", dependency)
				break
			}
		}
		// Dependency blocking takes precedence over this node's own evidence.
		if result.Status == StatusEvidenceCurrent {
			for _, id := range n.EvidenceIDs {
				verified, exists := current[id]
				if !exists || !verified {
					result.Status = StatusFailed
					result.Reason = fmt.Sprintf("evidence %q is missing or not verified", id)
					break
				}
			}
		}
		statuses[n.NodeID] = result.Status
		evaluation.Results = append(evaluation.Results, result)
		if result.Status != StatusEvidenceCurrent {
			evaluation.OK = false
		}
	}
	return evaluation
}
