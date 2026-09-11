package node

import (
	"fmt"
	"sort"

	"ownscout/internal/contract"
)

const (
	maxInputBytes        = 1 << 20
	maxNodes             = 256
	maxReferencesPerNode = 128
	maxTotalReferences   = 4096
)

// ValidateEnvelope checks both directly constructed and parsed envelopes.
// Both supplied bindings must exactly match the recomputed lowercase digest.
// An envelope must contain at least one node so that an empty graph cannot be
// represented as a vacuous success.
func ValidateEnvelope(env Envelope, packet contract.Packet, binding string) error {
	_, err := validateEnvelope(env, packet, binding)
	return err
}

// Share validation and its proven graph order with evaluation, without a second
// graph traversal or a path that evaluates partially validated input.
func validateEnvelope(env Envelope, packet contract.Packet, binding string) ([]int, error) {
	if err := validateLimits(env); err != nil {
		return nil, err
	}
	if env.SchemaVersion != SchemaVersionV1 {
		return nil, fmt.Errorf("schema_version must equal %q", SchemaVersionV1)
	}
	if len(env.Nodes) == 0 {
		return nil, fmt.Errorf("nodes must contain at least one node")
	}
	if err := validateIdentifier("envelope_id", env.EnvelopeID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("packet_id", env.PacketID); err != nil {
		return nil, err
	}
	if env.PacketID != packet.PacketID {
		return nil, fmt.Errorf("packet_id does not match packet")
	}
	computed, err := CanonicalPacketBinding(packet)
	if err != nil {
		return nil, err
	}
	if binding != computed {
		return nil, fmt.Errorf("binding does not match computed packet binding")
	}
	if env.PacketBindingSHA256 != computed {
		return nil, fmt.Errorf("packet_binding_sha256 does not match computed packet binding")
	}

	evidenceIDs := make(map[string]bool, len(packet.Evidence))
	for _, item := range packet.Evidence {
		if err := validateIdentifier("packet evidence_id", item.EvidenceID); err != nil {
			return nil, err
		}
		if evidenceIDs[item.EvidenceID] {
			return nil, fmt.Errorf("duplicate packet evidence_id %q", item.EvidenceID)
		}
		evidenceIDs[item.EvidenceID] = true
	}

	indexes := make(map[string]int, len(env.Nodes))
	for i, n := range env.Nodes {
		if err := validateIdentifier("node_id", n.NodeID); err != nil {
			return nil, err
		}
		if _, exists := indexes[n.NodeID]; exists {
			return nil, fmt.Errorf("duplicate node_id %q", n.NodeID)
		}
		indexes[n.NodeID] = i
		if n.Verifier != VerifierCurrent {
			return nil, fmt.Errorf("node %q: verifier must equal %q", n.NodeID, VerifierCurrent)
		}
		if err := validateReferences("depends_on", n.DependsOn); err != nil {
			return nil, fmt.Errorf("node %q: %w", n.NodeID, err)
		}
		if err := validateReferences("evidence_ids", n.EvidenceIDs); err != nil {
			return nil, fmt.Errorf("node %q: %w", n.NodeID, err)
		}
		for _, id := range n.EvidenceIDs {
			if !evidenceIDs[id] {
				return nil, fmt.Errorf("node %q: evidence %q is not present in packet", n.NodeID, id)
			}
		}
	}
	for _, n := range env.Nodes {
		for _, id := range n.DependsOn {
			if id == n.NodeID {
				return nil, fmt.Errorf("node %q: self-dependency", n.NodeID)
			}
			if _, exists := indexes[id]; !exists {
				return nil, fmt.Errorf("node %q: missing dependency %q", n.NodeID, id)
			}
		}
	}
	return topologicalOrder(env.Nodes, indexes)
}

func validateLimits(env Envelope) error {
	if env.Nodes == nil {
		return fmt.Errorf("nodes must be a non-null array")
	}
	if len(env.Nodes) > maxNodes {
		return fmt.Errorf("nodes exceeds %d node limit", maxNodes)
	}
	total := 0
	for i, n := range env.Nodes {
		if n.DependsOn == nil {
			return fmt.Errorf("nodes[%d].depends_on must be a non-null array", i)
		}
		if len(n.EvidenceIDs) == 0 {
			return fmt.Errorf("nodes[%d].evidence_ids must be a non-empty, non-null array", i)
		}
		if len(n.DependsOn) > maxReferencesPerNode {
			return fmt.Errorf("nodes[%d].depends_on exceeds %d reference limit", i, maxReferencesPerNode)
		}
		if len(n.EvidenceIDs) > maxReferencesPerNode {
			return fmt.Errorf("nodes[%d].evidence_ids exceeds %d reference limit", i, maxReferencesPerNode)
		}
		total += len(n.DependsOn) + len(n.EvidenceIDs)
		if total > maxTotalReferences {
			return fmt.Errorf("envelope exceeds %d total reference limit", maxTotalReferences)
		}
	}
	return nil
}

func validateIdentifier(field, value string) error {
	if len(value) < 1 || len(value) > 128 {
		return fmt.Errorf("%s must contain 1-128 ASCII identifier characters", field)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alphanumeric && (i == 0 || c != '.' && c != '_' && c != '-' && c != ':') {
			return fmt.Errorf("%s %q is not a valid identifier", field, value)
		}
	}
	return nil
}

func validateReferences(field string, refs []string) error {
	seen := make(map[string]bool, len(refs))
	for _, id := range refs {
		if err := validateIdentifier(field, id); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("duplicate %s reference %q", field, id)
		}
		seen[id] = true
	}
	return nil
}

// Kahn's algorithm picks the lexicographically smallest currently ready node,
// including newly ready nodes, rather than sorting only the initial frontier.
func topologicalOrder(nodes []Node, indexes map[string]int) ([]int, error) {
	degrees := make([]int, len(nodes))
	dependents := make([][]int, len(nodes))
	ready := make([]int, 0, len(nodes))
	for i, n := range nodes {
		degrees[i] = len(n.DependsOn)
		if degrees[i] == 0 {
			ready = append(ready, i)
		}
		for _, id := range n.DependsOn {
			parent := indexes[id]
			dependents[parent] = append(dependents[parent], i)
		}
	}
	order := make([]int, 0, len(nodes))
	for len(ready) != 0 {
		// With at most 256 nodes, sorting this bounded queue is sufficient.
		sort.Slice(ready, func(i, j int) bool { return nodes[ready[i]].NodeID < nodes[ready[j]].NodeID })
		index := ready[0]
		ready = ready[1:]
		order = append(order, index)
		for _, child := range dependents[index] {
			degrees[child]--
			if degrees[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("node dependency graph contains a cycle")
	}
	return order, nil
}
