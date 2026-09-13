use crate::result::{render, usage, ResultData};
use crate::{contract, evidence, ledger, node, packet, sha256};
use std::fs;

const ROOT_USAGE: &str = "OwnScout — local repository evidence checks\n\nUsage:\n  ownscout doctor\n  ownscout version\n  ownscout contract validate --packet <file> [--json]\n  ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nUse \"ownscout <command> --help\" for command details.";

pub fn run(args: &[String]) -> (String, i32) {
    if args.is_empty() {
        return (
            format!(
                "error: a command is required\n\n{}\n\nNext action: run 'ownscout --help'.\n",
                ROOT_USAGE
            ),
            2,
        );
    }
    if args.iter().any(|a| a == "--help" || a == "-h") {
        return help(args);
    }
    match args[0].as_str() {
        "doctor" if args.len() == 1 => ("OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.\n".into(), 0),
        "doctor" => (usage("doctor does not accept arguments", "ownscout doctor --help"), 2),
        "version" if args.len() == 1 => ("ownscout 0.1.0\n".into(), 0),
        "version" => (usage("version does not accept arguments", "ownscout version --help"), 2),
        "contract" => contract_command(args),
        "evidence" => evidence_command(args),
        "node" => node_command(args),
        other => (usage(&format!("unknown command '{}'", other), "ownscout --help"), 2),
    }
}

fn contract_command(args: &[String]) -> (String, i32) {
    // The reference scans the whole argument list for --json even when flag
    // parsing fails, so a usage failure is rendered in the requested mode.
    let json = args.iter().any(|a| a == "--json");
    if args.len() < 2 || args[1] != "validate" {
        return if args.len() > 1 {
            usage_result(
                json,
                &format!("unknown contract subcommand '{}'", args[1]),
                "ownscout contract --help",
            )
        } else {
            usage_result(
                false,
                "a contract subcommand is required",
                "ownscout contract --help",
            )
        };
    }
    let mut path = None;
    let mut i = 2;
    while i < args.len() {
        if args[i] == "--json" {
            i += 1;
            continue;
        }
        if args[i] == "--packet" {
            if i + 1 >= args.len() || args[i + 1].starts_with('-') {
                return usage_result(
                    json,
                    "--packet requires a value",
                    "ownscout contract validate --help",
                );
            }
            path = Some(args[i + 1].clone());
            i += 2
        } else {
            return usage_result(
                json,
                &format!("unknown flag or argument '{}'", args[i]),
                "ownscout contract validate --help",
            );
        }
    }
    let Some(path) = path else {
        return usage_result(
            json,
            "missing required --packet <file>",
            "ownscout contract validate --help",
        );
    };
    let p = match load_packet(&path) {
        Ok(v) => v,
        Err(e) => return load_error(json, &e),
    };
    let violations = contract::validate(&p);
    if !violations.is_empty() {
        let details = violations
            .iter()
            .map(|v| format!("{}: {}: {}", v.rule, v.field, v.message))
            .collect();
        return result(render(
            &ResultData {
                command: "contract validate".into(),
                ok: false,
                summary: format!("packet is invalid ({} violation(s))", violations.len()),
                details,
                next_action: "Fix the listed packet fields, then run contract validation again."
                    .into(),
            },
            json,
            1,
        ));
    }
    result(render(
        &ResultData {
            command: "contract validate".into(),
            ok: true,
            summary: "packet is valid".into(),
            details: vec![
                format!("outcome: {}", p.outcome),
                format!(
                    "default action: {}",
                    contract::action(&p.outcome).unwrap_or("blocked")
                ),
            ],
            next_action: "Run evidence verification before consuming this packet.".into(),
        },
        json,
        0,
    ))
}
fn load_error(json: bool, detail: &str) -> (String, i32) {
    result(render(
        &ResultData {
            command: "contract validate".into(),
            ok: false,
            summary: "packet could not be loaded".into(),
            details: vec![detail.into()],
            next_action: "Provide a readable JSON packet with --packet <file>.".into(),
        },
        json,
        2,
    ))
}

fn load_packet(path: &str) -> Result<packet::Packet, String> {
    let data = fs::read(path).map_err(|e| {
        if e.kind() == std::io::ErrorKind::NotFound {
            format!("packet file {:?} does not exist", path)
        } else {
            format!("read packet {:?}: {}", path, e)
        }
    })?;
    let trimmed = trim_json_space(&data);
    let value =
        crate::json::parse(trimmed).map_err(|_| format!("packet {:?} is not valid JSON", path))?;
    if !matches!(value, crate::json::Value::Object(_)) {
        return Err(format!("packet {:?} must contain a JSON object", path));
    }
    packet::decode(trimmed).map_err(|e| format!("packet {:?} {}", path, packet_decode_error(&e)))
}

