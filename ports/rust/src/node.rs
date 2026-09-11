use crate::{
    evidence::Report,
    json::{object, unique_object, Value},
    packet::Packet,
    sha256,
};
use std::collections::{HashMap, HashSet};

pub struct Envelope {
    pub schema_version: String,
    pub envelope_id: String,
    pub packet_id: String,
    pub binding: String,
    pub nodes: Vec<Node>,
}
pub struct Node {
    pub node_id: String,
    pub depends_on: Vec<String>,
    pub verifier: String,
    pub evidence_ids: Vec<String>,
}
pub struct NodeResult {
    pub node_id: String,
    pub status: String,
    pub reason: String,
}
pub struct Evaluation {
    pub ok: bool,
    pub results: Vec<NodeResult>,
}

fn map<'a>(
    v: &'a Value,
    allowed: &[&str],
) -> Result<std::collections::BTreeMap<&'a str, &'a Value>, String> {
    let m = unique_object(object(v)?)?;
    for k in m.keys() {
        if !allowed.contains(k) {
            return Err(format!("unknown field {:?}", k));
        }
    }
    Ok(m)
}
fn strv(m: &std::collections::BTreeMap<&str, &Value>, k: &str) -> Result<String, String> {
    match m.get(k) {
        Some(Value::String(v)) => Ok(v.clone()),
        _ => Err(format!("{}: expected string", k)),
    }
}
fn arr(
    m: &std::collections::BTreeMap<&str, &Value>,
    k: &str,
    limit: usize,
) -> Result<Vec<String>, String> {
    match m.get(k) {
        Some(Value::Array(v)) => {
            if v.len() > limit {
                return Err(format!("{} exceeds {} reference limit", k, limit));
            }
            v.iter()
                .map(|x| match x {
                    Value::String(s) => Ok(s.clone()),
                    _ => Err(format!("{}: expected string", k)),
                })
                .collect()
        }
        _ => Err(format!("{}: expected array", k)),
    }
}
pub fn parse(data: &[u8]) -> Result<Envelope, String> {
    if data.len() > 1 << 20 {
        return Err("envelope exceeds 1048576 byte input limit".into());
    }
    if std::str::from_utf8(data).is_err() {
        return Err("parse envelope: input is not valid UTF-8".into());
    }
    let v = crate::json::parse(data).map_err(|e| format!("parse envelope: {}", e))?;
    let m = map(
        &v,
        &[
            "schema_version",
            "envelope_id",
            "packet_id",
            "packet_binding_sha256",
            "nodes",
        ],
    )?;
    let ns = match m.get("nodes") {
        Some(Value::Array(v)) => v,
        _ => return Err("nodes: expected array".into()),
    };
    if ns.len() > 256 {
        return Err("nodes exceeds 256 node limit".into());
    }
    let mut nodes = Vec::new();
    for x in ns {
        let n = map(x, &["node_id", "depends_on", "verifier", "evidence_ids"])?;
        nodes.push(Node {
            node_id: strv(&n, "node_id")?,
            depends_on: arr(&n, "depends_on", 128)?,
            verifier: strv(&n, "verifier")?,
            evidence_ids: arr(&n, "evidence_ids", 128)?,
        });
    }
    if nodes.is_empty() {
        return Err("nodes must contain at least one node".into());
    }
    Ok(Envelope {
        schema_version: strv(&m, "schema_version")?,
        envelope_id: strv(&m, "envelope_id")?,
        packet_id: strv(&m, "packet_id")?,
        binding: strv(&m, "packet_binding_sha256")?,
        nodes,
    })
}
pub fn binding(p: &Packet) -> String {
    let mut q = p.clone();
    q.packet_hash.clear();
    sha256::hex(&sha256::digest(&canonical_packet(&q)))
}
fn esc(s: &str) -> String {
    let mut o = String::new();
    for c in s.chars() {
        match c {
            '"' => o.push_str("\\\""),
            '\\' => o.push_str("\\\\"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            '\t' => o.push_str("\\t"),
            '<' => o.push_str("\\u003c"),
            '>' => o.push_str("\\u003e"),
            '&' => o.push_str("\\u0026"),
            c if c.is_control() => o.push_str(&format!("\\u{:04x}", c as u32)),
            c => o.push(c),
        }
    }
    o
}
fn js(s: &str) -> String {
    format!("\"{}\"", esc(s))
}
pub fn canonical_packet(p: &Packet) -> Vec<u8> {
    let f = &p.freshness;
    let a = &p.authorization;
    let b = &p.budget;
    let mut o=format!("{{\"packet_id\":{},\"schema_version\":{},\"repo_root\":{},\"head_commit\":{},\"request_id\":{},\"issued_at\":{},\"outcome\":{},\"freshness\":{{\"head_commit\":{}",js(&p.packet_id),js(&p.schema_version),js(&p.repo_root),js(&p.head_commit),js(&p.request_id),js(&p.issued_at),js(&p.outcome),js(&f.head_commit));
    if !f.head_anchor.is_empty() {
        o += &format!(",\"head_anchor\":{}", js(&f.head_anchor));
    }
    if !f.status.is_empty() {
        o += &format!(",\"status\":{}", js(&f.status));
    }
    if f.current {
        o += ",\"current\":true";
    }
    if f.is_current {
        o += ",\"is_current\":true";
    }
    if !f.checked_at.is_empty() {
        o += &format!(",\"checked_at\":{}", js(&f.checked_at));
    }
    o += &format!("}},\"authorization\":{{\"level\":{}", js(&a.level));
    if !a.reason.is_empty() {
        o += &format!(",\"reason\":{}", js(&a.reason));
    }
    o+=&format!("}},\"budget\":{{\"max_evidence\":{},\"used_evidence\":{},\"max_bytes\":{},\"used_bytes\":{}}},\"evidence\":",b.max_evidence,b.used_evidence,b.max_bytes,b.used_bytes);
    if let Some(es) = &p.evidence {
        o.push('[');
        for (i, e) in es.iter().enumerate() {
            if i > 0 {
                o.push(',')
            }
            o+=&format!("{{\"evidence_id\":{},\"kind\":{},\"path\":{},\"commit\":{},\"line_start\":{},\"line_end\":{},\"source\":{},\"content_hash\":{},\"collected_at\":{},\"verifier_status\":{}}}",js(&e.evidence_id),js(&e.kind),js(&e.path),js(&e.commit),e.line_start,e.line_end,js(&e.source),js(&e.content_hash),js(&e.collected_at),js(&e.verifier_status));
        }
        o.push(']');
    } else {
        o += "null";
    }
    o += ",\"degradations\":";
    if let Some(ds) = &p.degradations {
        o.push('[');
        for (i, d) in ds.iter().enumerate() {
            if i > 0 {
                o.push(',')
            }
            o += &js(d);
        }
        o.push(']');
    } else {
        o += "null";
    }
    let pr = &p.provenance;
    o += &format!(",\"provenance\":{{");
    let mut first = true;
    for (k, v) in [
        ("collector", &pr.collector),
        ("tool", &pr.tool),
        ("version", &pr.version),
        ("tool_version", &pr.tool_version),
    ] {
        if !v.is_empty() {
            if !first {
                o.push(',')
            }
            first = false;
            o += &format!("{}:{}", js(k), js(v));
        }
    }
    o += &format!("}},\"packet_hash\":{}}}", js(&p.packet_hash));
    o.into_bytes()
}
pub fn validate(e: &Envelope, p: &Packet, b: &str) -> Result<Vec<usize>, String> {
    let _envelope_id = &e.envelope_id;
    if e.schema_version != "node-envelope-v1" {
        return Err("schema_version must equal \"node-envelope-v1\"".into());
    }
    if e.packet_id != p.packet_id {
        return Err("packet_id does not match packet".into());
    }
    let computed = binding(p);
    if b != computed || e.binding != computed {
        return Err("packet_binding_sha256 does not match computed packet binding".into());
    }
    let mut evid = HashSet::new();
    for x in p.evidence.as_ref().into_iter().flatten() {
        if !valid_id(&x.evidence_id) {
            return Err("packet evidence_id must contain 1-128 ASCII identifier characters".into());
        }
        if !evid.insert(x.evidence_id.clone()) {
            return Err(format!("duplicate packet evidence_id {:?}", x.evidence_id));
        }
    }
    let mut ix = HashMap::new();
    for (i, n) in e.nodes.iter().enumerate() {
        if !valid_id(&n.node_id) {
            return Err(format!("node_id {:?} is not a valid identifier", n.node_id));
        }
        if ix.insert(n.node_id.clone(), i).is_some() {
            return Err(format!("duplicate node_id {:?}", n.node_id));
        }
        if n.verifier != "evidence.current" {
            return Err(format!(
                "node {:?}: verifier must equal \"evidence.current\"",
                n.node_id
            ));
        }
        refs(&n.depends_on, "depends_on")?;
        refs(&n.evidence_ids, "evidence_ids")?;
        for id in &n.evidence_ids {
            if !evid.contains(id) {
                return Err(format!(
                    "node {:?}: evidence {:?} is not present in packet",
                    n.node_id, id
                ));
            }
        }
    }
    let mut total = 0;
    for n in &e.nodes {
        total += n.depends_on.len() + n.evidence_ids.len();
        for id in &n.depends_on {
            if id == &n.node_id {
                return Err(format!("node {:?}: self-dependency", n.node_id));
            }
            if !ix.contains_key(id) {
                return Err(format!("node {:?}: missing dependency {:?}", n.node_id, id));
            }
        }
    }
    if total > 4096 {
        return Err("envelope exceeds 4096 total reference limit".into());
    }
    let mut d = vec![0; e.nodes.len()];
    let mut children = vec![Vec::new(); e.nodes.len()];
    let mut ready = Vec::new();
    for (i, n) in e.nodes.iter().enumerate() {
        d[i] = n.depends_on.len();
        if d[i] == 0 {
            ready.push(i)
        }
        for x in &n.depends_on {
            children[ix[x]].push(i)
        }
    }
    let mut order = Vec::new();
    while !ready.is_empty() {
        ready.sort_by(|a, b| e.nodes[*a].node_id.cmp(&e.nodes[*b].node_id));
        let i = ready.remove(0);
        order.push(i);
        for c in &children[i] {
            d[*c] -= 1;
            if d[*c] == 0 {
                ready.push(*c)
            }
        }
    }
    if order.len() != e.nodes.len() {
        return Err("node dependency graph contains a cycle".into());
    }
    Ok(order)
}
fn refs(v: &[String], f: &str) -> Result<(), String> {
    let mut s = HashSet::new();
    for x in v {
        if !valid_id(x) {
            return Err(format!("{} {:?} is not a valid identifier", f, x));
        }
        if !s.insert(x) {
            return Err(format!("duplicate {} reference {:?}", f, x));
        }
    }
    Ok(())
}
fn valid_id(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 128
        && s.bytes().enumerate().all(|(i, c)| {
            c.is_ascii_alphanumeric() || (i > 0 && matches!(c, b'.' | b'_' | b'-' | b':'))
        })
}
pub fn evaluate(e: &Envelope, p: &Packet, b: &str, r: &Report) -> Evaluation {
    let order = match validate(e, p, b) {
        Ok(v) => v,
        Err(_) => {
            return Evaluation {
                ok: false,
                results: Vec::new(),
            }
        }
    };
    let mut cur = HashMap::new();
    for x in &r.results {
        let old = cur.get(&x.evidence_id).copied();
        cur.insert(
            x.evidence_id.clone(),
            x.status == "verified" && old.unwrap_or(true),
        );
    }
    let mut status = HashMap::new();
    let mut out = Vec::new();
    let mut ok = true;
    for i in order {
        let n = &e.nodes[i];
        let mut s = "evidence_current";
        let mut reason = String::new();
        for dep in &n.depends_on {
            if status.get(dep).copied() != Some("evidence_current") {
                s = "blocked";
                reason = format!("dependency {:?} is not evidence_current", dep);
                break;
            }
        }
        if s == "evidence_current" {
            for id in &n.evidence_ids {
                if !cur.get(id).copied().unwrap_or(false) {
                    s = "failed";
                    reason = format!("evidence {:?} is missing or not verified", id);
                    break;
                }
            }
        }
        if s != "evidence_current" {
            ok = false
        }
        status.insert(n.node_id.clone(), s);
        out.push(NodeResult {
            node_id: n.node_id.clone(),
            status: s.into(),
            reason,
        });
    }
    Evaluation { ok, results: out }
}
