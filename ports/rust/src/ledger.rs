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

// ---------------------------------------------------------------------------
// Read-only ledger verification (mirrors internal/ledger/ledger.go Verify).
// ---------------------------------------------------------------------------

const MAX_LEDGER_SIZE: usize = 1 << 20;
const MAX_RECORD_SIZE: usize = 64 << 10;

pub struct Summary {
    pub records: usize,
    pub tip: String,
}

pub enum VerifyError {
    Read(String),
    Validation(String),
}

pub fn verify(path: &str) -> Result<Summary, VerifyError> {
    let ledger_path = abs_clean(path);
    let display = ledger_path.display().to_string();
    let quoted = crate::packet::go_quote(&display);
    let info = match fs::symlink_metadata(&ledger_path) {
        Ok(info) => info,
        Err(e) => {
            return Err(VerifyError::Read(format!(
                "read ledger {}: lstat {}: {}",
                quoted,
                display,
                go_errno(&e)
            )))
        }
    };
    if !info.is_file() {
        return Err(VerifyError::Read(format!(
            "read ledger {}: not a regular file",
            quoted
        )));
    }
    let data = match fs::read(&ledger_path) {
        Ok(data) => data,
        Err(e) => {
            return Err(VerifyError::Read(format!(
                "read ledger {}: open {}: {}",
                quoted,
                display,
                go_errno(&e)
            )))
        }
    };
    if data.len() > MAX_LEDGER_SIZE {
        return Err(VerifyError::Read(format!(
            "read ledger {}: ledger exceeds {} bytes",
            quoted, MAX_LEDGER_SIZE
        )));
    }
    validate_ledger(&data)
        .map_err(|e| VerifyError::Validation(format!("validate ledger {}: {}", quoted, e)))
}

fn abs_clean(path: &str) -> PathBuf {
    let p = Path::new(path);
    let joined = if p.is_absolute() {
        p.to_path_buf()
    } else {
        std::env::current_dir()
            .unwrap_or_default()
            .join(p)
    };
    let mut out = PathBuf::new();
    for c in joined.components() {
        match c {
            std::path::Component::CurDir => {}
            std::path::Component::ParentDir => {
                if out.file_name().is_some() {
                    out.pop();
                } else {
                    out.push("..");
                }
            }
            other => out.push(other.as_os_str()),
        }
    }
    if out.as_os_str().is_empty() {
        out.push(".");
    }
    out
}

// go_errno renders a std::io::Error the way Go's PathError renders the errno.
fn go_errno(e: &std::io::Error) -> String {
    let text = e.to_string();
    let base = text.split(" (os error").next().unwrap_or(&text);
    let mut chars = base.chars();
    match chars.next() {
        Some(first) => first.to_lowercase().chain(chars).collect(),
        None => base.to_string(),
    }
}

fn validate_ledger(data: &[u8]) -> Result<Summary, String> {
    if !data.is_empty() && !data.ends_with(b"\n") {
        return Err("nonempty ledger must end with LF".into());
    }
    let mut prev = ZERO.to_string();
    let mut count = 0usize;
    for raw in scan_lines(data)? {
        let line = trim_space(raw);
        if line.is_empty() {
            return Err("empty or blank line".into());
        }
        let record = decode_record(line).map_err(|e| format!("line {}: {}", count + 1, e))?;
        let expected_seq = (count + 1) as u64;
        validate_record(&record, expected_seq, &prev)
            .map_err(|e| format!("line {}: {}", count + 1, e))?;
        let expected_hash = record_hash(&record);
        if record.hash != expected_hash {
            return Err(format!(
                "line {}: record_hash does not match canonical record",
                count + 1
            ));
        }
        prev = record.hash.clone();
        count += 1;
    }
    Ok(Summary {
        records: count,
        tip: if count == 0 { String::new() } else { prev },
    })
}

fn scan_lines(data: &[u8]) -> Result<Vec<&[u8]>, String> {
    let mut out = Vec::new();
    let mut start = 0usize;
    for index in 0..data.len() {
        if data[index] == b'\n' {
            let raw = &data[start..index];
            if raw.len() > MAX_RECORD_SIZE + 1 {
                return Err("read line: bufio.Scanner: token too long".into());
            }
            out.push(raw);
            start = index + 1;
        }
    }
    if start < data.len() {
        let raw = &data[start..];
        if raw.len() > MAX_RECORD_SIZE + 1 {
            return Err("read line: bufio.Scanner: token too long".into());
        }
        out.push(raw);
    }
    Ok(out)
}

