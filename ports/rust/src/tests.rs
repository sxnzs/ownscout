use crate::{cli, contract, evidence, ledger, node, packet, sha256};
use std::fs;
use std::path::PathBuf;

fn temp_dir(label: &str) -> PathBuf {
    let path = std::env::temp_dir().join(format!(
        "ownscout-rust-{}-{}-{}",
        label,
        std::process::id(),
        sha256::hex(&sha256::digest(
            format!("{:?}", std::time::SystemTime::now()).as_bytes()
        ))
    ));
    fs::create_dir_all(&path).unwrap();
    path
}

fn packet() -> packet::Packet {
    packet::Packet {
        packet_id: "packet-1".into(),
        schema_version: "v1".into(),
        repo_root: "/repo".into(),
        head_commit: "abc123".into(),
        request_id: "request-1".into(),
        issued_at: "2026-09-05T00:00:00Z".into(),
        outcome: "complete".into(),
        freshness: packet::Freshness {
            head_commit: "abc123".into(),
            status: "current".into(),
            ..Default::default()
        },
        authorization: packet::Authorization {
            level: "autonomous".into(),
            ..Default::default()
        },
        budget: packet::Budget {
            max_evidence: 10,
            used_evidence: 1,
            max_bytes: 1000,
            used_bytes: 10,
        },
        evidence: Some(vec![packet::Evidence {
            evidence_id: "e1".into(),
            kind: "file".into(),
            path: "source.txt".into(),
            commit: "abc123".into(),
            line_start: 1,
            line_end: 1,
            source: "repository".into(),
            content_hash: sha256::hex(&sha256::digest(b"hello\n")),
            collected_at: "2026-09-05T00:00:00Z".into(),
            verifier_status: "verified".into(),
        }]),
        degradations: Some(Vec::new()),
        provenance: packet::Provenance {
            collector: "test".into(),
            version: "1".into(),
            ..Default::default()
        },
        packet_hash: "untrusted".into(),
    }
}

fn envelope(p: &packet::Packet, nodes: Vec<node::Node>) -> node::Envelope {
    node::Envelope {
        schema_version: "node-envelope-v1".into(),
        envelope_id: "envelope-1".into(),
        packet_id: p.packet_id.clone(),
        binding: node::binding(p),
        nodes,
    }
}

fn one_node(id: &str) -> node::Node {
    node::Node {
        node_id: id.into(),
        depends_on: Vec::new(),
        verifier: "evidence.current".into(),
        evidence_ids: vec!["e1".into()],
    }
}

fn valid_report() -> evidence::Report {
    evidence::Report {
        results: vec![evidence::Verification {
            evidence_id: "e1".into(),
            path: "source.txt".into(),
            status: "verified".into(),
            actual_hash: String::new(),
            message: String::new(),
        }],
        verified_count: 1,
        failed_count: 0,
        skipped_count: 0,
        ok: true,
    }
}

#[test]
fn packet_decode_rejects_strict_json_errors() {
    for input in [
        b"".as_slice(),
        br#"{"packet_id":1}"#.as_slice(),
        br#"{"unknown":true}"#.as_slice(),
        br#"{} trailing"#.as_slice(),
        br#"{"freshness":[]}"#.as_slice(),
        br#"{"evidence":[{"evidence_id":1}]}"#.as_slice(),
    ] {
        assert!(packet::decode_strict(input).is_err(), "accepted {:?}", input);
    }
    assert!(packet::decode_strict(b"null").is_err());
    assert!(packet::decode_strict(&vec![b' '; (1 << 20) + 1]).is_err());
}

