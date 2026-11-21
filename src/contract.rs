use crate::packet::{Evidence, Packet};

#[derive(Clone)]
pub struct Violation {
    pub rule: &'static str,
    pub field: String,
    pub message: &'static str,
}

pub fn validate(p: &Packet) -> Vec<Violation> {
    let mut v = Vec::new();
    for (field, value) in [
        ("packet_id", &p.packet_id),
        ("schema_version", &p.schema_version),
        ("repo_root", &p.repo_root),
        ("head_commit", &p.head_commit),
        ("request_id", &p.request_id),
        ("issued_at", &p.issued_at),
        ("outcome", &p.outcome),
        ("packet_hash", &p.packet_hash),
    ] {
        if value.trim().is_empty() {
            v.push(req(field));
        }
    }
    if fresh_empty(&p.freshness) {
        v.push(req("freshness"));
    }
    if p.authorization.level.trim().is_empty() {
        v.push(req("authorization"));
    }
    if budget_empty(&p.budget) {
        v.push(req("budget"));
    }
    if p.evidence.is_none() {
        v.push(req("evidence"));
    }
    if p.degradations.is_none() {
        v.push(req("degradations"));
    }
    if provenance_empty(&p.provenance) {
        v.push(req("provenance"));
    }
    if p.schema_version != "v1" {
        v.push(Violation {
            rule: "schema_version",
            field: "schema_version".into(),
            message: "must be v1",
        });
    }
    if ![
        "complete",
        "partial",
        "partial_degraded",
        "no_match",
        "stale",
        "unavailable",
        "blocked",
        "needs_more_evidence",
        "failed_verification",
        "budget_exhausted",
    ]
    .contains(&p.outcome.as_str())
    {
        v.push(Violation {
            rule: "outcome",
            field: "outcome".into(),
            message: "unknown outcome",
        });
    }
    let anchor = if !p.freshness.head_commit.trim().is_empty() {
        p.freshness.head_commit.trim()
    } else {
        p.freshness.head_anchor.trim()
    };
    if !p.head_commit.is_empty() && !anchor.is_empty() && anchor != p.head_commit {
        v.push(Violation {
            rule: "freshness",
            field: "freshness.head_commit".into(),
            message: "freshness head anchor does not match packet head_commit",
        });
    }
    if !p.freshness.head_commit.is_empty()
        && !p.freshness.head_anchor.is_empty()
        && p.freshness.head_commit != p.freshness.head_anchor
    {
        v.push(Violation {
            rule: "freshness",
            field: "freshness.head_anchor".into(),
            message: "freshness head anchors disagree",
        });
    }
    if p.outcome == "complete" {
        if anchor.is_empty() {
            v.push(Violation {
                rule: "freshness",
                field: "freshness.head_commit".into(),
                message: "complete packet requires a freshness head anchor",
            });
        }
        if !fresh_current(&p.freshness) {
            v.push(Violation {
                rule: "freshness",
                field: "freshness.status".into(),
                message: "complete packet requires current freshness",
            });
        }
    }
    if budget_empty(&p.budget) {
        v.push(Violation {
            rule: "budget",
            field: "budget".into(),
            message: "budget must contain counters",
        });
    } else {
        let b = &p.budget;
        for (field, n) in [
            ("budget.max_evidence", b.max_evidence),
            ("budget.used_evidence", b.used_evidence),
            ("budget.max_bytes", b.max_bytes),
            ("budget.used_bytes", b.used_bytes),
        ] {
            if n < 0 {
                v.push(Violation {
                    rule: "budget",
                    field: field.into(),
                    message: "budget counter cannot be negative",
                });
            }
        }
        if b.used_evidence > b.max_evidence {
            v.push(Violation {
                rule: "budget",
                field: "budget.used_evidence".into(),
                message: "used evidence exceeds max_evidence",
            });
        }
        if b.used_bytes > b.max_bytes {
            v.push(Violation {
                rule: "budget",
                field: "budget.used_bytes".into(),
                message: "used bytes exceeds max_bytes",
            });
        }
    }
    if let Some(ds) = &p.degradations {
        for (i, d) in ds.iter().enumerate() {
            if d.trim().is_empty() {
                v.push(Violation {
                    rule: "degradations",
                    field: format!("degradations[{}]", i),
                    message: "degradation must not be empty",
                });
            }
        }
        if p.outcome == "complete" && !ds.is_empty() {
            v.push(Violation {
                rule: "degradations",
                field: "degradations".into(),
                message: "complete packet cannot contain degradations",
            });
        }
    }
    if let Some(es) = &p.evidence {
        for (i, e) in es.iter().enumerate() {
            validate_evidence(&mut v, p.outcome.as_str(), i, e);
        }
    }
    // The reference runs this even when the field is absent or null
    // (internal/contract/contract.go:351-353).
    if p.outcome == "complete" && p.evidence.as_ref().map_or(true, |es| es.is_empty()) {
        v.push(Violation {
            rule: "evidence",
            field: "evidence".into(),
            message: "complete packet requires at least one evidence entry",
        });
    }
    if [
        "failed_verification",
        "blocked",
        "unavailable",
        "budget_exhausted",
    ]
    .contains(&p.outcome.as_str())
        && ["autonomous", "autonomous_proceed", "autonomous-proceed"]
            .contains(&p.authorization.level.trim().to_ascii_lowercase().as_str())
    {
        v.push(Violation {
            rule: "authorization",
            field: "authorization.level".into(),
            message: "autonomous authorization is forbidden for this outcome",
        });
    }
    v
}
fn req(field: &str) -> Violation {
    Violation {
        rule: "required_field",
        field: field.into(),
        message: "required field is missing",
    }
}
fn fresh_empty(f: &crate::packet::Freshness) -> bool {
    f.head_commit.trim().is_empty()
        && f.head_anchor.trim().is_empty()
        && f.status.trim().is_empty()
        && !f.current
        && !f.is_current
        && f.checked_at.trim().is_empty()
}
fn fresh_current(f: &crate::packet::Freshness) -> bool {
    matches!(
        f.status.trim().to_ascii_lowercase().as_str(),
        "current" | "fresh"
    ) || f.current
        || f.is_current
}
fn budget_empty(b: &crate::packet::Budget) -> bool {
    b.max_evidence == 0 && b.used_evidence == 0 && b.max_bytes == 0 && b.used_bytes == 0
}
fn provenance_empty(p: &crate::packet::Provenance) -> bool {
    p.collector.trim().is_empty()
        && p.tool.trim().is_empty()
        && p.version.trim().is_empty()
        && p.tool_version.trim().is_empty()
}
fn validate_evidence(v: &mut Vec<Violation>, outcome: &str, i: usize, e: &Evidence) {
    let base = format!("evidence[{}]", i);
    for (name, value) in [
        ("evidence_id", &e.evidence_id),
        ("kind", &e.kind),
        ("path", &e.path),
        ("commit", &e.commit),
        ("source", &e.source),
        ("content_hash", &e.content_hash),
        ("collected_at", &e.collected_at),
        ("verifier_status", &e.verifier_status),
    ] {
        if value.trim().is_empty() {
            v.push(Violation {
                rule: "malformed_evidence",
                field: format!("{}.{}", base, name),
                message: "required evidence field is missing",
            });
        }
    }
    if e.line_start < 1 {
        v.push(Violation {
            rule: "malformed_evidence",
            field: format!("{}.line_start", base),
            message: "line_start must be at least 1",
        });
    }
    if e.line_end < 1 {
        v.push(Violation {
            rule: "malformed_evidence",
            field: format!("{}.line_end", base),
            message: "line_end must be at least 1",
        });
    }
    if e.line_start >= 1 && e.line_end >= 1 && e.line_end < e.line_start {
        v.push(Violation {
            rule: "malformed_evidence",
            field: base.clone(),
            message: "line_end must be greater than or equal to line_start",
        });
    }
    if !["verified", "unverified", "failed", "unavailable", "pending"]
        .contains(&e.verifier_status.as_str())
    {
        v.push(Violation {
            rule: "malformed_evidence",
            field: format!("{}.verifier_status", base),
            message: "unknown verifier status",
        });
    }
    if outcome == "complete" && e.verifier_status != "verified" {
        v.push(Violation {
            rule: "evidence",
            field: format!("{}.verifier_status", base),
            message: "complete packet requires verified evidence",
        });
    }
}
pub fn action(outcome: &str) -> Option<&'static str> {
    match outcome {
        "complete" => Some("autonomous_proceed"),
        "partial" => Some("bounded_more_evidence"),
        "partial_degraded" => Some("autonomous_proceed"),
        "no_match" => Some("autonomous_proceed"),
        "stale" => Some("bounded_refresh"),
        "unavailable" | "blocked" => Some("blocked"),
        "needs_more_evidence" => Some("bounded_more_evidence"),
        "failed_verification" => Some("quarantine"),
        "budget_exhausted" => Some("human_approval"),
        _ => None,
    }
}