fn trim_json_space(data: &[u8]) -> &[u8] {
    let start = data
        .iter()
        .position(|b| !matches!(b, b' ' | b'\t' | b'\r' | b'\n'))
        .unwrap_or(data.len());
    let end = data
        .iter()
        .rposition(|b| !matches!(b, b' ' | b'\t' | b'\r' | b'\n'))
        .map_or(start, |i| i + 1);
    &data[start..end]
}

fn packet_decode_error(error: &str) -> String {
    if let Some(name) = error.strip_prefix("unknown field ") {
        return format!(
            "contains an unknown JSON field: json: unknown field {}",
            name
        );
    }
    if let Some(details) = error.strip_prefix("type mismatch|") {
        let mut parts = details.split('|');
        let path = parts.next().unwrap_or("");
        let expected = parts.next().unwrap_or("");
        let actual = match parts.next().unwrap_or("") {
            "array" => "array",
            "object" => "object",
            "string" => "string",
            "number" => "number",
            "bool" => "bool",
            _ => "null",
        };
        let evidence_item = path
            .strip_prefix("Packet.evidence.")
            .and_then(|index| index.parse::<usize>().ok())
            .is_some();
        let detail = if evidence_item {
            format!(
                "json: cannot unmarshal {} into {} of type {}",
                actual, path, expected
            )
        } else {
            format!(
                "json: cannot unmarshal {} into Go struct field {} of type {}",
                actual, path, expected
            )
        };
        return format!("is not valid JSON: {}", detail);
    }
    format!("is not valid JSON: {}", error)
}
fn result(v: (String, i32)) -> (String, i32) {
    v
}

// usage_result mirrors the reference's usageFailureWithJSON: a usage error is
// reported as the human form, or as a structured result when --json was asked
// for, and always exits 2.
fn usage_result(json: bool, message: &str, next: &str) -> (String, i32) {
    if json {
        return result(render(
            &ResultData {
                command: "usage".into(),
                ok: false,
                summary: message.into(),
                details: vec![format!("usage: {}", next)],
                next_action: format!("Run '{}'.", next),
            },
            true,
            2,
        ));
    }
    (usage(message, next), 2)
}

fn evidence_command(args: &[String]) -> (String, i32) {
    if args.len() < 2 {
        return usage_result(
            false,
            "an evidence subcommand is required",
            "ownscout evidence --help",
        );
    }
    let json = args.iter().any(|a| a == "--json");
    if args[1] != "verify" {
        let msg = format!("unknown evidence subcommand '{}'", args[1]);
        return usage_result(json, &msg, "ownscout evidence --help");
    }
    let mut repo = None;
    let mut packet_path = None;
    let mut relocate = false;
    let mut i = 2;
    while i < args.len() {
        if args[i] == "--json" {
            i += 1;
            continue;
        }
        if args[i] == "--relocate" {
            relocate = true;
            i += 1;
            continue;
        }
        let slot = match args[i].as_str() {
            "--repo" => &mut repo,
            "--packet" => &mut packet_path,
            _ => {
                return usage_result(
                    json,
                    &format!("unknown flag or argument '{}'", args[i]),
                    "ownscout evidence verify --help",
                )
            }
        };
        if i + 1 >= args.len() || args[i + 1].starts_with('-') {
            return usage_result(
                json,
                &format!("{} requires a value", args[i]),
                "ownscout evidence verify --help",
            );
        }
        *slot = Some(args[i + 1].clone());
        i += 2;
    }
    let (Some(repo), Some(path)) = (repo, packet_path) else {
        return usage_result(
            json,
            "both --repo <dir> and --packet <file> are required",
            "ownscout evidence verify --help",
        );
    };
    let p = match load_packet(&path) {
        Ok(v) => v,
        Err(e) => return load_evidence_error(json, &e),
    };
    let violations = contract::validate(&p);
    if !violations.is_empty() {
        let d = violations
            .iter()
            .map(|v| format!("{}: {}: {}", v.rule, v.field, v.message))
            .collect();
        return result(render(
            &ResultData {
                command: "evidence verify".into(),
                ok: false,
                summary: "packet is invalid".into(),
                details: d,
                next_action: "Fix the packet contract, then verify evidence again.".into(),
            },
            json,
            1,
        ));
    }
    let report = match evidence::verify_with_options(&repo, &p, &evidence::Options { relocate }) {
        Ok(v) => v,
        Err(e) => {
            return result(render(
                &ResultData {
                    command: "evidence verify".into(),
                    ok: false,
                    summary: "repository could not be checked".into(),
                    details: vec![e],
                    next_action: "Provide a readable repository directory with --repo <dir>."
                        .into(),
                },
                json,
                2,
            ))
        }
    };
    let issues = report
        .results
        .iter()
        .filter(|x| x.status != "verified")
        .map(|x| {
            format!(
                "evidence {:?} ({:?}): {}",
                x.evidence_id,
                x.path,
                if x.message.is_empty() {
                    format!("status: {}", x.status)
                } else {
                    x.message.clone()
                }
            )
        })
        .collect::<Vec<_>>();
    if !report.ok {
        return result(render(
            &ResultData {
                command: "evidence verify".into(),
                ok: false,
                summary: format!("evidence verification failed ({} issue(s))", issues.len()),
                details: issues,
                next_action: "Refresh or correct the listed evidence, then verify again.".into(),
            },
            json,
            1,
        ));
    }
    result(render(
        &ResultData {
            command: "evidence verify".into(),
            ok: true,
            summary: "evidence verified".into(),
            details: vec![format!(
                "verified {} evidence span(s)",
                report.verified_count
            )],
            next_action: "The packet is ready for its declared default action.".into(),
        },
        json,
        0,
    ))
}
fn load_evidence_error(json: bool, d: &str) -> (String, i32) {
    result(render(
        &ResultData {
            command: "evidence verify".into(),
            ok: false,
            summary: "packet could not be loaded".into(),
            details: vec![d.into()],
            next_action: "Provide a readable JSON packet with --packet <file>.".into(),
        },
        json,
        2,
    ))
}