#[test]
fn packet_decode_is_strict_everywhere() {
    // The retired lenient adapter tolerated nulls, duplicate keys, case
    // variants and unpaired surrogate escapes; the single strict boundary
    // rejects all of them.
    for input in [
        br#"{"packet_id":null}"#.as_slice(),
        br#"{"packet_id":"first","packet_id":"last"}"#.as_slice(),
        br#"{"SCHEMA_VERSION":"v1"}"#.as_slice(),
        br#"{"freshness":{"extra":true}}"#.as_slice(),
        br#"{"packet_id":"\uD800"}"#.as_slice(),
        b"{\"packet_id\":\"\xff\"}".as_slice(),
    ] {
        assert!(packet::decode_strict(input).is_err(), "accepted {:?}", input);
    }
    assert_eq!(
        packet::decode_strict(br#"{"packet_id":"p"}"#)
            .unwrap()
            .packet_id,
        "p"
    );
}

#[test]
fn contract_actions_and_validation_cover_outcomes() {
    let mut p = packet();
    assert!(contract::validate(&p).is_empty());
    for (outcome, action) in [
        ("complete", "autonomous_proceed"),
        ("partial", "bounded_more_evidence"),
        ("stale", "bounded_refresh"),
        ("blocked", "blocked"),
        ("failed_verification", "quarantine"),
        ("budget_exhausted", "human_approval"),
    ] {
        p.outcome = outcome.into();
        if outcome != "complete" {
            p.authorization.level = "human".into();
        }
        assert_eq!(contract::action(outcome), Some(action));
    }
    p.outcome = "complete".into();
    p.freshness.status = "stale".into();
    assert!(contract::validate(&p)
        .iter()
        .any(|v| v.field == "freshness.status"));
    p = packet();
    p.budget.used_bytes = p.budget.max_bytes + 1;
    assert!(contract::validate(&p)
        .iter()
        .any(|v| v.field == "budget.used_bytes"));
    p = packet();
    p.authorization.level = "autonomous".into();
    p.outcome = "blocked".into();
    assert!(contract::validate(&p)
        .iter()
        .any(|v| v.field == "authorization.level"));
}

#[test]
fn contract_checks_evidence_shape_and_complete_requirements() {
    let mut p = packet();
    p.evidence.as_mut().unwrap()[0].line_start = 0;
    p.evidence.as_mut().unwrap()[0].line_end = 0;
    p.evidence.as_mut().unwrap()[0].verifier_status = "unknown".into();
    p.degradations = Some(vec![" ".into()]);
    let violations = contract::validate(&p);
    assert!(violations.iter().any(|v| v.field.ends_with("line_start")));
    assert!(violations.iter().any(|v| v.field.ends_with("line_end")));
    assert!(violations
        .iter()
        .any(|v| v.field.ends_with("verifier_status")));
    assert!(violations.iter().any(|v| v.field == "degradations[0]"));
    p = packet();
    p.evidence = Some(Vec::new());
    assert!(contract::validate(&p)
        .iter()
        .any(|v| v.message.contains("at least one evidence")));
    p = packet();
    p.degradations = Some(vec!["limited".into()]);
    assert!(contract::validate(&p)
        .iter()
        .any(|v| v.message.contains("cannot contain degradations")));
}

#[test]
fn evidence_verification_hashes_selected_lines_and_normalizes_crlf() {
    let root = temp_dir("evidence");
    fs::write(root.join("source.txt"), b"one\r\ntwo\r\n").unwrap();
    let mut p = packet();
    p.evidence.as_mut().unwrap()[0].line_end = 2;
    p.evidence.as_mut().unwrap()[0].content_hash = sha256::hex(&sha256::digest(b"one\ntwo\n"));
    let report = evidence::verify(root.to_str().unwrap(), &p).unwrap();
    assert!(report.ok);
    assert_eq!(report.verified_count, 1);
    assert_eq!(report.results[0].status, "verified");
    p.evidence.as_mut().unwrap()[0].content_hash = "0".repeat(64);
    let failed = evidence::verify(root.to_str().unwrap(), &p).unwrap();
    assert!(!failed.ok);
    assert!(failed.results[0].message.contains("mismatch"));
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn evidence_verification_rejects_missing_unsafe_and_invalid_ranges() {
    let root = temp_dir("evidence-errors");
    let mut p = packet();
    for (path, expected) in [
        ("missing.txt", "cannot access evidence path"),
        ("../source.txt", "parent component"),
        ("/tmp/source.txt", "repository-relative"),
    ] {
        p.evidence.as_mut().unwrap()[0].path = path.into();
        let r = evidence::verify(root.to_str().unwrap(), &p).unwrap();
        assert!(
            r.results[0].message.contains(expected),
            "{:?}",
            r.results[0].message
        );
    }
    fs::write(root.join("source.txt"), b"line\n").unwrap();
    p.evidence.as_mut().unwrap()[0].path = "source.txt".into();
    p.evidence.as_mut().unwrap()[0].line_end = 2;
    let r = evidence::verify(root.to_str().unwrap(), &p).unwrap();
    assert!(r.results[0].message.contains("invalid line range"));
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn node_parser_rejects_boundaries_and_duplicates() {
    let p = packet();
    let b = node::binding(&p);
    let valid = format!(
        r#"{{"schema_version":"node-envelope-v1","envelope_id":"env","packet_id":"packet-1","packet_binding_sha256":"{}","nodes":[{{"node_id":"a","depends_on":[],"verifier":"evidence.current","evidence_ids":["e1"]}}]}}"#,
        b
    );
    let e = node::parse(valid.as_bytes()).unwrap();
    assert_eq!(e.nodes.len(), 1);
    for bad in [
        valid.replace("\"node_id\":\"a\"", "\"extra\":1,\"node_id\":\"a\""),
        valid.replace("\"evidence_ids\":[\"e1\"]", "\"evidence_ids\":[null]"),
        format!("{} null", valid),
        valid.replace("\"node_id\":\"a\"", "\"node_id\":\"a\",\"node_id\":\"a\""),
    ] {
        assert!(node::parse(bad.as_bytes()).is_err());
    }
    assert!(node::parse(&vec![b' '; (1 << 20) + 1]).is_err());
}

#[test]
fn node_validation_orders_nodes_and_blocks_dependents() {
    let p = packet();
    let mut a = one_node("a");
    a.evidence_ids = vec!["e1".into()];
    let mut b = one_node("b");
    b.depends_on = vec!["a".into()];
    let e = envelope(&p, vec![b, a]);
    let order = node::validate(&e, &p, &node::binding(&p)).unwrap();
    assert_eq!(
        order
            .iter()
            .map(|i| e.nodes[*i].node_id.as_str())
            .collect::<Vec<_>>(),
        vec!["a", "b"]
    );
    let result = node::evaluate(&e, &p, &node::binding(&p), &valid_report());
    assert!(result.ok);
    assert_eq!(result.results[0].node_id, "a");
    let mut bad = valid_report();
    bad.results[0].status = "failed".into();
    let result = node::evaluate(&e, &p, &node::binding(&p), &bad);
    assert!(!result.ok);
    assert_eq!(result.results[0].status, "failed");
    assert_eq!(result.results[1].status, "blocked");
}

#[test]
fn node_validation_rejects_graph_and_binding_errors() {
    let p = packet();
    let mut e = envelope(&p, vec![one_node("a")]);
    e.nodes[0].depends_on = vec!["missing".into()];
    assert!(node::validate(&e, &p, &node::binding(&p))
        .unwrap_err()
        .contains("missing"));
    e.nodes[0].depends_on = vec!["a".into()];
    assert!(node::validate(&e, &p, &node::binding(&p))
        .unwrap_err()
        .contains("self"));
    e.nodes[0].depends_on.clear();
    assert!(node::validate(&e, &p, &"0".repeat(64)).is_err());
    e.packet_id = "other".into();
    assert!(node::validate(&e, &p, &node::binding(&p)).is_err());
}

#[test]
fn node_validation_enforces_reference_limits_and_identifier_syntax() {
    let p = packet();
    let mut e = envelope(&p, vec![one_node("a")]);
    let refs = (0..129)
        .map(|i| format!("\"n{}\"", i))
        .collect::<Vec<_>>()
        .join(",");
    let raw = format!(
        r#"{{"schema_version":"node-envelope-v1","envelope_id":"env","packet_id":"packet-1","packet_binding_sha256":"{}","nodes":[{{"node_id":"a","depends_on":[{}],"verifier":"evidence.current","evidence_ids":["e1"]}}]}}"#,
        node::binding(&p),
        refs
    );
    let err = match node::parse(raw.as_bytes()) {
        Ok(_) => panic!("reference limit was ignored"),
        Err(err) => err,
    };
    assert!(err.contains("depends_on exceeds 128"));
    e.nodes[0].evidence_ids = vec!["e1".into()];
    e.nodes[0].node_id = ".bad".into();
    assert!(node::validate(&e, &p, &node::binding(&p))
        .unwrap_err()
        .contains("valid identifier"));
    e.nodes[0].node_id = "a".into();
    e.nodes[0].depends_on = (0..129).map(|i| format!("n{}", i)).collect();
    assert!(node::validate(&e, &p, &node::binding(&p))
        .unwrap_err()
        .contains("missing"));
}

#[test]
fn node_parser_rejects_empty_evidence_ids() {
    let p = packet();
    let raw = format!(
        r#"{{"schema_version":"node-envelope-v1","envelope_id":"env","packet_id":"packet-1","packet_binding_sha256":"{}","nodes":[{{"node_id":"a","depends_on":[],"verifier":"evidence.current","evidence_ids":[]}}]}}"#,
        node::binding(&p)
    );
    let err = match node::parse(raw.as_bytes()) {
        Ok(_) => panic!("empty evidence_ids was accepted"),
        Err(err) => err,
    };
    assert!(err.contains("non-empty"), "{err}");
}

#[test]
fn node_validation_checks_every_identifier_field() {
    let p = packet();
    let b = node::binding(&p);
    // envelope_id: the length rule and the character rule are distinct.
    for (value, expected) in [
        ("env%lope-1", "is not a valid identifier"),
        ("-env-1", "is not a valid identifier"),
        ("e".repeat(129).as_str(), "1-128 ASCII identifier characters"),
        ("", "1-128 ASCII identifier characters"),
    ] {
        let mut e = envelope(&p, vec![one_node("a")]);
        e.envelope_id = value.into();
        let err = node::validate(&e, &p, &b).unwrap_err();
        assert!(err.contains(expected), "{value:?}: {err}");
    }
    // packet_id is an identifier too, before it is compared to the packet.
    let mut e = envelope(&p, vec![one_node("a")]);
    e.packet_id = "bad%id".into();
    assert!(node::validate(&e, &p, &b)
        .unwrap_err()
        .contains("packet_id"));
    // depends_on and evidence_ids entries are validated.
    let mut e = envelope(&p, vec![one_node("a")]);
    e.nodes[0].depends_on = vec!["bad%dep".into()];
    assert!(node::validate(&e, &p, &b)
        .unwrap_err()
        .contains("is not a valid identifier"));
    let mut e = envelope(&p, vec![one_node("a")]);
    e.nodes[0].evidence_ids = vec!["bad%ev".into()];
    assert!(node::validate(&e, &p, &b)
        .unwrap_err()
        .contains("is not a valid identifier"));
    // Punctuation after the first byte is accepted.
    let mut e = envelope(&p, vec![one_node("a")]);
    e.envelope_id = "env.1_-:x".into();
    assert!(node::validate(&e, &p, &b).is_ok());
}

#[test]
fn ledger_appends_hash_chained_records_and_reopens() {
    let root = temp_dir("ledger-root");
    let outside = temp_dir("ledger-outside").join("ledger.jsonl");
    let hash = "a".repeat(64);
    let mut store = ledger::open(outside.to_str().unwrap(), root.to_str().unwrap()).unwrap();
    let first = store
        .append(
            &hash,
            &"b".repeat(64),
            "0.1.0",
            vec![ledger::NodeResult {
                node_id: "build".into(),
                status: "evidence_current".into(),
                reason: String::new(),
            }],
        )
        .unwrap();
    let second = store
        .append(
            &hash,
            &"b".repeat(64),
            "0.1.0",
            vec![ledger::NodeResult {
                node_id: "deploy".into(),
                status: "blocked".into(),
                reason: "approval".into(),
            }],
        )
        .unwrap();
    assert_eq!(first.seq, 1);
    assert_eq!(second.seq, 2);
    drop(store);
    drop(ledger::open(outside.to_str().unwrap(), root.to_str().unwrap()).unwrap());
    let data = fs::read(&outside).unwrap();
    assert_eq!(data.iter().filter(|x| **x == b'\n').count(), 2);
    fs::remove_dir_all(root).unwrap();
    fs::remove_dir_all(outside.parent().unwrap()).unwrap();
}

#[test]
fn ledger_rejects_malformed_existing_files() {
    let root = temp_dir("ledger-malformed-root");
    for (name, contents, expected) in [
        (
            "truncated",
            b"{\"schema_version\":".as_slice(),
            "end with lf",
        ),
        ("no-final-lf", b"{}".as_slice(), "end with lf"),
        ("blank-line", b"{}\n\n".as_slice(), "seq must"),
    ] {
        let dir = temp_dir(name);
        let path = dir.join("ledger");
        fs::write(&path, contents).unwrap();
        let err = match ledger::open(path.to_str().unwrap(), root.to_str().unwrap()) {
            Ok(_) => panic!("malformed ledger was accepted"),
            Err(err) => err,
        };
        assert!(err.to_ascii_lowercase().contains(expected), "{err}");
        fs::remove_dir_all(dir).unwrap();
    }
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn ledger_rejects_invalid_inputs_and_repository_paths() {
    let root = temp_dir("ledger-invalid");
    assert!(ledger::open(
        root.join("ledger").to_str().unwrap(),
        root.to_str().unwrap()
    )
    .is_err());
    let outside = temp_dir("ledger-invalid-outside").join("ledger");
    let mut store = ledger::open(outside.to_str().unwrap(), root.to_str().unwrap()).unwrap();
    assert!(store
        .append(&"A".repeat(64), &"b".repeat(64), "v", vec![])
        .is_err());
    assert!(store
        .append(&"g".repeat(64), &"b".repeat(64), "v", vec![])
        .is_err());
    assert!(store
        .append(
            &"a".repeat(64),
            &"b".repeat(64),
            "",
            vec![ledger::NodeResult {
                node_id: "x".into(),
                status: "evidence_current".into(),
                reason: String::new()
            }]
        )
        .is_err());
    drop(store);
    fs::remove_dir_all(root).unwrap();
    fs::remove_dir_all(outside.parent().unwrap()).unwrap();
}

#[test]
fn cli_ledger_verify_reports_summary_and_errors() {
    let root = temp_dir("ledger-verify-root");
    let outside = temp_dir("ledger-verify-outside").join("ledger.jsonl");
    let mut store = ledger::open(outside.to_str().unwrap(), root.to_str().unwrap()).unwrap();
    store
        .append(
            &"a".repeat(64),
            &"b".repeat(64),
            "0.1.0",
            vec![ledger::NodeResult {
                node_id: "build".into(),
                status: "evidence_current".into(),
                reason: String::new(),
            }],
        )
        .unwrap();
    drop(store);
    let run = |path: &std::path::Path| {
        cli::run(&[
            "ledger".into(),
            "verify".into(),
            "--ledger".into(),
            path.to_str().unwrap().into(),
            "--json".into(),
        ])
    };
    let (out, code) = run(&outside);
    assert_eq!(code, 0, "{out}");
    assert!(out.contains("\"summary\":\"ledger is intact\""), "{out}");
    assert!(out.contains("1 record(s), tip "), "{out}");
    // A malformed record is a validation failure carrying Go's scanner error.
    fs::write(&outside, b"not a ledger record\n").unwrap();
    let (out, code) = run(&outside);
    assert_eq!(code, 1, "{out}");
    assert!(
        out.contains("invalid character 'o' in literal null (expecting 'u')"),
        "{out}"
    );
    // A missing path and a directory are read failures.
    let (out, code) = run(&outside.with_extension("absent"));
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("ledger could not be read"), "{out}");
    let (out, code) = run(&root);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("not a regular file"), "{out}");
    fs::remove_dir_all(root).unwrap();
    fs::remove_dir_all(outside.parent().unwrap()).unwrap();
}

#[test]
fn cli_node_bind_and_verify_relocate_dispatch() {
    let p = packet();
    let dir = temp_dir("node-bind");
    let path = dir.join("packet.json");
    fs::write(&path, node::canonical_packet(&p)).unwrap();
    let expected = node::binding(&p);
    let (out, code) = cli::run(&[
        "node".into(),
        "bind".into(),
        "--packet".into(),
        path.to_str().unwrap().into(),
        "--json".into(),
    ]);
    assert_eq!(code, 0, "{out}");
    assert!(out.contains(&expected), "{out}");
    // A missing --packet is a usage error.
    let (out, code) = cli::run(&["node".into(), "bind".into()]);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("missing required --packet value"), "{out}");
    // The strict decoder rejects a duplicate key.
    fs::write(&path, br#"{"packet_id":"a","packet_id":"b"}"#).unwrap();
    let (out, code) = cli::run(&[
        "node".into(),
        "bind".into(),
        "--packet".into(),
        path.to_str().unwrap().into(),
        "--json".into(),
    ]);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("packet could not be decoded"), "{out}");
    // node verify accepts --relocate as a switch.
    let (out, code) = cli::run(&["node".into(), "verify".into(), "--relocate".into()]);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("missing required --repo value"), "{out}");
    fs::remove_dir_all(dir).unwrap();
}

