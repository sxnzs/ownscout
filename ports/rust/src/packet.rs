use crate::json::{object, object_map, Value};

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
    let map = object_map(fields);
    for key in map.keys() {
        if !allowed.contains(key) {
            return Err(format!("unknown field {:?}", key));
        }
    }
    Ok(map)
}
fn string(map: &std::collections::BTreeMap<&str, &Value>, key: &str) -> Result<String, String> {
    string_at(map, key, &format!("Packet.{}", key))
}
fn string_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<String, String> {
    match map.get(key) {
        None => Ok(String::new()),
        Some(Value::String(v)) => Ok(v.clone()),
        Some(Value::Null) => Ok(String::new()),
        Some(v) => Err(type_error(path, "string", v)),
    }
}
fn integer_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<i64, String> {
    let expected = if path.ends_with("max_bytes") || path.ends_with("used_bytes") {
        "int64"
    } else {
        "int"
    };
    match map.get(key) {
        None => Ok(0),
        Some(Value::Number(v)) => v
            .parse()
            .map_err(|_| type_error(path, expected, &Value::Number(v.clone()))),
        Some(Value::Null) => Ok(0),
        Some(v) => Err(type_error(path, expected, v)),
    }
}
fn boolean_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<bool, String> {
    match map.get(key) {
        None => Ok(false),
        Some(Value::Bool(v)) => Ok(*v),
        Some(Value::Null) => Ok(false),
        Some(v) => Err(type_error(path, "bool", v)),
    }
}
fn strings(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
) -> Result<Option<Vec<String>>, String> {
    strings_at(map, key, &format!("Packet.{}", key))
}
fn strings_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<Option<Vec<String>>, String> {
    match map.get(key) {
        None => Ok(None),
        Some(Value::Array(items)) => items
            .iter()
            .map(|v| match v {
                Value::String(s) => Ok(s.clone()),
                v => Err(type_error(path, "string", v)),
            })
            .collect::<Result<Vec<_>, _>>()
            .map(Some),
        Some(Value::Null) => Ok(None),
        Some(v) => Err(type_error(path, "[]string", v)),
    }
}
fn type_error(path: &str, expected: &str, value: &Value) -> String {
    format!(
        "type mismatch|{}|{}|{}",
        path,
        expected,
        match value {
            Value::Null => "null",
            Value::Bool(_) => "bool",
            Value::Number(_) => "number",
            Value::String(_) => "string",
            Value::Array(_) => "array",
            Value::Object(_) => "object",
        }
    )
}
fn nested_fields<'a>(
    value: &'a Value,
    allowed: &[&str],
    path: &str,
    expected: &str,
) -> Result<std::collections::BTreeMap<&'a str, &'a Value>, String> {
    match value {
        Value::Null => Ok(std::collections::BTreeMap::new()),
        Value::Object(_) => fields(value, allowed),
        _ => Err(type_error(path, expected, value)),
    }
}
pub fn decode(data: &[u8]) -> Result<Packet, String> {
    if data.len() > 1 << 20 {
        return Err("nodepacket: packet: input exceeds 1 MiB".into());
    }
    let value = crate::json::parse(data).map_err(|e| format!("nodepacket: packet: {}", e))?;
    let raw_fields = object(&value)?;
    let map = object_map(raw_fields);
    for (key, value) in raw_fields {
        match key.as_str() {
            "packet_id" | "schema_version" | "repo_root" | "head_commit" | "request_id"
            | "issued_at" | "outcome" | "packet_hash" => {
                string_at(&map, key, &format!("Packet.{}", key))?;
            }
            "freshness" => {
                parse_freshness(value)?;
            }
            "authorization" => {
                parse_authorization(value)?;
            }
            "budget" => {
                parse_budget(value)?;
            }
            "evidence" => {
                parse_evidence(value)?;
            }
            "degradations" => {
                strings_at(&map, key, "Packet.degradations")?;
            }
            "provenance" => {
                parse_provenance(value)?;
            }
            _ => return Err(format!("unknown field {:?}", key)),
        }
    }
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
    let m = nested_fields(
        v,
        &[
            "head_commit",
            "head_anchor",
            "status",
            "current",
            "is_current",
            "checked_at",
        ],
        "Packet.freshness",
        "contract.Freshness",
    )?;
    Ok(Freshness {
        head_commit: string_at(&m, "head_commit", "Packet.freshness.head_commit")?,
        head_anchor: string_at(&m, "head_anchor", "Packet.freshness.head_anchor")?,
        status: string_at(&m, "status", "Packet.freshness.status")?,
        current: boolean_at(&m, "current", "Packet.freshness.current")?,
        is_current: boolean_at(&m, "is_current", "Packet.freshness.is_current")?,
        checked_at: string_at(&m, "checked_at", "Packet.freshness.checked_at")?,
    })
}
fn parse_authorization(v: &Value) -> Result<Authorization, String> {
    let m = nested_fields(
        v,
        &["level", "reason"],
        "Packet.authorization",
        "contract.Authorization",
    )?;
    Ok(Authorization {
        level: string_at(&m, "level", "Packet.authorization.level")?,
        reason: string_at(&m, "reason", "Packet.authorization.reason")?,
    })
}
fn parse_budget(v: &Value) -> Result<Budget, String> {
    let m = nested_fields(
        v,
        &["max_evidence", "used_evidence", "max_bytes", "used_bytes"],
        "Packet.budget",
        "contract.Budget",
    )?;
    Ok(Budget {
        max_evidence: integer_at(&m, "max_evidence", "Packet.budget.max_evidence")?,
        used_evidence: integer_at(&m, "used_evidence", "Packet.budget.used_evidence")?,
        max_bytes: integer_at(&m, "max_bytes", "Packet.budget.max_bytes")?,
        used_bytes: integer_at(&m, "used_bytes", "Packet.budget.used_bytes")?,
    })
}
fn parse_evidence(v: &Value) -> Result<Option<Vec<Evidence>>, String> {
    let items = match v {
        Value::Array(v) => v,
        Value::Null => return Ok(None),
        _ => return Err(type_error("Packet.evidence", "[]contract.Evidence", v)),
    };
    let mut out = Vec::new();
    for (index, item) in items.iter().enumerate() {
        let path = format!("Packet.evidence.{}", index);
        let m = match item {
            Value::Null => std::collections::BTreeMap::new(),
            Value::Object(fields) => {
                let m = object_map(fields);
                for (key, value) in fields {
                    let field_path = format!("{}.{}", path, key);
                    match key.as_str() {
                        "evidence_id" | "kind" | "path" | "commit" | "source" | "content_hash"
                        | "collected_at" | "verifier_status" => {
                            if !matches!(value, Value::String(_) | Value::Null) {
                                return Err(type_error(&field_path, "string", value));
                            }
                        }
                        "line_start" | "line_end" => {
                            if !matches!(value, Value::Number(_) | Value::Null) {
                                return Err(type_error(&field_path, "int", value));
                            }
                        }
                        _ => return Err(format!("unknown field {:?}", key)),
                    }
                }
                m
            }
            _ => return Err(type_error(&path, "contract.Evidence", item)),
        };
        out.push(Evidence {
            evidence_id: string_at(&m, "evidence_id", &format!("{}.evidence_id", path))?,
            kind: string_at(&m, "kind", &format!("{}.kind", path))?,
            path: string_at(&m, "path", &format!("{}.path", path))?,
            commit: string_at(&m, "commit", &format!("{}.commit", path))?,
            line_start: integer_at(&m, "line_start", &format!("{}.line_start", path))?,
            line_end: integer_at(&m, "line_end", &format!("{}.line_end", path))?,
            source: string_at(&m, "source", &format!("{}.source", path))?,
            content_hash: string_at(&m, "content_hash", &format!("{}.content_hash", path))?,
            collected_at: string_at(&m, "collected_at", &format!("{}.collected_at", path))?,
            verifier_status: string_at(
                &m,
                "verifier_status",
                &format!("{}.verifier_status", path),
            )?,
        });
    }
    Ok(Some(out))
}
fn parse_provenance(v: &Value) -> Result<Provenance, String> {
    let m = nested_fields(
        v,
        &["collector", "tool", "version", "tool_version"],
        "Packet.provenance",
        "contract.Provenance",
    )?;
    Ok(Provenance {
        collector: string_at(&m, "collector", "Packet.provenance.collector")?,
        tool: string_at(&m, "tool", "Packet.provenance.tool")?,
        version: string_at(&m, "version", "Packet.provenance.version")?,
        tool_version: string_at(&m, "tool_version", "Packet.provenance.tool_version")?,
    })
}
