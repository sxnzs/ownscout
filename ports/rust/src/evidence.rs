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
    let text = String::from_utf8_lossy(&data).replace("\r\n", "\n");
    let mut lines: Vec<&str> = text.split('\n').collect();
    if lines.last() == Some(&"") {
        lines.pop();
    }
    if e.line_start < 1 || e.line_end < e.line_start || e.line_end as usize > lines.len() {
        r.message = format!(
            "invalid line range {}-{} for {} line(s)",
            e.line_start,
            e.line_end,
            lines.len()
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
    let selected = format!(
        "{}\n",
        lines[(e.line_start - 1) as usize..e.line_end as usize].join("\n")
    );
    r.actual_hash = sha256::hex(&sha256::digest(selected.as_bytes()));
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