fn trim_space(data: &[u8]) -> &[u8] {
    let is_space = |b: u8| matches!(b, b' ' | b'\t' | b'\n' | b'\r' | 0x0b | 0x0c);
    let mut start = 0usize;
    let mut end = data.len();
    while start < end && is_space(data[start]) {
        start += 1;
    }
    while end > start && is_space(data[end - 1]) {
        end -= 1;
    }
    &data[start..end]
}

fn validate_record(record: &Record, expected_seq: u64, expected_prev: &str) -> Result<(), String> {
    if record.schema != "ownscout-ledger-v1" {
        return Err("schema_version must be \"ownscout-ledger-v1\"".into());
    }
    if record.seq != expected_seq {
        return Err(format!(
            "seq {} does not follow expected sequence {}",
            record.seq, expected_seq
        ));
    }
    if record.prev != expected_prev {
        return Err("prev_record_hash does not match hash chain".into());
    }
    for (field, value) in [
        ("prev_record_hash", &record.prev),
        ("record_hash", &record.hash),
        ("envelope_sha256", &record.envelope),
        ("packet_binding_sha256", &record.binding),
    ] {
        if !sha256_lower_hex(value) {
            return Err(format!(
                "{} must be exactly 64 lowercase hexadecimal characters",
                field
            ));
        }
    }
    if record.version.is_empty() {
        return Err("ownscout_version must be non-empty".into());
    }
    if record.results.is_empty() {
        return Err("node_results must be non-empty".into());
    }
    if record.results.len() > 4096 {
        return Err("node_results exceeds 4096 results".into());
    }
    if [
        &record.schema,
        &record.prev,
        &record.hash,
        &record.envelope,
        &record.binding,
        &record.version,
    ]
    .iter()
    .any(|s| s.as_bytes().contains(&0))
    {
        return Err("record contains a NUL byte".into());
    }
    let mut seen: Vec<&str> = Vec::new();
    for (index, result) in record.results.iter().enumerate() {
        validate_node_result(result).map_err(|e| format!("node_results[{}]: {}", index, e))?;
        if seen.contains(&result.node_id.as_str()) {
            return Err(format!(
                "duplicate node_id {}",
                crate::packet::go_quote(&result.node_id)
            ));
        }
        seen.push(result.node_id.as_str());
    }
    Ok(())
}

fn validate_node_result(result: &NodeResult) -> Result<(), String> {
    validate_ledger_identifier(&result.node_id).map_err(|e| format!("node_id: {}", e))?;
    match result.status.as_str() {
        "evidence_current" | "failed" | "blocked" => {}
        other => return Err(format!("status {} is invalid", crate::packet::go_quote(other))),
    }
    if [&result.node_id, &result.status, &result.reason]
        .iter()
        .any(|s| s.as_bytes().contains(&0))
    {
        return Err("contains a NUL byte".into());
    }
    Ok(())
}

fn validate_ledger_identifier(value: &str) -> Result<(), String> {
    if value.is_empty() || value.len() > 128 {
        return Err("must contain 1-128 ASCII identifier characters".into());
    }
    for (i, c) in value.bytes().enumerate() {
        let alphanumeric = c.is_ascii_alphanumeric();
        if i == 0 {
            if !alphanumeric {
                return Err("must start with an ASCII alphanumeric character".into());
            }
            continue;
        }
        if !alphanumeric && !matches!(c, b'.' | b'_' | b'-' | b':') {
            return Err("contains an invalid character".into());
        }
    }
    Ok(())
}

