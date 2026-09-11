// Package contract contains the versioned packet contract consumed by Astra.
//
// The package deliberately keeps the wire representation small and uses only
// standard-library types.  Validation is separate from JSON decoding so that
// callers which construct a Packet directly get the same checks as callers
// which decode one from disk.
package contract

import (
	"strconv"
	"strings"
)

const (
	// SchemaVersionV1 is the only packet schema understood by this package.
	SchemaVersionV1 = "v1"

	// Outcome values accepted by packet schema v1.
	OutcomeComplete           = "complete"
	OutcomePartial            = "partial"
	OutcomePartialDegraded    = "partial_degraded"
	OutcomeNoMatch            = "no_match"
	OutcomeStale              = "stale"
	OutcomeUnavailable        = "unavailable"
	OutcomeBlocked            = "blocked"
	OutcomeNeedsMoreEvidence  = "needs_more_evidence"
	OutcomeFailedVerification = "failed_verification"
	OutcomeBudgetExhausted    = "budget_exhausted"

	// Authorization levels used by the packet producers.  The contract does
	// not require a particular level for every outcome, but it does prohibit
	// autonomous authorization for terminally unsafe outcomes.
	AuthorizationAutonomous = "autonomous"

	// Violation rule identifiers.  They are stable identifiers intended for
	// deterministic CLI and machine-readable output.
	RuleRequiredField     = "required_field"
	RuleSchemaVersion     = "schema_version"
	RuleOutcome           = "outcome"
	RuleFreshness         = "freshness"
	RuleBudget            = "budget"
	RuleDegradations      = "degradations"
	RuleEvidence          = "evidence"
	RuleMalformedEvidence = "malformed_evidence"
	RuleAuthorization     = "authorization"
)

// Packet is an OwnScout packet schema v1 document.
type Packet struct {
	PacketID      string        `json:"packet_id"`
	SchemaVersion string        `json:"schema_version"`
	RepoRoot      string        `json:"repo_root"`
	HeadCommit    string        `json:"head_commit"`
	RequestID     string        `json:"request_id"`
	IssuedAt      string        `json:"issued_at"`
	Outcome       string        `json:"outcome"`
	Freshness     Freshness     `json:"freshness"`
	Authorization Authorization `json:"authorization"`
	Budget        Budget        `json:"budget"`
	Evidence      []Evidence    `json:"evidence"`
	Degradations  []string      `json:"degradations"`
	Provenance    Provenance    `json:"provenance"`
	PacketHash    string        `json:"packet_hash"`
}

// Freshness anchors the packet to the repository head against which it was
// collected.  HeadAnchor is accepted as a compatibility spelling for
// head_commit; producers should use HeadCommit on the wire.
type Freshness struct {
	HeadCommit string `json:"head_commit"`
	HeadAnchor string `json:"head_anchor,omitempty"`
	Status     string `json:"status,omitempty"`
	Current    bool   `json:"current,omitempty"`
	IsCurrent  bool   `json:"is_current,omitempty"`
	CheckedAt  string `json:"checked_at,omitempty"`
}

// Authorization describes the authority granted to the packet consumer.
type Authorization struct {
	Level  string `json:"level"`
	Reason string `json:"reason,omitempty"`
}

// Budget contains the evidence and byte counters used while collecting a
// packet.  A used counter must be non-negative and cannot exceed its maximum.
type Budget struct {
	MaxEvidence  int   `json:"max_evidence"`
	UsedEvidence int   `json:"used_evidence"`
	MaxBytes     int64 `json:"max_bytes"`
	UsedBytes    int64 `json:"used_bytes"`
}

// Evidence is one source span supplied to the packet consumer.
type Evidence struct {
	EvidenceID     string `json:"evidence_id"`
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	Commit         string `json:"commit"`
	LineStart      int    `json:"line_start"`
	LineEnd        int    `json:"line_end"`
	Source         string `json:"source"`
	ContentHash    string `json:"content_hash"`
	CollectedAt    string `json:"collected_at"`
	VerifierStatus string `json:"verifier_status"`
}

// Provenance identifies the collector and its version.  The packet contract
// requires the provenance object; its internal fields are intentionally
// permissive so a collector can add metadata without changing this package.
type Provenance struct {
	Collector   string `json:"collector,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Version     string `json:"version,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
}

