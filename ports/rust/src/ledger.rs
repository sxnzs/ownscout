use crate::{
    json::{object, unique_object, Value},
    sha256,
};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    path::{Path, PathBuf},
};

const ZERO: &str = "0000000000000000000000000000000000000000000000000000000000000000";
#[derive(Clone)]
pub struct NodeResult {
    pub node_id: String,
    pub status: String,
    pub reason: String,
}
pub struct Record {
    pub schema: String,
    pub seq: u64,
    pub prev: String,
    pub hash: String,
    pub envelope: String,
    pub binding: String,
    pub version: String,
    pub results: Vec<NodeResult>,
}
pub struct Store {
    file: File,
    next: u64,
    prev: String,
}
pub fn open(path: &str, repo: &str) -> Result<Store, String> {
    let repo =
        fs::canonicalize(repo).map_err(|e| format!("resolve repository path {:?}: {}", repo, e))?;
    let abs = fs::canonicalize(Path::new(path).parent().unwrap_or(Path::new(".")))
        .unwrap_or_else(|_| std::env::current_dir().unwrap_or_default());
    let full = abs.join(Path::new(path).file_name().unwrap_or_default());
    let full = full.canonicalize().unwrap_or(full);
    if inside(&repo, &full) {
        return Err(format!(
            "ledger path {:?} is inside repository {:?}",
            full.display(),
            repo.display()
        ));
    }
    reject_ancestors(&full)?;
    if let Ok(m) = fs::symlink_metadata(&full) {
        if m.file_type().is_symlink() {
            return Err(format!("ledger path {:?} is a symlink", full.display()));
        }
        if !m.is_file() {
            return Err(format!("ledger {:?} is not a regular file", full.display()));
        }
    }
    let mut f = OpenOptions::new()
        .read(true)
        .append(true)
        .create(true)
        .open(&full)
        .map_err(|e| format!("open ledger {:?}: {}", full.display(), e))?;
    let mut data = Vec::new();
    f.read_to_end(&mut data)
        .map_err(|e| format!("read ledger {:?}: {}", full.display(), e))?;
    if data.len() > 1 << 20 {
        return Err("ledger exceeds 1048576 bytes".into());
    }
    if !data.is_empty() && !data.ends_with(b"\n") {
        return Err("validate ledger: nonempty ledger must end with LF".into());
    }
    let mut prev = ZERO.to_string();
    let mut next = 1;
    for (line_no, line) in data
        .split(|c| *c == b'\n')
        .filter(|x| !x.is_empty())
        .enumerate()
    {
        let r = parse_record(line).map_err(|e| {
            format!(
                "validate ledger {:?}: line {}: {}",
                full.display(),
                line_no + 1,
                e
            )
        })?;
        if r.seq != next {
            return Err(format!(
                "validate ledger {:?}: line {}: seq {} does not follow expected sequence {}",
                full.display(),
                line_no + 1,
                r.seq,
                next
            ));
        }
        if r.prev != prev {
            return Err(format!(
                "validate ledger {:?}: line {}: prev_record_hash does not match hash chain",
                full.display(),
                line_no + 1
            ));
        }
        let h = record_hash(&r);
        if r.hash != h {
            return Err(format!(
                "validate ledger {:?}: line {}: record_hash does not match canonical record",
                full.display(),
                line_no + 1
            ));
        }
        prev = r.hash;
        next += 1;
    }
    Ok(Store {
        file: f,
        next,
        prev,
    })
}
impl Store {
    pub fn append(
        &mut self,
        envelope: &str,
        binding: &str,
        version: &str,
        results: Vec<NodeResult>,
    ) -> Result<Record, String> {
        if !hex64(envelope) || !hex64(binding) {
            return Err("invalid SHA-256".into());
        }
        if version.is_empty() {
            return Err("ownscout_version must be non-empty".into());
        }
        if results.is_empty() {
            return Err("node_results must be non-empty".into());
        }
        let r = Record {
            schema: "ownscout-ledger-v1".into(),
            seq: self.next,
            prev: self.prev.clone(),
            hash: ZERO.into(),
            envelope: envelope.into(),
            binding: binding.into(),
            version: version.into(),
            results,
        };
        let mut r = r;
        r.hash = record_hash(&r);
        let s = encode(&r, true);
        if s.len() + 1 > 65536 {
            return Err("record exceeds 65536 bytes".into());
        }
        self.file
            .write_all(s.as_bytes())
            .and_then(|_| self.file.write_all(b"\n"))
            .and_then(|_| self.file.sync_all())
            .map_err(|e| format!("append ledger record: {}", e))?;
        self.next += 1;
        self.prev = r.hash.clone();
        Ok(r)
    }
}
fn parse_record(data: &[u8]) -> Result<Record, String> {
    let v = crate::json::parse(data).map_err(|e| format!("malformed record: {}", e))?;
    let m = unique_object(object(&v)?)?;
    let allowed = [
        "schema_version",
        "seq",
        "prev_record_hash",
        "record_hash",
        "envelope_sha256",
        "packet_binding_sha256",
        "ownscout_version",
        "node_results",
    ];
    for k in m.keys() {
        if !allowed.contains(k) {
            return Err(format!("unknown JSON field {:?}", k));
        }
    }
    let s = |k: &str| -> Result<String, String> {
        match m.get(k) {
            Some(Value::String(x)) => Ok(x.clone()),
            _ => Err(format!("{} must be a string", k)),
        }
    };
    let n = match m.get("seq") {
        Some(Value::Number(x)) => x.parse().map_err(|_| "seq must be integer".to_string())?,
        _ => return Err("seq must be integer".into()),
    };
    let results = match m.get("node_results") {
        Some(Value::Array(xs)) => xs
            .iter()
            .map(|x| {
                let z = unique_object(object(x)?)?;
                let id = match z.get("node_id") {
                    Some(Value::String(x)) => x.clone(),
                    _ => return Err("node_id must be string".into()),
                };
                let st = match z.get("status") {
                    Some(Value::String(x)) => x.clone(),
                    _ => return Err("status must be string".into()),
                };
                let reason = match z.get("reason") {
                    Some(Value::String(x)) => x.clone(),
                    None => String::new(),
                    _ => return Err("reason must be string".into()),
                };
                Ok(NodeResult {
                    node_id: id,
                    status: st,
                    reason,
                })
            })
            .collect::<Result<Vec<_>, String>>()?,
        _ => return Err("node_results must be an array".into()),
    };
    Ok(Record {
        schema: s("schema_version")?,
        seq: n,
        prev: s("prev_record_hash")?,
        hash: s("record_hash")?,
        envelope: s("envelope_sha256")?,
        binding: s("packet_binding_sha256")?,
        version: s("ownscout_version")?,
        results,
    })
}
fn encode(r: &Record, include_hash: bool) -> String {
    let mut s=format!("{{\"schema_version\":{},\"seq\":{},\"prev_record_hash\":{},\"record_hash\":{},\"envelope_sha256\":{},\"packet_binding_sha256\":{},\"ownscout_version\":{},\"node_results\":[",q(&r.schema),r.seq,q(&r.prev),q(if include_hash{&r.hash}else{""}),q(&r.envelope),q(&r.binding),q(&r.version));
    for (i, n) in r.results.iter().enumerate() {
        if i > 0 {
            s.push(',')
        }
        s += &format!(
            "{{\"node_id\":{},\"status\":{}",
            q(&n.node_id),
            q(&n.status)
        );
        if !n.reason.is_empty() {
            s += &format!(",\"reason\":{}", q(&n.reason));
        }
        s.push('}')
    }
    s += "]}";
    s
}
fn q(s: &str) -> String {
    let mut o = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => o += "\\\"",
            '\\' => o += "\\\\",
            '\n' => o += "\\n",
            '\r' => o += "\\r",
            '\t' => o += "\\t",
            c if c.is_control() => o += &format!("\\u{:04x}", c as u32),
            c => o.push(c),
        }
    }
    o.push('"');
    o
}
fn record_hash(r: &Record) -> String {
    sha256::hex(&sha256::digest(encode(r, false).as_bytes()))
}
fn hex64(s: &str) -> bool {
    s.len() == 64
        && s.bytes()
            .all(|x| x.is_ascii_hexdigit() && !(b'A'..=b'F').contains(&x))
}
fn inside(root: &Path, p: &Path) -> bool {
    p.strip_prefix(root).is_ok()
}
fn reject_ancestors(p: &Path) -> Result<(), String> {
    let mut cur = PathBuf::new();
    for c in p.components() {
        cur.push(c);
        if let Ok(m) = fs::symlink_metadata(&cur) {
            if m.file_type().is_symlink() {
                return Err(format!(
                    "ledger path {:?} has symlink ancestor {:?}",
                    p.display(),
                    cur.display()
                ));
            }
        }
    }
    Ok(())
}