fn sha256_lower_hex(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

const RECORD_FIELDS: &[&str] = &[
    "schema_version",
    "seq",
    "prev_record_hash",
    "record_hash",
    "envelope_sha256",
    "packet_binding_sha256",
    "ownscout_version",
    "node_results",
];
const NODE_RESULT_FIELDS: &[&str] = &["node_id", "status", "reason"];

fn decode_record(data: &[u8]) -> Result<Record, String> {
    go_validate_object(data)?;
    let value = crate::json::parse(data).map_err(|e| format!("malformed JSON: {}", e))?;
    let Value::Object(fields) = &value else {
        return Err("record must be a JSON object".into());
    };
    build_record(fields)
}

// go_validate_object reproduces the reference decode preflight: decoding the
// document with the record's field set, then rejecting trailing data. Go 1.27
// ships the encoding/json v2 implementation, whose token order and error text
// differ from older releases, so this walks the document token by token the way
// json.Decoder does.
fn go_validate_object(data: &[u8]) -> Result<(), String> {
    let mut dec = GoDec { data, pos: 0 };
    match dec.token() {
        Ok(GoTok::Delim(b'{')) => {}
        Ok(_) => return Err("record must be a JSON object".into()),
        Err(e) => return Err(format!("malformed JSON: {}", e)),
    }
    dec.consume_object(Some(RECORD_FIELDS))
        .map_err(|e| format!("malformed JSON: {}", e))?;
    dec.skip_ws();
    if dec.pos < dec.data.len() {
        return match dec.token() {
            Ok(_) => Err("trailing data".into()),
            Err(e) => Err(format!("trailing data: {}", e)),
        };
    }
    Ok(())
}

enum GoTok {
    Delim(u8),
    Str(String),
    Other,
}

struct GoDec<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> GoDec<'a> {
    fn skip_ws(&mut self) {
        while self.pos < self.data.len() && is_json_space(self.data[self.pos]) {
            self.pos += 1;
        }
    }
    fn peek(&self) -> Option<u8> {
        self.data.get(self.pos).copied()
    }
    fn err_at(&self, pos: usize, context: &str) -> String {
        format!("invalid character {} {}", quote_at(self.data, pos), context)
    }

    // token reads one value token, the way Decoder.Token does in a value slot.
    fn token(&mut self) -> Result<GoTok, String> {
        self.skip_ws();
        let c = match self.peek() {
            Some(c) => c,
            None => return Err("EOF".into()),
        };
        match c {
            b'{' => {
                self.pos += 1;
                Ok(GoTok::Delim(b'{'))
            }
            b'[' => {
                self.pos += 1;
                Ok(GoTok::Delim(b'['))
            }
            b'"' => Ok(GoTok::Str(self.string()?)),
            b't' => {
                self.literal(b"true")?;
                Ok(GoTok::Other)
            }
            b'f' => {
                self.literal(b"false")?;
                Ok(GoTok::Other)
            }
            b'n' => {
                self.literal(b"null")?;
                Ok(GoTok::Other)
            }
            b'-' | b'0'..=b'9' => {
                self.number()?;
                Ok(GoTok::Other)
            }
            _ => Err(self.err_at(self.pos, "looking for beginning of value")),
        }
    }

    fn literal(&mut self, word: &[u8]) -> Result<(), String> {
        for (index, expected) in word.iter().enumerate() {
            let c = match self.peek() {
                Some(c) => c,
                None => return Err("unexpected EOF".into()),
            };
            if c != *expected {
                return Err(self.err_at(self.pos, literal_context(word, index)));
            }
            self.pos += 1;
        }
        Ok(())
    }

    fn number(&mut self) -> Result<(), String> {
        if self.peek() == Some(b'-') {
            self.pos += 1;
        }
        match self.peek() {
            None => return Err("unexpected EOF".into()),
            Some(b'0') => self.pos += 1,
            Some(b'1'..=b'9') => self.digits(),
            Some(_) => return Err(self.err_at(self.pos, "in numeric literal")),
        }
        if self.peek() == Some(b'.') {
            self.pos += 1;
            match self.peek() {
                None => return Err("unexpected EOF".into()),
                Some(b'0'..=b'9') => self.digits(),
                Some(_) => return Err(self.err_at(self.pos, "in numeric literal")),
            }
        }
        if matches!(self.peek(), Some(b'e' | b'E')) {
            self.pos += 1;
            if matches!(self.peek(), Some(b'+' | b'-')) {
                self.pos += 1;
            }
            match self.peek() {
                None => return Err("unexpected EOF".into()),
                Some(b'0'..=b'9') => self.digits(),
                Some(_) => return Err(self.err_at(self.pos, "in numeric literal")),
            }
        }
        Ok(())
    }

    fn digits(&mut self) {
        while matches!(self.peek(), Some(b'0'..=b'9')) {
            self.pos += 1;
        }
    }

    fn string(&mut self) -> Result<String, String> {
        self.pos += 1;
        let mut out = String::new();
        loop {
            let c = match self.peek() {
                Some(c) => c,
                None => return Err("unexpected EOF".into()),
            };
            self.pos += 1;
            match c {
                b'"' => return Ok(out),
                b'\\' => {
                    let escape = self.pos - 1;
                    let e = match self.peek() {
                        Some(e) => e,
                        None => return Err("unexpected EOF".into()),
                    };
                    self.pos += 1;
                    match e {
                        b'"' => out.push('"'),
                        b'\\' => out.push('\\'),
                        b'/' => out.push('/'),
                        b'b' => out.push('\u{8}'),
                        b'f' => out.push('\u{c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let mut digits = [0u8; 4];
                            let mut seen = 0usize;
                            for slot in digits.iter_mut() {
                                match self.peek() {
                                    Some(d) => {
                                        *slot = d;
                                        seen += 1;
                                        self.pos += 1;
                                    }
                                    None => break,
                                }
                            }
                            if seen < 4 || !digits.iter().all(|d| d.is_ascii_hexdigit()) {
                                return Err(format!(
                                    "invalid escape sequence `{}` in string",
                                    String::from_utf8_lossy(&self.data[escape..self.pos])
                                ));
                            }
                            let value =
                                digits.iter().fold(0u32, |acc, d| acc * 16 + hex_digit(*d));
                            out.push(char::from_u32(value).unwrap_or('\u{FFFD}'));
                        }
                        _ => {
                            return Err(format!(
                                "invalid escape sequence `{}` in string",
                                String::from_utf8_lossy(&self.data[escape..self.pos])
                            ))
                        }
                    }
                }
                c if c < 0x20 => return Err(self.err_at(self.pos - 1, "in string")),
                _ => {
                    let start = self.pos - 1;
                    while self.pos < self.data.len() && self.data[self.pos] >= 0x80 {
                        self.pos += 1;
                    }
                    out.push_str(&String::from_utf8_lossy(&self.data[start..self.pos]));
                }
            }
        }
    }

    // after_comma applies the reference's comma rule: a closing delimiter right
    // after a comma is reported against the comma itself.
    fn after_comma(&mut self) -> Result<(), String> {
        let comma = self.pos;
        self.pos += 1;
        self.skip_ws();
        match self.peek() {
            Some(b']') | Some(b'}') => Err(self.err_at(comma, "looking for beginning of value")),
            _ => Ok(()),
        }
    }

    fn consume_value(&mut self) -> Result<(), String> {
        match self.token()? {
            GoTok::Delim(b'{') => self.consume_object(None),
            GoTok::Delim(b'[') => self.consume_array(),
            _ => Ok(()),
        }
    }

    fn consume_array(&mut self) -> Result<(), String> {
        loop {
            self.skip_ws();
            match self.peek() {
                Some(b']') => {
                    self.pos += 1;
                    return Ok(());
                }
                None => return Err("unexpected end of JSON input".into()),
                _ => {}
            }
            self.consume_value()?;
            self.skip_ws();
            match self.peek() {
                None => return Err("unexpected end of JSON input".into()),
                Some(b',') => self.after_comma()?,
                Some(b']') => {
                    self.pos += 1;
                    return Ok(());
                }
                Some(_) => return Err(self.err_at(self.pos, "after array element")),
            }
        }
    }

    fn consume_object(&mut self, allowed: Option<&[&str]>) -> Result<(), String> {
        let mut seen: Vec<String> = Vec::new();
        loop {
            self.skip_ws();
            match self.peek() {
                None => return Err("unexpected end of JSON input".into()),
                Some(b'}') => {
                    self.pos += 1;
                    return Ok(());
                }
                _ => {}
            }
            let key = match self.token()? {
                GoTok::Str(key) => key,
                _ => return Err("object member name must be a string".into()),
            };
            if seen.contains(&key) {
                return Err(format!(
                    "duplicate JSON field {}",
                    crate::packet::go_quote(&key)
                ));
            }
            seen.push(key.clone());
            if let Some(allowed) = allowed {
                if !allowed.contains(&key.as_str()) {
                    return Err(non_canonical_or_unknown(&key, allowed));
                }
            }
            self.skip_ws();
            match self.peek() {
                None => return Err("EOF".into()),
                Some(b':') => self.pos += 1,
                Some(_) => return Err(self.err_at(self.pos, "after object key")),
            }
            self.skip_ws();
            match self.peek() {
                None => return Err("EOF".into()),
                Some(b'}') => return Err("missing value after object key".into()),
                Some(b']') => return Err(self.err_at(self.pos, "after object key:value pair")),
                _ => {}
            }
            if allowed.is_some() && key == "node_results" {
                self.consume_node_results()?;
            } else {
                self.consume_value()?;
            }
            self.skip_ws();
            match self.peek() {
                None => return Err("unexpected end of JSON input".into()),
                Some(b',') => self.after_comma()?,
                Some(b'}') => {
                    self.pos += 1;
                    return Ok(());
                }
                Some(_) => return Err(self.err_at(self.pos, "after object key:value pair")),
            }
        }
    }

    fn consume_node_result(&mut self) -> Result<(), String> {
        match self.token()? {
            GoTok::Delim(b'{') => self.consume_object(Some(NODE_RESULT_FIELDS)),
            _ => Err("node result must be a JSON object".into()),
        }
    }

    fn consume_node_results(&mut self) -> Result<(), String> {
        match self.token()? {
            GoTok::Delim(b'[') => {}
            _ => return Err("node_results must be an array".into()),
        }
        loop {
            self.skip_ws();
            match self.peek() {
                Some(b']') => {
                    self.pos += 1;
                    return Ok(());
                }
                None => return Err("unexpected end of JSON input".into()),
                _ => {}
            }
            self.consume_node_result()?;
            self.skip_ws();
            match self.peek() {
                None => return Err("unexpected end of JSON input".into()),
                Some(b',') => self.after_comma()?,
                Some(b']') => {
                    self.pos += 1;
                    return Ok(());
                }
                Some(_) => return Err(self.err_at(self.pos, "after array element")),
            }
        }
    }
}