// Violation describes one contract violation.
type Violation struct {
	Rule    string `json:"rule"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Evaluation is the result of validating a packet and resolving its default
// action.  An invalid packet is fail-closed: its action is blocked even when
// its outcome would otherwise map to autonomous_proceed.
type Evaluation struct {
	Valid              bool        `json:"valid"`
	Action             string      `json:"action"`
	AuthorizationLevel string      `json:"authorization_level"`
	Violations         []Violation `json:"violations"`
}

var outcomeActions = map[string]string{
	OutcomeComplete:           "autonomous_proceed",
	OutcomePartial:            "bounded_more_evidence",
	OutcomePartialDegraded:    "autonomous_proceed",
	OutcomeNoMatch:            "autonomous_proceed",
	OutcomeStale:              "bounded_refresh",
	OutcomeUnavailable:        "blocked",
	OutcomeBlocked:            "blocked",
	OutcomeNeedsMoreEvidence:  "bounded_more_evidence",
	OutcomeFailedVerification: "quarantine",
	OutcomeBudgetExhausted:    "human_approval",
}

var validOutcomes = map[string]struct{}{
	OutcomeComplete:           {},
	OutcomePartial:            {},
	OutcomePartialDegraded:    {},
	OutcomeNoMatch:            {},
	OutcomeStale:              {},
	OutcomeUnavailable:        {},
	OutcomeBlocked:            {},
	OutcomeNeedsMoreEvidence:  {},
	OutcomeFailedVerification: {},
	OutcomeBudgetExhausted:    {},
}

// DefaultAction returns the action assigned to an outcome by packet schema
// v1.  The bool is false when outcome is not part of the schema.
func DefaultAction(outcome string) (string, bool) {
	action, ok := outcomeActions[outcome]
	return action, ok
}

// EvaluatePacket validates p and resolves its action.  Invalid packets are
// never actionable, and therefore receive the fail-closed blocked action.
func EvaluatePacket(p Packet) Evaluation {
	violations := ValidatePacket(p)
	action, knownOutcome := DefaultAction(p.Outcome)
	if !knownOutcome {
		action = "blocked"
	}
	if len(violations) != 0 {
		action = "blocked"
	}

	authorizationLevel := strings.TrimSpace(p.Authorization.Level)
	if authorizationLevel == "" {
		authorizationLevel = "unknown"
	}

	return Evaluation{
		Valid:              len(violations) == 0,
		Action:             action,
		AuthorizationLevel: authorizationLevel,
		Violations:         violations,
	}
}

// ValidatePacket returns all deterministic validation violations for p.
func ValidatePacket(p Packet) []Violation {
	var violations []Violation
	add := func(rule, field, message string) {
		violations = append(violations, Violation{Rule: rule, Field: field, Message: message})
	}

	for _, required := range []struct {
		field string
		value string
	}{
		{"packet_id", p.PacketID},
		{"schema_version", p.SchemaVersion},
		{"repo_root", p.RepoRoot},
		{"head_commit", p.HeadCommit},
		{"request_id", p.RequestID},
		{"issued_at", p.IssuedAt},
		{"outcome", p.Outcome},
		{"packet_hash", p.PacketHash},
	} {
		if strings.TrimSpace(required.value) == "" {
			add(RuleRequiredField, required.field, "required field is missing")
		}
	}

	if p.Freshness.empty() {
		add(RuleRequiredField, "freshness", "required field is missing")
	}
	if strings.TrimSpace(p.Authorization.Level) == "" {
		add(RuleRequiredField, "authorization", "required field is missing")
	}
	if p.Budget.empty() {
		add(RuleRequiredField, "budget", "required field is missing")
	}
	if p.Evidence == nil {
		add(RuleRequiredField, "evidence", "required field is missing")
	}
	if p.Degradations == nil {
		add(RuleRequiredField, "degradations", "required field is missing")
	}
	if p.Provenance.empty() {
		add(RuleRequiredField, "provenance", "required field is missing")
	}

	if p.SchemaVersion != SchemaVersionV1 {
		add(RuleSchemaVersion, "schema_version", "must be v1")
	}
	if _, ok := validOutcomes[p.Outcome]; !ok {
		add(RuleOutcome, "outcome", "unknown outcome")
	}

	validateFreshness(p, add)
	validateBudget(p, add)
	validateDegradations(p, add)
	validateEvidence(p, add)
	validateAuthorization(p, add)

	return violations
}

func validateFreshness(p Packet, add func(string, string, string)) {
	anchor := strings.TrimSpace(p.Freshness.HeadCommit)
	if anchor == "" {
		anchor = strings.TrimSpace(p.Freshness.HeadAnchor)
	}
	if p.HeadCommit != "" && anchor != "" && anchor != p.HeadCommit {
		add(RuleFreshness, "freshness.head_commit", "freshness head anchor does not match packet head_commit")
	}
	if p.Freshness.HeadCommit != "" && p.Freshness.HeadAnchor != "" &&
		p.Freshness.HeadCommit != p.Freshness.HeadAnchor {
		add(RuleFreshness, "freshness.head_anchor", "freshness head anchors disagree")
	}

	if p.Outcome != OutcomeComplete {
		return
	}
	if anchor == "" {
		add(RuleFreshness, "freshness.head_commit", "complete packet requires a freshness head anchor")
	}
	if !p.Freshness.current() {
		add(RuleFreshness, "freshness.status", "complete packet requires current freshness")
	}
}

func validateBudget(p Packet, add func(string, string, string)) {
	if p.Budget.empty() {
		add(RuleBudget, "budget", "budget must contain counters")
		return
	}

	if p.Budget.MaxEvidence < 0 {
		add(RuleBudget, "budget.max_evidence", "budget counter cannot be negative")
	}
	if p.Budget.UsedEvidence < 0 {
		add(RuleBudget, "budget.used_evidence", "budget counter cannot be negative")
	}
	if p.Budget.MaxBytes < 0 {
		add(RuleBudget, "budget.max_bytes", "budget counter cannot be negative")
	}
	if p.Budget.UsedBytes < 0 {
		add(RuleBudget, "budget.used_bytes", "budget counter cannot be negative")
	}
	if p.Budget.UsedEvidence > p.Budget.MaxEvidence {
		add(RuleBudget, "budget.used_evidence", "used evidence exceeds max_evidence")
	}
	if p.Budget.UsedBytes > p.Budget.MaxBytes {
		add(RuleBudget, "budget.used_bytes", "used bytes exceeds max_bytes")
	}
}

func validateDegradations(p Packet, add func(string, string, string)) {
	for i, degradation := range p.Degradations {
		if strings.TrimSpace(degradation) == "" {
			add(RuleDegradations, "degradations["+strconv.Itoa(i)+"]", "degradation must not be empty")
		}
	}
	if p.Outcome == OutcomeComplete && len(p.Degradations) != 0 {
		add(RuleDegradations, "degradations", "complete packet cannot contain degradations")
	}
}

func validateEvidence(p Packet, add func(string, string, string)) {
	for i, evidence := range p.Evidence {
		field := "evidence[" + strconv.Itoa(i) + "]"
		for _, required := range []struct {
			name  string
			value string
		}{
			{"evidence_id", evidence.EvidenceID},
			{"kind", evidence.Kind},
			{"path", evidence.Path},
			{"commit", evidence.Commit},
			{"source", evidence.Source},
			{"content_hash", evidence.ContentHash},
			{"collected_at", evidence.CollectedAt},
			{"verifier_status", evidence.VerifierStatus},
		} {
			if strings.TrimSpace(required.value) == "" {
				add(RuleMalformedEvidence, field+"."+required.name, "required evidence field is missing")
			}
		}
		if evidence.LineStart < 1 {
			add(RuleMalformedEvidence, field+".line_start", "line_start must be at least 1")
		}
		if evidence.LineEnd < 1 {
			add(RuleMalformedEvidence, field+".line_end", "line_end must be at least 1")
		}
		if evidence.LineStart >= 1 && evidence.LineEnd >= 1 && evidence.LineEnd < evidence.LineStart {
			add(RuleMalformedEvidence, field, "line_end must be greater than or equal to line_start")
		}
		if !validVerifierStatus(evidence.VerifierStatus) {
			add(RuleMalformedEvidence, field+".verifier_status", "unknown verifier status")
		}

		if p.Outcome == OutcomeComplete && evidence.VerifierStatus != "verified" {
			add(RuleEvidence, field+".verifier_status", "complete packet requires verified evidence")
		}
	}

	if p.Outcome == OutcomeComplete && len(p.Evidence) == 0 {
		add(RuleEvidence, "evidence", "complete packet requires at least one evidence entry")
	}
}

func validateAuthorization(p Packet, add func(string, string, string)) {
	if !isAutonomous(p.Authorization.Level) {
		return
	}
	switch p.Outcome {
	case OutcomeFailedVerification, OutcomeBlocked, OutcomeUnavailable, OutcomeBudgetExhausted:
		add(RuleAuthorization, "authorization.level", "autonomous authorization is forbidden for this outcome")
	}
}

func validVerifierStatus(status string) bool {
	switch status {
	case "verified", "unverified", "failed", "unavailable", "pending":
		return true
	default:
		return false
	}
}

func isAutonomous(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "autonomous", "autonomous_proceed", "autonomous-proceed":
		return true
	default:
		return false
	}
}

func (f Freshness) empty() bool {
	return strings.TrimSpace(f.HeadCommit) == "" &&
		strings.TrimSpace(f.HeadAnchor) == "" &&
		strings.TrimSpace(f.Status) == "" &&
		!f.Current && !f.IsCurrent && strings.TrimSpace(f.CheckedAt) == ""
}

func (f Freshness) current() bool {
	switch strings.ToLower(strings.TrimSpace(f.Status)) {
	case "current", "fresh":
		return true
	}
	return f.Current || f.IsCurrent
}

func (b Budget) empty() bool {
	return b.MaxEvidence == 0 && b.UsedEvidence == 0 && b.MaxBytes == 0 && b.UsedBytes == 0
}

func (p Provenance) empty() bool {
	return strings.TrimSpace(p.Collector) == "" && strings.TrimSpace(p.Tool) == "" &&
		strings.TrimSpace(p.Version) == "" && strings.TrimSpace(p.ToolVersion) == ""
}