#[test]
fn cli_help_covers_ledger_and_node_bind() {
    let (out, code) = cli::run(&["--help".into()]);
    assert_eq!(code, 0);
    assert!(
        out.contains("ownscout ledger verify --ledger <file> [--json]"),
        "{out}"
    );
    assert!(
        out.contains("ownscout node bind --packet <file> [--json]"),
        "{out}"
    );
    let (out, _) = cli::run(&["node".into(), "bind".into(), "--help".into()]);
    assert!(out.contains("Computes the canonical packet binding"), "{out}");
    let (out, _) = cli::run(&["ledger".into(), "verify".into(), "--help".into()]);
    assert!(out.contains("Replays the SHA-256 ledger chain"), "{out}");
    let (out, code) = cli::run(&["ledger".into()]);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("a ledger subcommand is required"), "{out}");
    let (out, code) = cli::run(&["ledger".into(), "bogus".into()]);
    assert_eq!(code, 2, "{out}");
    assert!(out.contains("unknown ledger subcommand 'bogus'"), "{out}");
}

#[test]
fn cli_handles_help_version_and_usage_errors() {
    let (out, code) = cli::run(&["version".into()]);
    assert_eq!(code, 0);
    assert!(out.contains("ownscout 0.1.0"));
    let (out, code) = cli::run(&["--help".into()]);
    assert_eq!(code, 0);
    assert!(out.contains("Usage:"));
    let (out, code) = cli::run(&[]);
    assert_eq!(code, 2);
    assert!(out.contains("a command is required"));
    let (out, code) = cli::run(&["doctor".into(), "extra".into()]);
    assert_eq!(code, 2);
    assert!(out.contains("does not accept arguments"));
}