fn node_command(args: &[String]) -> (String, i32) {
    if args.len() < 2 {
        return (
            usage("a node subcommand is required", "ownscout node --help"),
            2,
        );
    }
    if args[1] != "verify" {
        return (
            usage(
                &format!("unknown node subcommand '{}'", args[1]),
                "ownscout node --help",
            ),
            2,
        );
    }
    let json = args.iter().any(|a| a == "--json");
    let mut repo = None;
    let mut packet_path = None;
    let mut envelope_path = None;
    let mut ledger_path = None;
    let mut i = 2;
    while i < args.len() {
        if args[i] == "--json" {
            i += 1;
            continue;
        }
        let slot = match args[i].as_str() {
            "--repo" => &mut repo,
            "--packet" => &mut packet_path,
            "--envelope" => &mut envelope_path,
            "--ledger" => &mut ledger_path,
            _ => {
                return usage_result(
                    json,
                    &format!("unknown flag or argument '{}'", args[i]),
                    "ownscout node verify --help",
                )
            }
        };
        if i + 1 >= args.len() || args[i + 1].starts_with('-') {
            return usage_result(
                json,
                &format!("{} requires a value", args[i]),
                "ownscout node verify --help",
            );
        }
        *slot = Some(args[i + 1].clone());
        i += 2;
    }
    // The reference checks the four paths in this order and reports the first
    // one that is missing, so the reported flag is part of the oracle.
    let missing = [
        ("--repo", repo.is_none()),
        ("--packet", packet_path.is_none()),
        ("--envelope", envelope_path.is_none()),
        ("--ledger", ledger_path.is_none()),
    ]
    .into_iter()
    .find(|(_, is_missing)| *is_missing)
    .map(|(name, _)| name);
    if let Some(name) = missing {
        return usage_result(
            json,
            &format!("missing required {} value", name),
            "ownscout node verify --help",
        );
    }
    let (Some(repo), Some(packet_path), Some(envelope_path), Some(ledger_path)) =
        (repo, packet_path, envelope_path, ledger_path)
    else {
        return usage_result(
            json,
            "missing required --repo value",
            "ownscout node verify --help",
        );
    };
    let eb = match fs::read(&envelope_path) {
        Ok(v) => v,
        Err(_) => {
            return node_error(
                json,
                "envelope could not be parsed",
                "strict envelope parsing failed",
                2,
                "Provide one valid node-envelope-v1 JSON object with --envelope <file>.",
            )
        }
    };
    let env = match node::parse(&eb) {
        Ok(v) => v,
        Err(_) => {
            return node_error(
                json,
                "envelope could not be parsed",
                "strict envelope parsing failed",
                2,
                "Provide one valid node-envelope-v1 JSON object with --envelope <file>.",
            )
        }
    };
    let p = match load_packet(&packet_path) {
        Ok(v) => v,
        Err(e) => {
            return node_error(
                json,
                "packet could not be loaded",
                &e,
                2,
                "Fix the packet contract, then run node verification again.",
            )
        }
    };
    let violations = contract::validate(&p);
    if !violations.is_empty() {
        let d = violations
            .iter()
            .map(|v| format!("{}: {}: {}", v.rule, v.field, v.message))
            .collect();
        return result(render(
            &ResultData {
                command: "node verify".into(),
                ok: false,
                summary: format!("packet contract failed ({} violation(s))", violations.len()),
                details: d,
                next_action: "Fix the packet contract, then run node verification again.".into(),
            },
            json,
            2,
        ));
    }
    let binding = node::binding(&p);
    if node::validate(&env, &p, &binding).is_err() {
        return node_error(
            json,
            "envelope validation failed",
            "node-envelope-v1 validation failed",
            2,
            "Fix the envelope binding or graph, then run node verification again.",
        );
    }
    let report = match evidence::verify(&repo, &p) {
        Ok(v) => v,
        Err(e) => {
            return node_error(
                json,
                "repository could not be checked",
                &e,
                2,
                "Provide a readable repository directory with --repo <dir>.",
            )
        }
    };
    let eval = node::evaluate(&env, &p, &binding, &report);
    let results = eval
        .results
        .iter()
        .map(|x| ledger::NodeResult {
            node_id: x.node_id.clone(),
            status: x.status.clone(),
            reason: x.reason.clone(),
        })
        .collect();
    let mut store = match ledger::open(&ledger_path, &repo) {
        Ok(v) => v,
        Err(_) => {
            return node_error(
                json,
                "ledger could not be opened",
                "ledger open failed",
                2,
                "Provide a writable ledger path outside the repository and try again.",
            )
        }
    };
    if store
        .append(
            &sha256::hex(&sha256::digest(&eb)),
            &binding,
            "0.1.0",
            results,
        )
        .is_err()
    {
        return node_error(
            json,
            "ledger could not be updated",
            "append-only ledger update failed",
            2,
            "Provide a writable append-only ledger path outside the repository.",
        );
    }
    let details = eval
        .results
        .iter()
        .map(|x| {
            if x.reason.is_empty() {
                format!("node {:?}: {}", x.node_id, x.status)
            } else {
                format!("node {:?}: {} ({})", x.node_id, x.status, x.reason)
            }
        })
        .collect();
    if eval.ok {
        result(render(
            &ResultData {
                command: "node verify".into(),
                ok: true,
                summary: "all nodes are evidence_current".into(),
                details,
                next_action: "The node envelope is recorded and ready for its declared workflow."
                    .into(),
            },
            json,
            0,
        ))
    } else {
        result(render(
            &ResultData {
                command: "node verify".into(),
                ok: false,
                summary: "node evaluation failed".into(),
                details,
                next_action:
                    "Refresh or correct the failed evidence, then run node verification again."
                        .into(),
            },
            json,
            1,
        ))
    }
}
fn node_error(json: bool, summary: &str, detail: &str, code: i32, next: &str) -> (String, i32) {
    result(render(
        &ResultData {
            command: "node verify".into(),
            ok: false,
            summary: summary.into(),
            details: vec![detail.into()],
            next_action: next.into(),
        },
        json,
        code,
    ))
}

fn help(args: &[String]) -> (String, i32) {
    let mut text = ROOT_USAGE.to_string();
    if !args.is_empty() {
        text = match args[0].as_str() {
            "doctor" => "Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments.".into(),
            "version" => "Usage: ownscout version\n\nPrints the OwnScout version.".into(),
            "contract" => "Usage: ownscout contract validate --packet <file> [--json]\n\nValidates packet structure and outcome rules.".into(),
            "evidence" => "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nVerifies packet evidence spans against a local repository.".into(),
            "node" => "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nVerifies a node-envelope-v1 graph against fresh repository evidence and records the ordered results.".into(),
            _ => ROOT_USAGE.to_string(),
        };
    }
    if args.len() >= 2 && args[0] == "contract" && args[1] == "validate" {
        text = "Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.".into();
    }
    if args.len() >= 2 && args[0] == "evidence" && args[1] == "verify" {
        text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nValidates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun.".into();
    }
    if args.len() >= 2 && args[0] == "node" && args[1] == "verify" {
        text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nStrictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once.\n\nNext action: provide all four paths and rerun.".into();
    }
    (format!("{}\n", text), 0)
}
