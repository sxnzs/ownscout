package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAction(t *testing.T) {
	tests := []struct {
		outcome string
		action  string
	}{
		{OutcomeComplete, "autonomous_proceed"},
		{OutcomePartial, "bounded_more_evidence"},
		{OutcomePartialDegraded, "autonomous_proceed"},
		{OutcomeNoMatch, "autonomous_proceed"},
		{OutcomeStale, "bounded_refresh"},
		{OutcomeUnavailable, "blocked"},
		{OutcomeBlocked, "blocked"},
		{OutcomeNeedsMoreEvidence, "bounded_more_evidence"},
		{OutcomeFailedVerification, "quarantine"},
		{OutcomeBudgetExhausted, "human_approval"},
	}

	for _, test := range tests {
		t.Run(test.outcome, func(t *testing.T) {
			action, ok := DefaultAction(test.outcome)
			if !ok || action != test.action {
				t.Fatalf("DefaultAction(%q) = %q, %v; want %q, true", test.outcome, action, ok, test.action)
			}
		})
	}
	if action, ok := DefaultAction("unknown"); ok || action != "" {
		t.Fatalf("DefaultAction(unknown) = %q, %v; want empty action, false", action, ok)
	}
}

func TestValidPacket(t *testing.T) {
	packet := loadPacket(t, "valid.json")
	evaluation := EvaluatePacket(packet)
	if !evaluation.Valid {
		t.Fatalf("valid packet rejected: %+v", evaluation.Violations)
	}
	if evaluation.Action != "autonomous_proceed" {
		t.Fatalf("action = %q, want autonomous_proceed", evaluation.Action)
	}
	if evaluation.AuthorizationLevel != "autonomous" {
		t.Fatalf("authorization level = %q, want autonomous", evaluation.AuthorizationLevel)
	}
	if got := ValidatePacket(packet); len(got) != 0 {
		t.Fatalf("ValidatePacket(valid) = %+v", got)
	}
}

func TestInvalidPacketVariants(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantRule  string
		wantField string
	}{
		{"required field", "invalid-missing-required.json", RuleRequiredField, "packet_hash"},
		{"schema version", "invalid-schema.json", RuleSchemaVersion, "schema_version"},
		{"outcome enum", "invalid-outcome.json", RuleOutcome, "outcome"},
		{"freshness anchor", "invalid-freshness.json", RuleFreshness, "freshness.head_commit"},
		{"budget counters", "invalid-budget.json", RuleBudget, "budget.used_evidence"},
		{"degradations", "invalid-degradations.json", RuleDegradations, "degradations"},
		{"verified evidence", "invalid-unverified-evidence.json", RuleEvidence, "evidence[0].verifier_status"},
		{"forbidden autonomy", "invalid-forbidden-autonomy.json", RuleAuthorization, "authorization.level"},
		{"malformed evidence", "invalid-evidence.json", RuleMalformedEvidence, "evidence[0].evidence_id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := loadPacket(t, test.file)
			violations := ValidatePacket(packet)
			if len(violations) == 0 {
				t.Fatal("expected validation violations")
			}
			if !hasViolation(violations, test.wantRule, test.wantField) {
				t.Fatalf("violations = %+v; want rule=%q field=%q", violations, test.wantRule, test.wantField)
			}
			if EvaluatePacket(packet).Valid {
				t.Fatal("invalid packet evaluated as valid")
			}
		})
	}
}

func TestCompleteRequirements(t *testing.T) {
	packet := loadPacket(t, "valid.json")

	tests := []struct {
		name      string
		mutate    func(*Packet)
		wantRule  string
		wantField string
	}{
		{
			name: "not current",
			mutate: func(p *Packet) {
				p.Freshness.Status = "stale"
			},
			wantRule: RuleFreshness, wantField: "freshness.status",
		},
		{
			name: "missing evidence",
			mutate: func(p *Packet) {
				p.Evidence = []Evidence{}
			},
			wantRule: RuleEvidence, wantField: "evidence",
		},
		{
			name: "negative budget",
			mutate: func(p *Packet) {
				p.Budget.UsedEvidence = -1
			},
			wantRule: RuleBudget, wantField: "budget.used_evidence",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := packet
			test.mutate(&mutated)
			if !hasViolation(ValidatePacket(mutated), test.wantRule, test.wantField) {
				t.Fatalf("violations = %+v; want rule=%q field=%q", ValidatePacket(mutated), test.wantRule, test.wantField)
			}
		})
	}
}

func TestAutonomousAuthorizationForbiddenOutcomes(t *testing.T) {
	for _, outcome := range []string{
		OutcomeFailedVerification,
		OutcomeBlocked,
		OutcomeUnavailable,
		OutcomeBudgetExhausted,
	} {
		t.Run(outcome, func(t *testing.T) {
			packet := loadPacket(t, "valid.json")
			packet.Outcome = outcome
			packet.Authorization.Level = AuthorizationAutonomous
			violations := ValidatePacket(packet)
			if !hasViolation(violations, RuleAuthorization, "authorization.level") {
				t.Fatalf("violations = %+v; want forbidden autonomy violation", violations)
			}
		})
	}
}

func loadPacket(t *testing.T, name string) Packet {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var packet Packet
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return packet
}

func hasViolation(violations []Violation, rule, field string) bool {
	for _, violation := range violations {
		if violation.Rule == rule && violation.Field == field {
			return true
		}
	}
	return false
}
