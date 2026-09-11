use crate::json::{object, unique_object, Value};

#[derive(Clone, Default)]
pub struct Packet {
    pub packet_id: String,
    pub schema_version: String,
    pub repo_root: String,
    pub head_commit: String,
    pub request_id: String,
    pub issued_at: String,
    pub outcome: String,
    pub freshness: Freshness,
    pub authorization: Authorization,
    pub budget: Budget,
    pub evidence: Option<Vec<Evidence>>,
    pub degradations: Option<Vec<String>>,
    pub provenance: Provenance,
    pub packet_hash: String,
}
#[derive(Clone, Default)]
pub struct Freshness {
    pub head_commit: String,
    pub head_anchor: String,
    pub status: String,
    pub current: bool,
    pub is_current: bool,
    pub checked_at: String,
}
#[derive(Clone, Default)]
pub struct Authorization {
    pub level: String,
    pub reason: String,
}
#[derive(Clone, Default)]
pub struct Budget {
    pub max_evidence: i64,
    pub used_evidence: i64,
    pub max_bytes: i64,
    pub used_bytes: i64,
}
#[derive(Clone, Default)]
pub struct Evidence {
    pub evidence_id: String,
    pub kind: String,
    pub path: String,
    pub commit: String,
    pub line_start: i64,
    pub line_end: i64,
    pub source: String,
    pub content_hash: String,
    pub collected_at: String,
    pub verifier_status: String,
}
#[derive(Clone, Default)]
pub struct Provenance {
    pub collector: String,
    pub tool: String,
    pub version: String,
    pub tool_version: String,
}