fn literal_context(word: &[u8], index: usize) -> &'static str {
    match (word, index) {
        (b"true", 1) => "in literal true (expecting 'r')",
        (b"true", 2) => "in literal true (expecting 'u')",
        (b"true", _) => "in literal true (expecting 'e')",
        (b"false", 1) => "in literal false (expecting 'a')",
        (b"false", 2) => "in literal false (expecting 'l')",
        (b"false", 3) => "in literal false (expecting 's')",
        (b"false", _) => "in literal false (expecting 'e')",
        (_, 1) => "in literal null (expecting 'u')",
        (_, _) => "in literal null (expecting 'l')",
    }
}

fn hex_digit(d: u8) -> u32 {
    match d {
        b'0'..=b'9' => (d - b'0') as u32,
        b'a'..=b'f' => (d - b'a' + 10) as u32,
        _ => (d - b'A' + 10) as u32,
    }
}

fn non_canonical_or_unknown(key: &str, allowed: &[&str]) -> String {
    let folded = crate::packet::fold_name(key);
    if let Some(canonical) = allowed
        .iter()
        .find(|candidate| crate::packet::fold_name(candidate) == folded)
    {
        return format!(
            "non-canonical JSON field {}; use {}",
            crate::packet::go_quote(key),
            crate::packet::go_quote(canonical)
        );
    }
    format!("unknown JSON field {}", crate::packet::go_quote(key))
}

