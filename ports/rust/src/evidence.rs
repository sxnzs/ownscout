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

// Options selects optional verification behaviour. The zero value is the
// historical, always-on behaviour: a span is verified, or it is not.
pub struct Options {
    // Relocate re-resolves an evidence span whose content hash does not match
    // at the cited lines, and annotates the failure with where the same content
    // now lives. It is diagnostic only: relocation never changes a span's
    // status, the report counters, or the exit code.
    pub relocate: bool,
}

pub fn verify(repo: &str, p: &Packet) -> Result<Report, String> {
    verify_with_options(repo, p, &Options { relocate: false })
}

// verify_with_options is verify with optional behaviour enabled.
pub fn verify_with_options(repo: &str, p: &Packet, options: &Options) -> Result<Report, String> {
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
        let x = one(&root, e, options);
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
fn one(root: &Path, e: &Evidence, options: &Options) -> Verification {
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
        r.message += &location_clause(
            &data,
            line_count as i64,
            e.line_start,
            e.line_end,
            &e.content_hash,
            options,
        );
        return r;
    }
    let Some(expected) = normalized_expected_hash(&e.content_hash) else {
        r.message = format!("invalid SHA-256 content hash {:?}", e.content_hash);
        return r;
    };
    let mut selected: Vec<u8> = Vec::new();
    append_selected_lines(&data, e.line_start as usize, e.line_end as usize, &mut selected);
    r.actual_hash = sha256::hex(&sha256::digest(&selected));
    if r.actual_hash != expected {
        r.message = format!(
            "content hash mismatch: expected {}, got {}",
            e.content_hash, r.actual_hash
        );
        r.message += &location_clause(
            &data,
            line_count as i64,
            e.line_start,
            e.line_end,
            &e.content_hash,
            options,
        );
        return r;
    }
    r.status = "verified".into();
    r
}

