use crate::{
    packet::{Evidence, Packet},
    sha256,
};
use std::fs;
use std::path::{Path, PathBuf};

pub struct Verification {
    pub evidence_id: String,
    pub path: String,
    pub status: String,
    pub actual_hash: String,
    pub message: String,
}
pub struct Report {
    pub results: Vec<Verification>,
    pub verified_count: usize,
    pub failed_count: usize,
    pub skipped_count: usize,
    pub ok: bool,
}

pub fn verify(repo: &str, p: &Packet) -> Result<Report, String> {
    let root = match fs::canonicalize(repo) {
        Ok(v) => v,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            let abs = std::env::current_dir().unwrap_or_default().join(repo);
            return Err(format!(
                "resolve repository root {:?}: lstat {}: no such file or directory",
                repo,
                abs.display()
            ));
        }
        Err(e) => return Err(format!("resolve repository root {:?}: {}", repo, e)),
    };
    if !root.is_dir() {
        return Err(format!("repository {:?} is not a directory", repo));
    }
    let mut r = Report {
        results: Vec::new(),
        verified_count: 0,
        failed_count: 0,
        skipped_count: 0,
        ok: true,
    };
    for e in p.evidence.as_ref().into_iter().flatten() {
        let x = one(&root, e);
        match x.status.as_str() {
            "verified" => r.verified_count += 1,
            "failed" => r.failed_count += 1,
            _ => r.skipped_count += 1,
        };
        r.results.push(x);
    }
    r.ok = r.failed_count == 0 && r.skipped_count == 0;
    Ok(r)
}
fn one(root: &Path, e: &Evidence) -> Verification {
    let mut r = Verification {
        evidence_id: e.evidence_id.clone(),
        path: e.path.clone(),
        status: "failed".into(),
        actual_hash: String::new(),
        message: String::new(),
    };
    let path = match safe_path(root, &e.path) {
        Ok(v) => v,
        Err(x) => {
            r.message = x;
            return r;
        }
    };
    let meta = match fs::symlink_metadata(&path) {
        Ok(v) => v,
        Err(x) => {
            r.message = format!("cannot read evidence file {:?}: {}", path, x);
            return r;
        }
    };
    if !meta.is_file() {
        r.message = format!("evidence path {:?} is not a regular file", path);
        return r;
    }
    let data = match fs::read(&path) {
        Ok(v) => v,
        Err(x) => {
            r.message = format!("cannot read evidence file {:?}: {}", path, x);
            return r;
        }
    };
    let line_count = count_lines(&data);
    if e.line_start < 1 || e.line_end < e.line_start || e.line_end as usize > line_count {
        r.message = format!(
            "invalid line range {}-{} for {} line(s)",
            e.line_start,
            e.line_end,
            line_count
        );
        return r;
    }
    let expected = if let Some(v) = e.content_hash.strip_prefix("sha256:") {
        v
    } else {
        &e.content_hash
    };
    if expected.len() != 64 || !expected.bytes().all(|b| b.is_ascii_hexdigit()) {
        r.message = format!("invalid SHA-256 content hash {:?}", e.content_hash);
        return r;
    }
    let mut selected: Vec<u8> = Vec::new();
    append_selected_lines(&data, e.line_start as usize, e.line_end as usize, &mut selected);
    r.actual_hash = sha256::hex(&sha256::digest(&selected));
    if r.actual_hash != expected.to_ascii_lowercase() {
        r.message = format!(
            "content hash mismatch: expected {}, got {}",
            e.content_hash, r.actual_hash
        );
        return r;
    }
    r.status = "verified".into();
    r
}

// Line counting and range hashing mirror the reference: a "\r\n" pair is one
// terminator, a final terminator adds no trailing line, hashing joins the
// selected lines with "\n" and appends a final "\n" for non-empty content,
// and all bytes are raw — the file is never decoded as text.
fn count_lines(data: &[u8]) -> usize {
    if data.is_empty() {
        return 0;
    }
    let mut count = data.iter().filter(|&&b| b == b'\n').count();
    if *data.last().unwrap() != b'\n' {
        count += 1;
    }
    count
}

fn append_selected_lines(data: &[u8], line_start: usize, line_end: usize, out: &mut Vec<u8>) {
    let mut first_line_non_empty = false;
    let mut line = 1usize;
    let mut start = 0usize;
    while line <= line_end && start < data.len() {
        let nl = data[start..].iter().position(|&b| b == b'\n');
        let mut content_end = match nl {
            Some(k) => start + k,
            None => data.len(),
        };
        let terminated = nl.is_some();
        if terminated && content_end > start && data[content_end - 1] == b'\r' {
            content_end -= 1;
        }
        if line >= line_start {
            if line > line_start {
                out.push(b'\n');
            }
            out.extend_from_slice(&data[start..content_end]);
            if line == line_start {
                first_line_non_empty = content_end > start;
            }
        }
        match nl {
            Some(k) => {
                start += k + 1;
                line += 1;
            }
            None => break,
        }
    }
    if line_end > line_start || first_line_non_empty {
        out.push(b'\n');
    }
}
fn safe_path(root: &Path, raw: &str) -> Result<PathBuf, String> {
    let p = Path::new(raw);
    if raw.is_empty() {
        return Err("evidence path is empty".into());
    }
    if p.is_absolute() {
        return Err(format!(
            "evidence path {:?} must be repository-relative",
            raw
        ));
    }
    for c in p.components() {
        if matches!(c, std::path::Component::ParentDir) {
            return Err(format!(
                "evidence path {:?} contains a parent component",
                raw
            ));
        }
    }
    let full = root.join(p);
    let rel = full
        .strip_prefix(root)
        .map_err(|_| format!("evidence path {:?} is outside the repository", raw))?;
    let mut cur = root.to_path_buf();
    for c in rel.components() {
        if let std::path::Component::Normal(n) = c {
            cur.push(n);
            let m = match fs::symlink_metadata(&cur) {
                Ok(v) => v,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                    return Err(format!(
                        "cannot access evidence path {:?}: lstat {}: no such file or directory",
                        raw,
                        cur.display()
                    ))
                }
                Err(e) => return Err(format!("cannot access evidence path {:?}: {}", raw, e)),
            };
            if m.file_type().is_symlink() {
                return Err(format!("evidence path {:?} contains a symlink", raw));
            }
        }
    }
    Ok(full)
}