fn value_kind(value: &Value) -> &'static str {
    match value {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

fn build_record(fields: &[(String, Value)]) -> Result<Record, String> {
    let field = |name: &str| fields.iter().find(|(key, _)| key == name).map(|(_, v)| v);
    let string = |name: &str, owner: &str| -> Result<String, String> {
        match field(name) {
            None => Ok(String::new()),
            Some(Value::String(s)) => Ok(s.clone()),
            Some(Value::Null) => Ok(String::new()),
            Some(v) => Err(format!(
                "malformed record: json: cannot unmarshal {} into Go struct field {}.{} of type string",
                value_kind(v),
                owner,
                name
            )),
        }
    };
    let seq = match field("seq") {
        None => 0,
        Some(Value::Null) => 0,
        Some(Value::Number(n)) => n.parse::<u64>().map_err(|_| {
            format!(
                "malformed record: json: cannot unmarshal number {} into Go struct field Record.seq of type uint64",
                n
            )
        })?,
        Some(v) => {
            return Err(format!(
                "malformed record: json: cannot unmarshal {} into Go struct field Record.seq of type uint64",
                value_kind(v)
            ))
        }
    };
    let results = match field("node_results") {
        None => Vec::new(),
        Some(Value::Null) => Vec::new(),
        Some(Value::Array(items)) => {
            let mut out = Vec::new();
            for (index, item) in items.iter().enumerate() {
                match item {
                    Value::Object(inner) => {
                        let inner_field =
                            |name: &str| inner.iter().find(|(key, _)| key == name).map(|(_, v)| v);
                        let node_string = |name: &str| -> Result<String, String> {
                            match inner_field(name) {
                                None => Ok(String::new()),
                                Some(Value::String(s)) => Ok(s.clone()),
                                Some(Value::Null) => Ok(String::new()),
                                Some(v) => Err(format!(
                                    "malformed record: json: cannot unmarshal {} into Go struct field Record.node_results.{}.{} of type string",
                                    value_kind(v),
                                    index,
                                    name
                                )),
                            }
                        };
                        out.push(NodeResult {
                            node_id: node_string("node_id")?,
                            status: node_string("status")?,
                            reason: node_string("reason")?,
                        });
                    }
                    Value::Null => out.push(NodeResult {
                        node_id: String::new(),
                        status: String::new(),
                        reason: String::new(),
                    }),
                    v => {
                        return Err(format!(
                            "malformed record: json: cannot unmarshal {} into Go value of type ledger.NodeResult",
                            value_kind(v)
                        ))
                    }
                }
            }
            out
        }
        Some(v) => {
            return Err(format!(
                "malformed record: json: cannot unmarshal {} into Go struct field Record.node_results of type []ledger.NodeResult",
                value_kind(v)
            ))
        }
    };
    Ok(Record {
        schema: string("schema_version", "Record")?,
        seq,
        prev: string("prev_record_hash", "Record")?,
        hash: string("record_hash", "Record")?,
        envelope: string("envelope_sha256", "Record")?,
        binding: string("packet_binding_sha256", "Record")?,
        version: string("ownscout_version", "Record")?,
        results,
    })
}

fn is_json_space(b: u8) -> bool {
    matches!(b, b' ' | b'\t' | b'\n' | b'\r')
}

// quote_at renders the character at an error position the way the reference
// does: a valid UTF-8 rune is quoted as a rune (strconv.QuoteRune), while an
// invalid byte is quoted as \\xNN.
fn quote_at(data: &[u8], pos: usize) -> String {
    let Some(&b) = data.get(pos) else {
        return "''".into();
    };
    if b < 0x80 {
        return quote_rune(b as char);
    }
    let valid = match std::str::from_utf8(&data[pos..]) {
        Ok(text) => text.len(),
        Err(error) => error.valid_up_to(),
    };
    if valid > 0 {
        if let Some(c) = std::str::from_utf8(&data[pos..pos + valid])
            .ok()
            .and_then(|text| text.chars().next())
        {
            return quote_rune(c);
        }
    }
    format!("'\\x{:02x}'", b)
}

fn quote_rune(c: char) -> String {
    let mut inner = String::new();
    match c {
        '\'' => inner.push_str("\\'"),
        '\\' => inner.push_str("\\\\"),
        c if crate::packet::go_is_print(c) => inner.push(c),
        '\u{07}' => inner.push_str("\\a"),
        '\u{08}' => inner.push_str("\\b"),
        '\u{0c}' => inner.push_str("\\f"),
        '\n' => inner.push_str("\\n"),
        '\r' => inner.push_str("\\r"),
        '\t' => inner.push_str("\\t"),
        '\u{0b}' => inner.push_str("\\v"),
        c if (c as u32) < 0x20 || c as u32 == 0x7f => {
            inner.push_str(&format!("\\x{:02x}", c as u32))
        }
        c if (c as u32) < 0x10000 => inner.push_str(&format!("\\u{:04x}", c as u32)),
        c => inner.push_str(&format!("\\U{:08x}", c as u32)),
    }
    format!("'{}'", inner)
}