// normalized_expected_hash strips an optional "sha256:" prefix, requires exactly
// 64 hex digits, and lowercases the result. It returns None for an unusable
// fingerprint, which relocation treats as "no statement can be made".
fn normalized_expected_hash(value: &str) -> Option<String> {
    let hex_value = value.strip_prefix("sha256:").unwrap_or(value);
    if hex_value.len() != 64 || !hex_value.bytes().all(|b| b.is_ascii_hexdigit()) {
        return None;
    }
    Some(hex_value.to_ascii_lowercase())
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

// Anchor re-resolution.
//
// A packet carries no anchor origin and no copy of the cited text: the only
// identity an evidence span has is its SHA-256 content fingerprint, and the
// only shape it has is its recorded line count. Relocation resolves that
// identity against the file, and it never changes a span's status, the report
// counters, the exit code, or any ledger byte: the clause appended to the
// failure message is diagnostic only.

// RELOCATE_BYTE_BUDGET bounds the total number of window bytes hashed while
// resolving one anchor, so re-resolution cannot degrade into an unbounded scan.
const RELOCATE_BYTE_BUDGET: u64 = 8 << 20;

// relocation is the outcome of trying to resolve one anchor.
struct Relocation {
    found: bool,
    line_start: i64,
    line_end: i64,
    shift: i64,
    // exhaustive reports that every window that fits in the file was probed,
    // so "not present in this file" is a statement the search actually earned.
    exhaustive: bool,
}

// line_starts returns the byte offset at which each line begins. Line numbers
// are 1-based, so starts[i-1] is the first byte of line i. It returns an empty
// vector for empty input, which has no lines.
fn line_starts(data: &[u8]) -> Vec<usize> {
    let mut starts = Vec::new();
    let mut offset = 0usize;
    while offset < data.len() {
        starts.push(offset);
        match data[offset..].iter().position(|&b| b == b'\n') {
            Some(index) => offset += index + 1,
            None => break,
        }
    }
    starts
}

// content_end returns the offset just past the last content byte of 1-based
// line number, excluding its terminator. A "\r" immediately before the "\n" is
// part of the terminator; a lone "\r" on an unterminated final line is content.
fn content_end(data: &[u8], starts: &[usize], line: usize) -> usize {
    let begin = starts[line - 1];
    let mut end = data.len();
    if line < starts.len() {
        end = starts[line];
    }
    if end > begin && data[end - 1] == b'\n' {
        end -= 1;
        // A "\r" is part of a "\r\n" terminator only. On an unterminated final
        // line it stays content, exactly as the verification path keeps it.
        if end > begin && data[end - 1] == b'\r' {
            end -= 1;
        }
    }
    end
}

// window_hash hashes lines [line_start, line_end] exactly as
// append_selected_lines does: the selected lines are joined with "\n" and a
// final "\n" is appended for multi-line content or a non-empty single line. It
// differs only in how it locates the lines - from a precomputed start index
// rather than by re-walking the file from byte zero - which is what makes
// probing many candidate windows affordable.
fn window_hash(data: &[u8], starts: &[usize], line_start: usize, line_end: usize) -> String {
    let mut payload: Vec<u8> = Vec::new();
    let mut line = line_start;
    while line <= line_end {
        if line > line_start {
            payload.push(b'\n');
        }
        payload.extend_from_slice(&data[starts[line - 1]..content_end(data, starts, line)]);
        line += 1;
    }
    if line_end > line_start || content_end(data, starts, line_start) > starts[line_start - 1] {
        payload.push(b'\n');
    }
    sha256::hex(&sha256::digest(&payload))
}

// resolve_anchor searches for the recorded fingerprint. Candidate windows keep
// the cited line count, and are probed nearest-first: the cited start, then one
// line below, one line above, and so on outward, preferring the lower line
// number on a tie. A window that does not fit in the file is skipped.
fn resolve_anchor(
    data: &[u8],
    starts: &[usize],
    total_lines: i64,
    line_start: i64,
    line_end: i64,
    expected: &str,
) -> Relocation {
    let extent = line_end.saturating_sub(line_start).saturating_add(1);
    if extent < 1 || total_lines < extent {
        // No window of the recorded shape fits anywhere in the file.
        return Relocation {
            found: false,
            line_start: 0,
            line_end: 0,
            shift: 0,
            exhaustive: true,
        };
    }
    // Every valid start is probed at most once, so counting probes against the
    // number of valid starts tells us exactly whether the search covered the
    // whole file, including when it stops early on the byte budget.
    let valid_starts = total_lines - extent + 1;

    // Order probes by distance from the cited start, but clamp the origin into
    // the range of windows that actually fit.
    let mut origin = line_start;
    if origin < 1 {
        origin = 1;
    }
    if origin > valid_starts {
        origin = valid_starts;
    }

    let mut probes: i64 = 0;
    let mut used: u64 = 0;
    let mut distance: i64 = 0;
    loop {
        let low = origin - distance;
        let high = origin + distance;
        if low < 1 && high > valid_starts {
            break;
        }
        let candidates = [low, high];
        // At distance zero the two directions coincide; probe the origin once.
        let count = if distance == 0 { 1 } else { 2 };
        for index in 0..count {
            let candidate = candidates[index];
            if candidate < 1 || candidate > valid_starts {
                continue;
            }
            let last_line = (candidate + extent - 1) as usize;
            let cost =
                (content_end(data, starts, last_line) - starts[(candidate - 1) as usize]) as u64
                    + extent as u64;
            if used + cost > RELOCATE_BYTE_BUDGET {
                return Relocation {
                    found: false,
                    line_start: 0,
                    line_end: 0,
                    shift: 0,
                    exhaustive: probes == valid_starts,
                };
            }
            used += cost;
            probes += 1;
            if window_hash(data, starts, candidate as usize, last_line) == expected {
                return Relocation {
                    found: true,
                    line_start: candidate,
                    line_end: candidate + extent - 1,
                    shift: candidate - line_start,
                    exhaustive: false,
                };
            }
        }
        distance += 1;
    }
    Relocation {
        found: false,
        line_start: 0,
        line_end: 0,
        shift: 0,
        exhaustive: probes == valid_starts,
    }
}

// location_clause renders the relocation diagnostic for a failed span, or ""
// when relocation is disabled or the recorded fingerprint is unusable.
fn location_clause(
    data: &[u8],
    total_lines: i64,
    line_start: i64,
    line_end: i64,
    content_hash: &str,
    options: &Options,
) -> String {
    if !options.relocate {
        return String::new();
    }
    let Some(expected) = normalized_expected_hash(content_hash) else {
        return String::new();
    };
    relocation_clause(data, total_lines, line_start, line_end, &expected)
}

// relocation_clause renders the diagnostic appended to a failure message. It
// returns "" when no statement can honestly be made, which keeps the message
// byte-identical to the pre-relocation behaviour in that case.
fn relocation_clause(
    data: &[u8],
    total_lines: i64,
    line_start: i64,
    line_end: i64,
    expected: &str,
) -> String {
    if line_end.saturating_sub(line_start).saturating_add(1) < 1 {
        return String::new();
    }
    let starts = line_starts(data);
    let result = resolve_anchor(data, &starts, total_lines, line_start, line_end, expected);
    if result.found {
        format!(
            "; content relocates to lines {}-{} (shift {}; nearest matching window)",
            result.line_start,
            result.line_end,
            signed_shift(result.shift)
        )
    } else if result.exhaustive {
        "; content not found elsewhere in this file".to_string()
    } else {
        "; relocation search stopped after its byte budget".to_string()
    }
}

// signed_shift renders a line shift with an explicit sign so that a relocation
// upwards is never mistaken for a downwards one.
fn signed_shift(shift: i64) -> String {
    if shift < 0 {
        shift.to_string()
    } else {
        format!("+{}", shift)
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::packet::Evidence;

    // The reference hashing path: join the selected lines with "\n" and append
    // a final "\n" for non-empty content. window_hash must agree with it for
    // every window shape the relocation search can probe.
    fn reference_window_hash(data: &[u8], line_start: usize, line_end: usize) -> String {
        let mut joined = Vec::new();
        append_selected_lines(data, line_start, line_end, &mut joined);
        sha256::hex(&sha256::digest(&joined))
    }

    #[test]
    fn window_hash_matches_reference_hashing() {
        let crlf = b"a\r\nb\r\nc\r\n".as_slice();
        let unterminated = b"a\r\nb\rc".as_slice();
        let tail_only = b"tail".as_slice();
        let single_empty = b"\n".as_slice();
        // These payloads end an unterminated final line with a bare "\r": the
        // relocation path must keep that "\r" as content, as verification does.
        let trail_cr_after_nl = b"a\nb\r".as_slice();
        let bare_trail_cr = b"b\r".as_slice();
        let only_cr = b"\r".as_slice();
        let crlf_then_trail_cr = b"a\r\nb\r".as_slice();
        for (data, line_start, line_end) in [
            (crlf, 1, 1),
            (crlf, 2, 2),
            (crlf, 1, 2),
            (crlf, 2, 3),
            (crlf, 3, 3),
            (unterminated, 1, 1),
            (unterminated, 2, 2),
            (unterminated, 1, 2),
            (tail_only, 1, 1),
            (single_empty, 1, 1),
            (trail_cr_after_nl, 1, 1),
            (trail_cr_after_nl, 2, 2),
            (trail_cr_after_nl, 1, 2),
            (bare_trail_cr, 1, 1),
            (only_cr, 1, 1),
            (crlf_then_trail_cr, 1, 1),
            (crlf_then_trail_cr, 2, 2),
            (crlf_then_trail_cr, 1, 2),
        ] {
            let starts = line_starts(data);
            assert_eq!(
                window_hash(data, &starts, line_start, line_end),
                reference_window_hash(data, line_start, line_end),
                "window {}-{} of {:?}",
                line_start,
                line_end,
                data
            );
        }
    }

    #[test]
    fn relocation_reports_moved_window_and_exact_fit() {
        let data = b"one\ntwo\nthree\nfour\nfive\nsix\n";
        let total = count_lines(data) as i64;
        // Lines 1-2 are cited, but that content now lives at lines 4-5.
        let moved = reference_window_hash(data, 4, 5);
        assert_eq!(
            relocation_clause(data, total, 1, 2, &moved),
            "; content relocates to lines 4-5 (shift +3; nearest matching window)"
        );

        // A single window that exactly fills the file relocates with a zero
        // shift, which is still rendered with an explicit sign.
        let whole = b"alpha\nbeta\n";
        let total = count_lines(whole) as i64;
        let expected = reference_window_hash(whole, 1, 2);
        assert_eq!(
            relocation_clause(whole, total, 1, 2, &expected),
            "; content relocates to lines 1-2 (shift +0; nearest matching window)"
        );
    }

    #[test]
    fn relocation_reports_not_found_and_budget_stop() {
        let data = b"one\ntwo\nthree\n";
        let total = count_lines(data) as i64;
        assert_eq!(
            relocation_clause(data, total, 1, 2, &"ab".repeat(32)),
            "; content not found elsewhere in this file"
        );

        // One window whose own byte span exceeds the budget stops the search
        // before the budget can be earned, so it must not claim exhaustion.
        let mut large = vec![b'x'; 9 << 20];
        large.extend_from_slice(b"\nshort\n");
        let total = count_lines(&large) as i64;
        assert_eq!(
            relocation_clause(&large, total, 1, 2, &"ab".repeat(32)),
            "; relocation search stopped after its byte budget"
        );
    }

    #[test]
    fn relocation_clause_is_omitted_when_no_statement_is_possible() {
        let data = b"a\nb\n";
        let total = count_lines(data) as i64;
        let unusable = "not-a-hash";
        assert_eq!(
            location_clause(data, total, 1, 2, unusable, &Options { relocate: true }),
            ""
        );
        assert_eq!(
            location_clause(data, total, 1, 2, &"ab".repeat(32), &Options { relocate: false }),
            ""
        );
        // An extent below one describes no window at all.
        assert_eq!(relocation_clause(data, total, 2, 1, &"ab".repeat(32)), "");
    }

    #[test]
    fn relocation_is_diagnostic_only_and_keeps_the_span_failed() {
        let root = std::env::temp_dir().join(format!(
            "ownscout-rust-relocate-diagnostic-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        // "one\ntwo\n" is recorded as lines 1-2 but sits at lines 3-4.
        fs::write(root.join("drift.txt"), b"zero\none\ntwo\n").unwrap();
        let item = Evidence {
            evidence_id: "e1".into(),
            path: "drift.txt".into(),
            line_start: 1,
            line_end: 2,
            content_hash: reference_window_hash(b"zero\none\ntwo\n", 2, 3),
            ..Default::default()
        };
        let result = one(&root, &item, &Options { relocate: true });
        assert_eq!(result.status, "failed");
        assert_eq!(
            result.message,
            format!(
                "content hash mismatch: expected {}, got {}; content relocates to lines 2-3 (shift +1; nearest matching window)",
                item.content_hash, result.actual_hash
            )
        );
        // Without the option the message is byte-identical to the historical
        // behaviour: no clause is appended.
        let plain = one(&root, &item, &Options { relocate: false });
        assert_eq!(plain.status, "failed");
        assert!(!plain.message.contains("relocates"));
        fs::remove_dir_all(&root).unwrap();
    }
}