#[test]
fn cli_contract_validates_packet_and_supports_json_errors() {
    let dir = temp_dir("cli");
    let path = dir.join("packet.json");
    fs::write(&path, br#"{"packet_id":"p","schema_version":"v1"}"#).unwrap();
    let (out, code) = cli::run(&[
        "contract".into(),
        "validate".into(),
        "--packet".into(),
        path.to_str().unwrap().into(),
        "--json".into(),
    ]);
    assert_eq!(code, 1);
    assert!(out.contains("\"ok\":false"));
    let (out, code) = cli::run(&[
        "contract".into(),
        "validate".into(),
        "--packet".into(),
        dir.join("missing").to_str().unwrap().into(),
    ]);
    assert_eq!(code, 2);
    assert!(out.contains("could not be loaded"));
    fs::remove_dir_all(dir).unwrap();
}

#[test]
fn cli_rejects_unknown_subcommands_and_missing_arguments() {
    let (out, code) = cli::run(&["evidence".into(), "future".into()]);
    assert_eq!(code, 2);
    assert!(out.contains("unknown evidence subcommand"));
    let (out, code) = cli::run(&["node".into(), "verify".into()]);
    assert_eq!(code, 2);
    assert!(out.contains("--repo"));
    let (out, code) = cli::run(&["contract".into(), "validate".into(), "--json".into()]);
    assert_eq!(code, 2);
    assert!(out.contains("--packet"));
}

#[test]
fn sha256_matches_known_vectors() {
    assert_eq!(
        sha256::hex(&sha256::digest(b"")),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    assert_eq!(
        sha256::hex(&sha256::digest(b"abc")),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );
}