fn fields<'a>(
    value: &'a Value,
    allowed: &[&str],
) -> Result<std::collections::BTreeMap<&'a str, &'a Value>, String> {
    let fields = object(value)?;
    let map = unique_object(fields)?;
    for key in map.keys() {
        if !allowed.contains(key) {
            return Err(format!("unknown field {:?}", key));
        }
    }
    Ok(map)
}
fn string(map: &std::collections::BTreeMap<&str, &Value>, key: &str) -> Result<String, String> {
    match map.get(key) {
        None => Ok(String::new()),
        Some(Value::String(v)) => Ok(v.clone()),
        Some(Value::Null) => Ok(String::new()),
        Some(_) => Err(format!("{}. expected string", key)),
    }
}
fn integer(map: &std::collections::BTreeMap<&str, &Value>, key: &str) -> Result<i64, String> {
    match map.get(key) {
        None => Ok(0),
        Some(Value::Number(v)) => v.parse().map_err(|_| format!("{}. expected integer", key)),
        Some(Value::Null) => Ok(0),
        Some(_) => Err(format!("{}. expected integer", key)),
    }
}
fn boolean(map: &std::collections::BTreeMap<&str, &Value>, key: &str) -> Result<bool, String> {
    match map.get(key) {
        None => Ok(false),
        Some(Value::Bool(v)) => Ok(*v),
        Some(Value::Null) => Ok(false),
        Some(_) => Err(format!("{}. expected boolean", key)),
    }
}
fn strings(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
) -> Result<Option<Vec<String>>, String> {
    match map.get(key) {
        None => Ok(None),
        Some(Value::Array(items)) => items
            .iter()
            .map(|v| match v {
                Value::String(s) => Ok(s.clone()),
                _ => Err(format!("{}. expected string", key)),
            })
            .collect::<Result<Vec<_>, _>>()
            .map(Some),
        Some(Value::Null) => Ok(None),
        Some(_) => Err(format!("{}. expected array", key)),
    }
}
pub fn decode(data: &[u8]) -> Result<Packet, String> {
    if data.len() > 1 << 20 {
        return Err("nodepacket: packet: input exceeds 1 MiB".into());
    }
    let value = crate::json::parse(data).map_err(|e| format!("nodepacket: packet: {}", e))?;
    let map = fields(
        &value,
        &[
            "packet_id",
            "schema_version",
            "repo_root",
            "head_commit",
            "request_id",
            "issued_at",
            "outcome",
            "freshness",
            "authorization",
            "budget",
            "evidence",
            "degradations",
            "provenance",
            "packet_hash",
        ],
    )?;
    let freshness = match map.get("freshness") {
        Some(v) => parse_freshness(v)?,
        None => Freshness::default(),
    };
    let authorization = match map.get("authorization") {
        Some(v) => parse_authorization(v)?,
        None => Authorization::default(),
    };
    let budget = match map.get("budget") {
        Some(v) => parse_budget(v)?,
        None => Budget::default(),
    };
    let evidence = match map.get("evidence") {
        Some(v) => parse_evidence(v)?,
        None => None,
    };
    let degradations = strings(&map, "degradations")?;
    let provenance = match map.get("provenance") {
        Some(v) => parse_provenance(v)?,
        None => Provenance::default(),
    };
    Ok(Packet {
        packet_id: string(&map, "packet_id")?,
        schema_version: string(&map, "schema_version")?,
        repo_root: string(&map, "repo_root")?,
        head_commit: string(&map, "head_commit")?,
        request_id: string(&map, "request_id")?,
        issued_at: string(&map, "issued_at")?,
        outcome: string(&map, "outcome")?,
        freshness,
        authorization,
        budget,
        evidence,
        degradations,
        provenance,
        packet_hash: string(&map, "packet_hash")?,
    })
}
fn parse_freshness(v: &Value) -> Result<Freshness, String> {
    let m = fields(
        v,
        &[
            "head_commit",
            "head_anchor",
            "status",
            "current",
            "is_current",
            "checked_at",
        ],
    )?;
    Ok(Freshness {
        head_commit: string(&m, "head_commit")?,
        head_anchor: string(&m, "head_anchor")?,
        status: string(&m, "status")?,
        current: boolean(&m, "current")?,
        is_current: boolean(&m, "is_current")?,
        checked_at: string(&m, "checked_at")?,
    })
}
fn parse_authorization(v: &Value) -> Result<Authorization, String> {
    let m = fields(v, &["level", "reason"])?;
    Ok(Authorization {
        level: string(&m, "level")?,
        reason: string(&m, "reason")?,
    })
}
fn parse_budget(v: &Value) -> Result<Budget, String> {
    let m = fields(
        v,
        &["max_evidence", "used_evidence", "max_bytes", "used_bytes"],
    )?;
    Ok(Budget {
        max_evidence: integer(&m, "max_evidence")?,
        used_evidence: integer(&m, "used_evidence")?,
        max_bytes: integer(&m, "max_bytes")?,
        used_bytes: integer(&m, "used_bytes")?,
    })
}
fn parse_evidence(v: &Value) -> Result<Option<Vec<Evidence>>, String> {
    let items = match v {
        Value::Array(v) => v,
        _ => return Err("evidence. expected array".into()),
    };
    let mut out = Vec::new();
    for item in items {
        let m = fields(
            item,
            &[
                "evidence_id",
                "kind",
                "path",
                "commit",
                "line_start",
                "line_end",
                "source",
                "content_hash",
                "collected_at",
                "verifier_status",
            ],
        )?;
        out.push(Evidence {
            evidence_id: string(&m, "evidence_id")?,
            kind: string(&m, "kind")?,
            path: string(&m, "path")?,
            commit: string(&m, "commit")?,
            line_start: integer(&m, "line_start")?,
            line_end: integer(&m, "line_end")?,
            source: string(&m, "source")?,
            content_hash: string(&m, "content_hash")?,
            collected_at: string(&m, "collected_at")?,
            verifier_status: string(&m, "verifier_status")?,
        });
    }
    Ok(Some(out))
}
fn parse_provenance(v: &Value) -> Result<Provenance, String> {
    let m = fields(v, &["collector", "tool", "version", "tool_version"])?;
    Ok(Provenance {
        collector: string(&m, "collector")?,
        tool: string(&m, "tool")?,
        version: string(&m, "version")?,
        tool_version: string(&m, "tool_version")?,
    })
}
