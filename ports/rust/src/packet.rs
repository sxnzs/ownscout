use crate::json::{object, Value};

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

// unknown_field renders the reference's unknown-field error. Go reports the
// name with %q, i.e. strconv.Quote, so that escaping has to match byte for
// byte.
fn unknown_field(key: &str) -> String {
    format!("unknown field {}", go_quote(key))
}

// fold_rune mirrors Go's encoding/json foldRune for the orbits that can reach
// ASCII. Every declared field name is plain ASCII, so a key can only
// fold-match a field when the whole key folds to ASCII. An exhaustive scan of
// Go's orbit table shows the only non-ASCII runes that fold to ASCII are the
// long s (U+017F -> S) and the Kelvin sign (U+212A -> K); every other rune
// folds within non-ASCII, so leaving it unchanged cannot change whether a name
// matches an ASCII tag. fold_name is therefore matching-equivalent to Go's
// foldName for this packet even though it is not byte-identical for non-ASCII
// input (Go maps U+00F6 to U+00D6; this leaves it as U+00F6, and neither can
// equal an ASCII field name).
fn fold_rune(c: char) -> char {
    match c {
        'a'..='z' => c.to_ascii_uppercase(),
        '\u{017f}' => 'S',
        '\u{212a}' => 'K',
        _ => c,
    }
}
pub(crate) fn fold_name(name: &str) -> String {
    name.chars().map(fold_rune).collect()
}

// canonical_field resolves a JSON key to the declared field it names. The
// contract/evidence decoder (Go's encoding/json adapter) looks for an exact
// match first and only then falls back to a case-folded one; the strict
// node-packet decoder (Go's nodepacket) matches exact names only.
fn canonical_field<'a>(key: &str, allowed: &[&'a str], fold: bool) -> Option<&'a str> {
    if let Some(field) = allowed.iter().find(|field| **field == key) {
        return Some(field);
    }
    if !fold {
        return None;
    }
    let folded = fold_name(key);
    allowed
        .iter()
        .find(|field| fold_name(field) == folded)
        .copied()
}

// go_quote renders a string the way Go's strconv.Quote does.
pub(crate) fn go_quote(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{07}' => out.push_str("\\a"),
            '\u{08}' => out.push_str("\\b"),
            '\u{0c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{0b}' => out.push_str("\\v"),
            c if go_is_print(c) => out.push(c),
            c if (c as u32) < 0x20 || c as u32 == 0x7f => {
                out.push_str(&format!("\\x{:02x}", c as u32))
            }
            c if (c as u32) < 0x10000 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push_str(&format!("\\U{:08x}", c as u32)),
        }
    }
    out.push('"');
    out
}

// go_is_print reproduces Go's strconv.IsPrint exactly. Latin-1 uses its fast
// path; above Latin-1 the rune must fall inside Go's own print ranges and not
// be one of its listed exceptions. The tables are copied verbatim from Go's
// strconv package (isprint.go), so unassigned code points behave identically.
static IS_PRINT16: &[u16] = &[
    0x20, 0x7e, 0xa1, 0x377, 0x37a, 0x37f, 0x384, 0x556, 0x559, 0x58a,
    0x58d, 0x5c7, 0x5d0, 0x5ea, 0x5ef, 0x5f4, 0x606, 0x70d, 0x710, 0x74a,
    0x74d, 0x7b1, 0x7c0, 0x7fa, 0x7fd, 0x82d, 0x830, 0x85b, 0x85e, 0x86a,
    0x870, 0x88f, 0x897, 0x98c, 0x98f, 0x990, 0x993, 0x9b2, 0x9b6, 0x9b9,
    0x9bc, 0x9c4, 0x9c7, 0x9c8, 0x9cb, 0x9ce, 0x9d7, 0x9d7, 0x9dc, 0x9e3,
    0x9e6, 0x9fe, 0xa01, 0xa0a, 0xa0f, 0xa10, 0xa13, 0xa39, 0xa3c, 0xa42,
    0xa47, 0xa48, 0xa4b, 0xa4d, 0xa51, 0xa51, 0xa59, 0xa5e, 0xa66, 0xa76,
    0xa81, 0xab9, 0xabc, 0xacd, 0xad0, 0xad0, 0xae0, 0xae3, 0xae6, 0xaf1,
    0xaf9, 0xb0c, 0xb0f, 0xb10, 0xb13, 0xb39, 0xb3c, 0xb44, 0xb47, 0xb48,
    0xb4b, 0xb4d, 0xb55, 0xb57, 0xb5c, 0xb63, 0xb66, 0xb77, 0xb82, 0xb8a,
    0xb8e, 0xb95, 0xb99, 0xb9f, 0xba3, 0xba4, 0xba8, 0xbaa, 0xbae, 0xbb9,
    0xbbe, 0xbc2, 0xbc6, 0xbcd, 0xbd0, 0xbd0, 0xbd7, 0xbd7, 0xbe6, 0xbfa,
    0xc00, 0xc39, 0xc3c, 0xc4d, 0xc55, 0xc5d, 0xc60, 0xc63, 0xc66, 0xc6f,
    0xc77, 0xcb9, 0xcbc, 0xccd, 0xcd5, 0xcd6, 0xcdc, 0xce3, 0xce6, 0xcf3,
    0xd00, 0xd4f, 0xd54, 0xd63, 0xd66, 0xd96, 0xd9a, 0xdbd, 0xdc0, 0xdc6,
    0xdca, 0xdca, 0xdcf, 0xddf, 0xde6, 0xdef, 0xdf2, 0xdf4, 0xe01, 0xe3a,
    0xe3f, 0xe5b, 0xe81, 0xebd, 0xec0, 0xed9, 0xedc, 0xedf, 0xf00, 0xf6c,
    0xf71, 0xfda, 0x1000, 0x10c7, 0x10cd, 0x10cd, 0x10d0, 0x124d, 0x1250, 0x125d,
    0x1260, 0x128d, 0x1290, 0x12b5, 0x12b8, 0x12c5, 0x12c8, 0x1315, 0x1318, 0x135a,
    0x135d, 0x137c, 0x1380, 0x1399, 0x13a0, 0x13f5, 0x13f8, 0x13fd, 0x1400, 0x169c,
    0x16a0, 0x16f8, 0x1700, 0x1715, 0x171f, 0x1736, 0x1740, 0x1753, 0x1760, 0x1773,
    0x1780, 0x17dd, 0x17e0, 0x17e9, 0x17f0, 0x17f9, 0x1800, 0x1819, 0x1820, 0x1878,
    0x1880, 0x18aa, 0x18b0, 0x18f5, 0x1900, 0x192b, 0x1930, 0x193b, 0x1940, 0x1940,
    0x1944, 0x196d, 0x1970, 0x1974, 0x1980, 0x19ab, 0x19b0, 0x19c9, 0x19d0, 0x19da,
    0x19de, 0x1a1b, 0x1a1e, 0x1a7c, 0x1a7f, 0x1a89, 0x1a90, 0x1a99, 0x1aa0, 0x1aad,
    0x1ab0, 0x1add, 0x1ae0, 0x1aeb, 0x1b00, 0x1bf3, 0x1bfc, 0x1c37, 0x1c3b, 0x1c49,
    0x1c4d, 0x1c8a, 0x1c90, 0x1cba, 0x1cbd, 0x1cc7, 0x1cd0, 0x1cfa, 0x1d00, 0x1f15,
    0x1f18, 0x1f1d, 0x1f20, 0x1f45, 0x1f48, 0x1f4d, 0x1f50, 0x1f7d, 0x1f80, 0x1fd3,
    0x1fd6, 0x1fef, 0x1ff2, 0x1ffe, 0x2010, 0x2027, 0x2030, 0x205e, 0x2070, 0x2071,
    0x2074, 0x209c, 0x20a0, 0x20c1, 0x20d0, 0x20f0, 0x2100, 0x218b, 0x2190, 0x2429,
    0x2440, 0x244a, 0x2460, 0x2b73, 0x2b76, 0x2cf3, 0x2cf9, 0x2d27, 0x2d2d, 0x2d2d,
    0x2d30, 0x2d67, 0x2d6f, 0x2d70, 0x2d7f, 0x2d96, 0x2da0, 0x2e5d, 0x2e80, 0x2ef3,
    0x2f00, 0x2fd5, 0x2ff0, 0x3096, 0x3099, 0x30ff, 0x3105, 0x31e5, 0x31ef, 0xa48c,
    0xa490, 0xa4c6, 0xa4d0, 0xa62b, 0xa640, 0xa6f7, 0xa700, 0xa7dc, 0xa7f1, 0xa82c,
    0xa830, 0xa839, 0xa840, 0xa877, 0xa880, 0xa8c5, 0xa8ce, 0xa8d9, 0xa8e0, 0xa953,
    0xa95f, 0xa97c, 0xa980, 0xa9d9, 0xa9de, 0xaa36, 0xaa40, 0xaa4d, 0xaa50, 0xaa59,
    0xaa5c, 0xaac2, 0xaadb, 0xaaf6, 0xab01, 0xab06, 0xab09, 0xab0e, 0xab11, 0xab16,
    0xab20, 0xab6b, 0xab70, 0xabed, 0xabf0, 0xabf9, 0xac00, 0xd7a3, 0xd7b0, 0xd7c6,
    0xd7cb, 0xd7fb, 0xf900, 0xfa6d, 0xfa70, 0xfad9, 0xfb00, 0xfb06, 0xfb13, 0xfb17,
    0xfb1d, 0xfdcf, 0xfdf0, 0xfe19, 0xfe20, 0xfe6b, 0xfe70, 0xfefc, 0xff01, 0xffbe,
    0xffc2, 0xffc7, 0xffca, 0xffcf, 0xffd2, 0xffd7, 0xffda, 0xffdc, 0xffe0, 0xffee,
    0xfffc, 0xfffd,
];

static IS_NOT_PRINT16: &[u16] = &[
    0xad, 0x38b, 0x38d, 0x3a2, 0x530, 0x590, 0x61c, 0x6dd, 0x83f, 0x85f,
    0x8e2, 0x984, 0x9a9, 0x9b1, 0x9de, 0xa04, 0xa29, 0xa31, 0xa34, 0xa37,
    0xa3d, 0xa5d, 0xa84, 0xa8e, 0xa92, 0xaa9, 0xab1, 0xab4, 0xac6, 0xaca,
    0xb00, 0xb04, 0xb29, 0xb31, 0xb34, 0xb5e, 0xb84, 0xb91, 0xb9b, 0xb9d,
    0xbc9, 0xc0d, 0xc11, 0xc29, 0xc45, 0xc49, 0xc57, 0xc5b, 0xc8d, 0xc91,
    0xca9, 0xcb4, 0xcc5, 0xcc9, 0xcdf, 0xcf0, 0xd0d, 0xd11, 0xd45, 0xd49,
    0xd80, 0xd84, 0xdb2, 0xdbc, 0xdd5, 0xdd7, 0xe83, 0xe85, 0xe8b, 0xea4,
    0xea6, 0xec5, 0xec7, 0xecf, 0xf48, 0xf98, 0xfbd, 0xfcd, 0x10c6, 0x1249,
    0x1257, 0x1259, 0x1289, 0x12b1, 0x12bf, 0x12c1, 0x12d7, 0x1311, 0x1680, 0x176d,
    0x1771, 0x180e, 0x191f, 0x1a5f, 0x1b4d, 0x1f58, 0x1f5a, 0x1f5c, 0x1f5e, 0x1fb5,
    0x1fc5, 0x1fdc, 0x1ff5, 0x208f, 0x2d26, 0x2da7, 0x2daf, 0x2db7, 0x2dbf, 0x2dc7,
    0x2dcf, 0x2dd7, 0x2ddf, 0x2e9a, 0x3000, 0x3040, 0x3130, 0x318f, 0x321f, 0xa9ce,
    0xa9ff, 0xab27, 0xab2f, 0xfb37, 0xfb3d, 0xfb3f, 0xfb42, 0xfb45, 0xfe53, 0xfe67,
    0xfe75, 0xffe7,
];

static IS_PRINT32: &[u32] = &[
    0x10000, 0x1004d, 0x10050, 0x1005d, 0x10080, 0x100fa, 0x10100, 0x10102, 0x10107, 0x10133,
    0x10137, 0x1019c, 0x101a0, 0x101a0, 0x101d0, 0x101fd, 0x10280, 0x1029c, 0x102a0, 0x102d0,
    0x102e0, 0x102fb, 0x10300, 0x10323, 0x1032d, 0x1034a, 0x10350, 0x1037a, 0x10380, 0x103c3,
    0x103c8, 0x103d5, 0x10400, 0x1049d, 0x104a0, 0x104a9, 0x104b0, 0x104d3, 0x104d8, 0x104fb,
    0x10500, 0x10527, 0x10530, 0x10563, 0x1056f, 0x105bc, 0x105c0, 0x105f3, 0x10600, 0x10736,
    0x10740, 0x10755, 0x10760, 0x10767, 0x10780, 0x107ba, 0x10800, 0x10805, 0x10808, 0x10838,
    0x1083c, 0x1083c, 0x1083f, 0x1089e, 0x108a7, 0x108af, 0x108e0, 0x108f5, 0x108fb, 0x1091b,
    0x1091f, 0x10939, 0x1093f, 0x10959, 0x10980, 0x109b7, 0x109bc, 0x109cf, 0x109d2, 0x10a06,
    0x10a0c, 0x10a35, 0x10a38, 0x10a3a, 0x10a3f, 0x10a48, 0x10a50, 0x10a58, 0x10a60, 0x10a9f,
    0x10ac0, 0x10ae6, 0x10aeb, 0x10af6, 0x10b00, 0x10b35, 0x10b39, 0x10b55, 0x10b58, 0x10b72,
    0x10b78, 0x10b91, 0x10b99, 0x10b9c, 0x10ba9, 0x10baf, 0x10c00, 0x10c48, 0x10c80, 0x10cb2,
    0x10cc0, 0x10cf2, 0x10cfa, 0x10d27, 0x10d30, 0x10d39, 0x10d40, 0x10d65, 0x10d69, 0x10d85,
    0x10d8e, 0x10d8f, 0x10e60, 0x10ead, 0x10eb0, 0x10eb1, 0x10ec2, 0x10ec7, 0x10ed0, 0x10ed8,
    0x10efa, 0x10f27, 0x10f30, 0x10f59, 0x10f70, 0x10f89, 0x10fb0, 0x10fcb, 0x10fe0, 0x10ff6,
    0x11000, 0x1104d, 0x11052, 0x11075, 0x1107f, 0x110c2, 0x110d0, 0x110e8, 0x110f0, 0x110f9,
    0x11100, 0x11147, 0x11150, 0x11176, 0x11180, 0x111f4, 0x11200, 0x11241, 0x11280, 0x112a9,
    0x112b0, 0x112ea, 0x112f0, 0x112f9, 0x11300, 0x1130c, 0x1130f, 0x11310, 0x11313, 0x11344,
    0x11347, 0x11348, 0x1134b, 0x1134d, 0x11350, 0x11350, 0x11357, 0x11357, 0x1135d, 0x11363,
    0x11366, 0x1136c, 0x11370, 0x11374, 0x11380, 0x1138b, 0x1138e, 0x113c2, 0x113c5, 0x113d8,
    0x113e1, 0x113e2, 0x11400, 0x11461, 0x11480, 0x114c7, 0x114d0, 0x114d9, 0x11580, 0x115b5,
    0x115b8, 0x115dd, 0x11600, 0x11644, 0x11650, 0x11659, 0x11660, 0x1166c, 0x11680, 0x116b9,
    0x116c0, 0x116c9, 0x116d0, 0x116e3, 0x11700, 0x1171a, 0x1171d, 0x1172b, 0x11730, 0x11746,
    0x11800, 0x1183b, 0x118a0, 0x118f2, 0x118ff, 0x11906, 0x11909, 0x11909, 0x1190c, 0x11938,
    0x1193b, 0x11946, 0x11950, 0x11959, 0x119a0, 0x119a7, 0x119aa, 0x119d7, 0x119da, 0x119e4,
    0x11a00, 0x11a47, 0x11a50, 0x11aa2, 0x11ab0, 0x11af8, 0x11b00, 0x11b09, 0x11b60, 0x11b67,
    0x11bc0, 0x11be1, 0x11bf0, 0x11bf9, 0x11c00, 0x11c45, 0x11c50, 0x11c6c, 0x11c70, 0x11c8f,
    0x11c92, 0x11cb6, 0x11d00, 0x11d36, 0x11d3a, 0x11d47, 0x11d50, 0x11d59, 0x11d60, 0x11d98,
    0x11da0, 0x11da9, 0x11db0, 0x11ddb, 0x11de0, 0x11de9, 0x11ee0, 0x11ef8, 0x11f00, 0x11f3a,
    0x11f3e, 0x11f5a, 0x11fb0, 0x11fb0, 0x11fc0, 0x11ff1, 0x11fff, 0x12399, 0x12400, 0x12474,
    0x12480, 0x12543, 0x12f90, 0x12ff2, 0x13000, 0x1342f, 0x13440, 0x13455, 0x13460, 0x143fa,
    0x14400, 0x14646, 0x16100, 0x16139, 0x16800, 0x16a38, 0x16a40, 0x16a69, 0x16a6e, 0x16ac9,
    0x16ad0, 0x16aed, 0x16af0, 0x16af5, 0x16b00, 0x16b45, 0x16b50, 0x16b77, 0x16b7d, 0x16b8f,
    0x16d40, 0x16d79, 0x16e40, 0x16e9a, 0x16ea0, 0x16eb8, 0x16ebb, 0x16ed3, 0x16f00, 0x16f4a,
    0x16f4f, 0x16f87, 0x16f8f, 0x16f9f, 0x16fe0, 0x16fe4, 0x16ff0, 0x16ff6, 0x17000, 0x18cd5,
    0x18cff, 0x18d1e, 0x18d80, 0x18df2, 0x1aff0, 0x1b122, 0x1b132, 0x1b132, 0x1b150, 0x1b152,
    0x1b155, 0x1b155, 0x1b164, 0x1b167, 0x1b170, 0x1b2fb, 0x1bc00, 0x1bc6a, 0x1bc70, 0x1bc7c,
    0x1bc80, 0x1bc88, 0x1bc90, 0x1bc99, 0x1bc9c, 0x1bc9f, 0x1cc00, 0x1ccfc, 0x1cd00, 0x1ceb3,
    0x1ceba, 0x1ced0, 0x1cee0, 0x1cef0, 0x1cf00, 0x1cf2d, 0x1cf30, 0x1cf46, 0x1cf50, 0x1cfc3,
    0x1d000, 0x1d0f5, 0x1d100, 0x1d126, 0x1d129, 0x1d172, 0x1d17b, 0x1d1ea, 0x1d200, 0x1d245,
    0x1d2c0, 0x1d2d3, 0x1d2e0, 0x1d2f3, 0x1d300, 0x1d356, 0x1d360, 0x1d378, 0x1d400, 0x1d49f,
    0x1d4a2, 0x1d4a2, 0x1d4a5, 0x1d4a6, 0x1d4a9, 0x1d50a, 0x1d50d, 0x1d546, 0x1d54a, 0x1d6a5,
    0x1d6a8, 0x1d7cb, 0x1d7ce, 0x1da8b, 0x1da9b, 0x1daaf, 0x1df00, 0x1df1e, 0x1df25, 0x1df2a,
    0x1e000, 0x1e018, 0x1e01b, 0x1e02a, 0x1e030, 0x1e06d, 0x1e08f, 0x1e08f, 0x1e100, 0x1e12c,
    0x1e130, 0x1e13d, 0x1e140, 0x1e149, 0x1e14e, 0x1e14f, 0x1e290, 0x1e2ae, 0x1e2c0, 0x1e2f9,
    0x1e2ff, 0x1e2ff, 0x1e4d0, 0x1e4f9, 0x1e5d0, 0x1e5fa, 0x1e5ff, 0x1e5ff, 0x1e6c0, 0x1e6f5,
    0x1e6fe, 0x1e6ff, 0x1e7e0, 0x1e8c4, 0x1e8c7, 0x1e8d6, 0x1e900, 0x1e94b, 0x1e950, 0x1e959,
    0x1e95e, 0x1e95f, 0x1ec71, 0x1ecb4, 0x1ed01, 0x1ed3d, 0x1ee00, 0x1ee24, 0x1ee27, 0x1ee3b,
    0x1ee42, 0x1ee42, 0x1ee47, 0x1ee54, 0x1ee57, 0x1ee64, 0x1ee67, 0x1ee9b, 0x1eea1, 0x1eebb,
    0x1eef0, 0x1eef1, 0x1f000, 0x1f02b, 0x1f030, 0x1f093, 0x1f0a0, 0x1f0ae, 0x1f0b1, 0x1f0f5,
    0x1f100, 0x1f1ad, 0x1f1e6, 0x1f202, 0x1f210, 0x1f23b, 0x1f240, 0x1f248, 0x1f250, 0x1f251,
    0x1f260, 0x1f265, 0x1f300, 0x1f6d8, 0x1f6dc, 0x1f6ec, 0x1f6f0, 0x1f6fc, 0x1f700, 0x1f7d9,
    0x1f7e0, 0x1f7eb, 0x1f7f0, 0x1f7f0, 0x1f800, 0x1f80b, 0x1f810, 0x1f847, 0x1f850, 0x1f859,
    0x1f860, 0x1f887, 0x1f890, 0x1f8ad, 0x1f8b0, 0x1f8bb, 0x1f8c0, 0x1f8c1, 0x1f8d0, 0x1f8d8,
    0x1f900, 0x1fa57, 0x1fa60, 0x1fa6d, 0x1fa70, 0x1fa7c, 0x1fa80, 0x1fa8a, 0x1fa8e, 0x1fac8,
    0x1facd, 0x1fadc, 0x1fadf, 0x1faea, 0x1faef, 0x1faf8, 0x1fb00, 0x1fbfa, 0x20000, 0x2a6df,
    0x2a700, 0x2b81d, 0x2b820, 0x2cead, 0x2ceb0, 0x2ebe0, 0x2ebf0, 0x2ee5d, 0x2f800, 0x2fa1d,
    0x30000, 0x3134a, 0x31350, 0x33479, 0xe0100, 0xe01ef,
];

static IS_NOT_PRINT32: &[u16] = &[
    0xc, 0x27, 0x3b, 0x3e, 0x18f, 0x39e, 0x57b, 0x58b, 0x593, 0x596,
    0x5a2, 0x5b2, 0x5ba, 0x786, 0x7b1, 0x809, 0x836, 0x856, 0x8f3, 0xa04,
    0xa14, 0xa18, 0xe7f, 0xeaa, 0x10bd, 0x1135, 0x11e0, 0x1212, 0x1287, 0x1289,
    0x128e, 0x129e, 0x1304, 0x1329, 0x1331, 0x1334, 0x133a, 0x138a, 0x138f, 0x13b6,
    0x13c1, 0x13c6, 0x13cb, 0x13d6, 0x145c, 0x1914, 0x1917, 0x1936, 0x1c09, 0x1c37,
    0x1ca8, 0x1d07, 0x1d0a, 0x1d3b, 0x1d3e, 0x1d66, 0x1d69, 0x1d8f, 0x1d92, 0x1f11,
    0x246f, 0x6a5f, 0x6abf, 0x6b5a, 0x6b62, 0xaff4, 0xaffc, 0xafff, 0xd455, 0xd49d,
    0xd4ad, 0xd4ba, 0xd4bc, 0xd4c4, 0xd506, 0xd515, 0xd51d, 0xd53a, 0xd53f, 0xd545,
    0xd551, 0xdaa0, 0xe007, 0xe022, 0xe025, 0xe6df, 0xe7e7, 0xe7ec, 0xe7ef, 0xe7ff,
    0xee04, 0xee20, 0xee23, 0xee28, 0xee33, 0xee38, 0xee3a, 0xee48, 0xee4a, 0xee4c,
    0xee50, 0xee53, 0xee58, 0xee5a, 0xee5c, 0xee5e, 0xee60, 0xee63, 0xee6b, 0xee73,
    0xee78, 0xee7d, 0xee7f, 0xee8a, 0xeea4, 0xeeaa, 0xf0c0, 0xf0d0, 0xfac7, 0xfb93,
];

fn lower_bound<T: Ord>(values: &[T], needle: T) -> usize {
    match values.binary_search(&needle) {
        Ok(index) | Err(index) => index,
    }
}

fn go_is_print(c: char) -> bool {
    let r = c as u32;
    if r <= 0xFF {
        if (0x20..=0x7E).contains(&r) {
            return true;
        }
        if (0xA1..=0xFF).contains(&r) {
            return r != 0xAD;
        }
        return false;
    }
    if r < 1 << 16 {
        let rr = r as u16;
        let i = lower_bound(IS_PRINT16, rr);
        if i >= IS_PRINT16.len() || rr < IS_PRINT16[i & !1] || IS_PRINT16[i | 1] < rr {
            return false;
        }
        return IS_NOT_PRINT16.binary_search(&rr).is_err();
    }
    let i = lower_bound(IS_PRINT32, r);
    if i >= IS_PRINT32.len() || r < IS_PRINT32[i & !1] || IS_PRINT32[i | 1] < r {
        return false;
    }
    if r >= 0x20000 {
        return true;
    }
    IS_NOT_PRINT32.binary_search(&((r - 0x10000) as u16)).is_err()
}

fn string(map: &std::collections::BTreeMap<&str, &Value>, key: &str) -> Result<String, String> {
    string_at(map, key, &format!("Packet.{}", key))
}
fn string_value(value: &Value, path: &str) -> Result<String, String> {
    match value {
        Value::String(v) => Ok(v.clone()),
        Value::Null => Ok(String::new()),
        v => Err(type_error(path, "string", v)),
    }
}
fn string_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<String, String> {
    match map.get(key) {
        None => Ok(String::new()),
        Some(v) => string_value(v, path),
    }
}
fn integer_value(value: &Value, path: &str) -> Result<i64, String> {
    let expected = if path.ends_with("max_bytes") || path.ends_with("used_bytes") {
        "int64"
    } else {
        "int"
    };
    match value {
        Value::Number(v) => v
            .parse()
            .map_err(|_| type_error(path, expected, &Value::Number(v.clone()))),
        Value::Null => Ok(0),
        v => Err(type_error(path, expected, v)),
    }
}
fn integer_at(
    map: &std::collections::BTreeMap<&str, &Value>,
    key: &str,
    path: &str,
) -> Result<i64, String> {
    match map.get(key) {
        None => Ok(0),
        Some(v) => integer_value(v, path),
    }
}
fn boolean_value(value: &Value, path: &str) -> Result<bool, String> {
    match value {
        Value::Bool(v) => Ok(*v),
        Value::Null => Ok(false),
        v => Err(type_error(path, "bool", v)),
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
        Some(v) => strings_value(v, path),
    }
}
fn strings_value(value: &Value, path: &str) -> Result<Option<Vec<String>>, String> {
    match value {
        Value::Array(items) => items
            .iter()
            .map(|v| match v {
                Value::String(s) => Ok(s.clone()),
                v => Err(type_error(path, "string", v)),
            })
            .collect::<Result<Vec<_>, _>>()
            .map(Some),
        Value::Null => Ok(None),
        v => Err(type_error(path, "[]string", v)),
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
// object_fields returns a nested object's fields in document order. Callers
// walk them once, checking each key as they reach it, so that unknown-field and
// type errors are reported in the order the reference reports them. Null
// decodes as an absent object.
fn object_fields<'a>(
    value: &'a Value,
    path: &str,
    expected: &str,
) -> Result<&'a [(String, Value)], String> {
    match value {
        Value::Null => Ok(&[]),
        Value::Object(fields) => Ok(fields),
        _ => Err(type_error(path, expected, value)),
    }
}
const PACKET_FIELDS: &[&str] = &[
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
];

pub fn decode(data: &[u8]) -> Result<Packet, String> {
    decode_packet(data, true)
}

// decode_strict mirrors internal/nodepacket.Decode, the wire-shape decoder used
// on the node command boundary: input must be valid UTF-8 with no unpaired
// surrogate escapes, field names match exactly, and duplicate keys, unknown
// keys, explicit nulls and values of the wrong JSON type are rejected. The
// contract/evidence path keeps encoding/json behaviour via `decode`.
pub fn decode_strict(data: &[u8]) -> Result<Packet, String> {
    if data.len() > 1 << 20 {
        return Err("nodepacket: packet: input exceeds 1 MiB".into());
    }
    if std::str::from_utf8(data).is_err() {
        return Err("nodepacket: packet: input is not valid UTF-8".into());
    }
    if !valid_unicode_escapes(data) {
        return Err("nodepacket: packet: invalid Unicode escape".into());
    }
    let value =
        crate::json::parse(data).map_err(|_| "nodepacket: packet: invalid JSON".to_string())?;
    check_strict(&value)?;
    build_packet(&value, false)
}

fn decode_packet(data: &[u8], fold: bool) -> Result<Packet, String> {
    if data.len() > 1 << 20 {
        return Err("nodepacket: packet: input exceeds 1 MiB".into());
    }
    let value = crate::json::parse(data).map_err(|e| format!("nodepacket: packet: {}", e))?;
    build_packet(&value, fold)
}

fn build_packet(value: &Value, fold: bool) -> Result<Packet, String> {
    let raw_fields = object(value)?;
    // Resolve each key to its declared field, in document order, validating as
    // we go. The resolved map is keyed by the declared name so the construction
    // below works for case-variant keys too.
    let mut map = std::collections::BTreeMap::new();
    for (key, value) in raw_fields {
        let Some(field) = canonical_field(key, PACKET_FIELDS, fold) else {
            return Err(unknown_field(key));
        };
        match field {
            "packet_id" | "schema_version" | "repo_root" | "head_commit" | "request_id"
            | "issued_at" | "outcome" | "packet_hash" => {
                string_value(value, &format!("Packet.{}", field))?;
            }
            "freshness" => {
                parse_freshness(value, fold)?;
            }
            "authorization" => {
                parse_authorization(value, fold)?;
            }
            "budget" => {
                parse_budget(value, fold)?;
            }
            "evidence" => {
                parse_evidence(value, fold)?;
            }
            "degradations" => {
                strings_value(value, "Packet.degradations")?;
            }
            "provenance" => {
                parse_provenance(value, fold)?;
            }
            _ => return Err(unknown_field(key)),
        }
        map.insert(field, value);
    }
    let freshness = match map.get("freshness") {
        Some(v) => parse_freshness(v, fold)?,
        None => Freshness::default(),
    };
    let authorization = match map.get("authorization") {
        Some(v) => parse_authorization(v, fold)?,
        None => Authorization::default(),
    };
    let budget = match map.get("budget") {
        Some(v) => parse_budget(v, fold)?,
        None => Budget::default(),
    };
    let evidence = match map.get("evidence") {
        Some(v) => parse_evidence(v, fold)?,
        None => None,
    };
    let degradations = strings(&map, "degradations")?;
    let provenance = match map.get("provenance") {
        Some(v) => parse_provenance(v, fold)?,
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

// ---- strict node-packet wire shape (internal/nodepacket/decode.go) ----

type FieldsCheck = fn(&[(String, Value)], &str) -> Result<(), String>;

enum FieldKind {
    Str,
    Bool,
    Int,
    Object(FieldsCheck),
    StrArray,
    ObjectArray(FieldsCheck),
}

const PACKET_SPEC: &[(&str, FieldKind)] = &[
    ("packet_id", FieldKind::Str),
    ("schema_version", FieldKind::Str),
    ("repo_root", FieldKind::Str),
    ("head_commit", FieldKind::Str),
    ("request_id", FieldKind::Str),
    ("issued_at", FieldKind::Str),
    ("outcome", FieldKind::Str),
    ("freshness", FieldKind::Object(check_freshness_fields)),
    ("authorization", FieldKind::Object(check_authorization_fields)),
    ("budget", FieldKind::Object(check_budget_fields)),
    ("evidence", FieldKind::ObjectArray(check_evidence_fields)),
    ("degradations", FieldKind::StrArray),
    ("provenance", FieldKind::Object(check_provenance_fields)),
    ("packet_hash", FieldKind::Str),
];
const FRESHNESS_SPEC: &[(&str, FieldKind)] = &[
    ("head_commit", FieldKind::Str),
    ("head_anchor", FieldKind::Str),
    ("status", FieldKind::Str),
    ("current", FieldKind::Bool),
    ("is_current", FieldKind::Bool),
    ("checked_at", FieldKind::Str),
];
const AUTHORIZATION_SPEC: &[(&str, FieldKind)] = &[
    ("level", FieldKind::Str),
    ("reason", FieldKind::Str),
];
const BUDGET_SPEC: &[(&str, FieldKind)] = &[
    ("max_evidence", FieldKind::Int),
    ("used_evidence", FieldKind::Int),
    ("max_bytes", FieldKind::Int),
    ("used_bytes", FieldKind::Int),
];
const EVIDENCE_SPEC: &[(&str, FieldKind)] = &[
    ("evidence_id", FieldKind::Str),
    ("kind", FieldKind::Str),
    ("path", FieldKind::Str),
    ("commit", FieldKind::Str),
    ("line_start", FieldKind::Int),
    ("line_end", FieldKind::Int),
    ("source", FieldKind::Str),
    ("content_hash", FieldKind::Str),
    ("collected_at", FieldKind::Str),
    ("verifier_status", FieldKind::Str),
];
const PROVENANCE_SPEC: &[(&str, FieldKind)] = &[
    ("collector", FieldKind::Str),
    ("tool", FieldKind::Str),
    ("version", FieldKind::Str),
    ("tool_version", FieldKind::Str),
];

fn check_freshness_fields(fields: &[(String, Value)], path: &str) -> Result<(), String> {
    check_object(fields, FRESHNESS_SPEC, path)
}
fn check_authorization_fields(fields: &[(String, Value)], path: &str) -> Result<(), String> {
    check_object(fields, AUTHORIZATION_SPEC, path)
}
fn check_budget_fields(fields: &[(String, Value)], path: &str) -> Result<(), String> {
    check_object(fields, BUDGET_SPEC, path)
}
fn check_evidence_fields(fields: &[(String, Value)], path: &str) -> Result<(), String> {
    check_object(fields, EVIDENCE_SPEC, path)
}
fn check_provenance_fields(fields: &[(String, Value)], path: &str) -> Result<(), String> {
    check_object(fields, PROVENANCE_SPEC, path)
}

fn check_strict(value: &Value) -> Result<(), String> {
    match value {
        Value::Object(fields) => check_object(fields, PACKET_SPEC, ""),
        _ => reject("packet", "expected object"),
    }
}

fn check_object(
    fields: &[(String, Value)],
    spec: &[(&str, FieldKind)],
    path: &str,
) -> Result<(), String> {
    let mut seen: Vec<&str> = Vec::new();
    for (key, value) in fields {
        let Some((name, kind)) = spec.iter().find(|(name, _)| *name == key.as_str()) else {
            return reject(path, "unknown field");
        };
        if seen.contains(name) {
            return reject(path, "duplicate key");
        }
        seen.push(name);
        let field_path = if path.is_empty() {
            (*name).to_string()
        } else {
            format!("{}.{}", path, name)
        };
        check_kind(kind, value, &field_path)?;
    }
    Ok(())
}

fn check_kind(kind: &FieldKind, value: &Value, path: &str) -> Result<(), String> {
    match kind {
        FieldKind::Str => match value {
            Value::String(_) => Ok(()),
            _ => reject(path, "expected string"),
        },
        FieldKind::Bool => match value {
            Value::Bool(_) => Ok(()),
            _ => reject(path, "expected boolean"),
        },
        FieldKind::Int => match value {
            Value::Number(n) => n
                .parse::<i64>()
                .map(|_| ())
                .map_err(|_| format!("nodepacket: {}: integer is out of range or not integral", path)),
            _ => reject(path, "expected integer"),
        },
        FieldKind::Object(check) => match value {
            Value::Object(fields) => check(fields, path),
            _ => reject(path, "expected object"),
        },
        FieldKind::StrArray => match value {
            Value::Array(items) => {
                if items.iter().all(|item| matches!(item, Value::String(_))) {
                    Ok(())
                } else {
                    reject(path, "expected string")
                }
            }
            _ => reject(path, "expected array"),
        },
        FieldKind::ObjectArray(check) => match value {
            Value::Array(items) => {
                for (index, item) in items.iter().enumerate() {
                    match item {
                        Value::Object(fields) => check(fields, &format!("{}[{}]", path, index))?,
                        _ => return reject(path, "expected object"),
                    }
                }
                Ok(())
            }
            _ => reject(path, "expected array"),
        },
    }
}

fn reject(path: &str, reason: &str) -> Result<(), String> {
    let path = if path.is_empty() { "packet" } else { path };
    Err(format!("nodepacket: {}: {}", path, reason))
}

// valid_unicode_escapes mirrors internal/nodepacket's preflight: encoding/json
// replaces unpaired surrogate escapes with U+FFFD, but the strict node decoder
// rejects them. Literal escaped backslashes and actual U+FFFD characters pass.
fn valid_unicode_escapes(data: &[u8]) -> bool {
    let mut index = 0usize;
    while index < data.len() {
        if data[index] != b'\\' {
            index += 1;
            continue;
        }
        index += 1;
        if index >= data.len() || data[index] != b'u' {
            index += 1;
            continue;
        }
        let Some(value) = hex_escape(&data[index + 1..]) else {
            return false;
        };
        index += 4;
        match value {
            v if (0xdc00..=0xdfff).contains(&v) => return false,
            v if (0xd800..=0xdbff).contains(&v) => {
                if index + 6 >= data.len() || data[index + 1] != b'\\' || data[index + 2] != b'u'
                {
                    return false;
                }
                let Some(low) = hex_escape(&data[index + 3..]) else {
                    return false;
                };
                if !(0xdc00..=0xdfff).contains(&low) {
                    return false;
                }
                index += 6;
            }
            _ => {}
        }
        index += 1;
    }
    true
}

fn hex_escape(data: &[u8]) -> Option<u16> {
    if data.len() < 4 {
        return None;
    }
    let mut value = 0u16;
    for &digit in &data[..4] {
        value = value.checked_mul(16)?
            + match digit {
                b'0'..=b'9' => (digit - b'0') as u16,
                b'a'..=b'f' => (digit - b'a' + 10) as u16,
                b'A'..=b'F' => (digit - b'A' + 10) as u16,
                _ => return None,
            };
    }
    Some(value)
}

const FRESHNESS_FIELDS: &[&str] = &[
    "head_commit",
    "head_anchor",
    "status",
    "current",
    "is_current",
    "checked_at",
];
fn parse_freshness(v: &Value, fold: bool) -> Result<Freshness, String> {
    let raw = object_fields(v, "Packet.freshness", "contract.Freshness")?;
    let mut out = Freshness::default();
    for (key, value) in raw {
        let Some(field) = canonical_field(key, FRESHNESS_FIELDS, fold) else {
            return Err(unknown_field(key));
        };
        match field {
            "head_commit" => out.head_commit = string_value(value, "Packet.freshness.head_commit")?,
            "head_anchor" => out.head_anchor = string_value(value, "Packet.freshness.head_anchor")?,
            "status" => out.status = string_value(value, "Packet.freshness.status")?,
            "current" => out.current = boolean_value(value, "Packet.freshness.current")?,
            "is_current" => out.is_current = boolean_value(value, "Packet.freshness.is_current")?,
            "checked_at" => out.checked_at = string_value(value, "Packet.freshness.checked_at")?,
            _ => return Err(unknown_field(key)),
        }
    }
    Ok(out)
}
const AUTHORIZATION_FIELDS: &[&str] = &["level", "reason"];
fn parse_authorization(v: &Value, fold: bool) -> Result<Authorization, String> {
    let raw = object_fields(v, "Packet.authorization", "contract.Authorization")?;
    let mut out = Authorization::default();
    for (key, value) in raw {
        let Some(field) = canonical_field(key, AUTHORIZATION_FIELDS, fold) else {
            return Err(unknown_field(key));
        };
        match field {
            "level" => out.level = string_value(value, "Packet.authorization.level")?,
            "reason" => out.reason = string_value(value, "Packet.authorization.reason")?,
            _ => return Err(unknown_field(key)),
        }
    }
    Ok(out)
}
const BUDGET_FIELDS: &[&str] = &["max_evidence", "used_evidence", "max_bytes", "used_bytes"];
fn parse_budget(v: &Value, fold: bool) -> Result<Budget, String> {
    let raw = object_fields(v, "Packet.budget", "contract.Budget")?;
    let mut out = Budget::default();
    for (key, value) in raw {
        let Some(field) = canonical_field(key, BUDGET_FIELDS, fold) else {
            return Err(unknown_field(key));
        };
        match field {
            "max_evidence" => {
                out.max_evidence = integer_value(value, "Packet.budget.max_evidence")?
            }
            "used_evidence" => {
                out.used_evidence = integer_value(value, "Packet.budget.used_evidence")?
            }
            "max_bytes" => out.max_bytes = integer_value(value, "Packet.budget.max_bytes")?,
            "used_bytes" => out.used_bytes = integer_value(value, "Packet.budget.used_bytes")?,
            _ => return Err(unknown_field(key)),
        }
    }
    Ok(out)
}
fn parse_evidence(v: &Value, fold: bool) -> Result<Option<Vec<Evidence>>, String> {
    const EVIDENCE_FIELDS: &[&str] = &[
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
    ];
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
                let mut m = std::collections::BTreeMap::new();
                for (key, value) in fields {
                    let Some(field) = canonical_field(key, EVIDENCE_FIELDS, fold) else {
                        return Err(unknown_field(key));
                    };
                    let field_path = format!("{}.{}", path, field);
                    match field {
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
                        _ => return Err(unknown_field(key)),
                    }
                    m.insert(field, value);
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
const PROVENANCE_FIELDS: &[&str] = &["collector", "tool", "version", "tool_version"];
fn parse_provenance(v: &Value, fold: bool) -> Result<Provenance, String> {
    let raw = object_fields(v, "Packet.provenance", "contract.Provenance")?;
    let mut out = Provenance::default();
    for (key, value) in raw {
        let Some(field) = canonical_field(key, PROVENANCE_FIELDS, fold) else {
            return Err(unknown_field(key));
        };
        match field {
            "collector" => out.collector = string_value(value, "Packet.provenance.collector")?,
            "tool" => out.tool = string_value(value, "Packet.provenance.tool")?,
            "version" => out.version = string_value(value, "Packet.provenance.version")?,
            "tool_version" => {
                out.tool_version = string_value(value, "Packet.provenance.tool_version")?
            }
            _ => return Err(unknown_field(key)),
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    // Every escaping row Go's %q can produce for a field name, plus the
    // printable runes that must stay literal.
    #[test]
    fn go_quote_escapes_like_go() {
        for (input, expected) in [
            // Quote and backslash always escape.
            ("a\"b\\c", "\"a\\\"b\\\\c\""),
            // The named C escapes.
            (
                "a\u{07}b\u{08}c\u{0c}d\ne\rf\tg\u{0b}h",
                "\"a\\ab\\bc\\fd\\ne\\rf\\tg\\vh\"",
            ),
            // Other control bytes and DEL use \xNN.
            ("a\u{01}b\u{1f}c\u{7f}d", "\"a\\x01b\\x1fc\\x7fd\""),
            // Printable non-ASCII stays literal; non-printable needs \uNNNN.
            ("héllo🎉\u{a0}\u{2028}", "\"héllo🎉\\u00a0\\u2028\""),
            // Above U+FFFF a non-printable rune needs the eight-digit \U form.
            ("astral\u{10fffe}end", "\"astral\\U0010fffeend\""),
            ("astral\u{10ffff}end", "\"astral\\U0010ffffend\""),
            // The soft hyphen and private use are non-printable.
            ("\u{ad}", "\"\\u00ad\""),
            ("\u{e000}", "\"\\ue000\""),
        ] {
            assert_eq!(go_quote(input), expected, "input {:?}", input);
        }
    }

    #[test]
    fn decode_reports_the_first_error_in_document_order() {
        fn decode_err(data: &[u8]) -> String {
            match decode(data) {
                Ok(_) => panic!("expected a decode error for {:?}", data),
                Err(e) => e,
            }
        }
        // An unknown field before the badly-typed one wins.
        let unknown_first = br#"{"budget":{"aaa_unknown":1,"max_evidence":"notanint"}}"#;
        assert_eq!(
            decode_err(unknown_first),
            "unknown field \"aaa_unknown\""
        );
        // A badly-typed field before the unknown one wins.
        let unknown_last = br#"{"budget":{"max_evidence":"notanint","zzz_unknown":1}}"#;
        assert_eq!(
            decode_err(unknown_last),
            "type mismatch|Packet.budget.max_evidence|int|string"
        );
        // The same precedence holds at the top level.
        assert_eq!(
            decode_err(br#"{"packet_id":123,"zzz_unknown":1}"#),
            "type mismatch|Packet.packet_id|string|number"
        );
        assert_eq!(
            decode_err(br#"{"zzz_unknown":1,"packet_id":123}"#),
            "unknown field \"zzz_unknown\""
        );
        // A nested unknown field still wins over a later top-level type error,
        // because the reference walks depth-first in document order.
        assert_eq!(
            decode_err(br#"{"budget":{"aaa_unknown":1},"packet_id":123}"#),
            "unknown field \"aaa_unknown\""
        );
    }

    fn decode_error(data: &[u8]) -> String {
        match decode(data) {
            Ok(_) => panic!("expected a decode error for {:?}", data),
            Err(e) => e,
        }
    }
    fn decode_strict_error(data: &[u8]) -> String {
        match decode_strict(data) {
            Ok(_) => panic!("expected a strict decode error for {:?}", data),
            Err(e) => e,
        }
    }

    #[test]
    fn case_variant_field_names_fold_only_on_the_contract_path() {
        // The encoding/json adapter folds a key to its declared field.
        assert!(decode(br#"{"SCHEMA_VERSION":"v1","packet_id":"p"}"#).is_ok());
        // The strict nodepacket decoder matches exact names only.
        assert_eq!(
            decode_strict_error(br#"{"SCHEMA_VERSION":"v1"}"#),
            "nodepacket: packet: unknown field"
        );
        // Nested fields fold as well.
        assert!(decode(br#"{"evidence":[{"LINE_START":1}]}"#).is_ok());
        assert!(decode_strict(br#"{"evidence":[{"LINE_START":1}]}"#).is_err());
        // A non-ASCII case variant folds within non-ASCII and cannot equal the
        // ASCII tag, on either path.
        assert_eq!(
            decode_error("{\"\u{f6}utcome\":1}".as_bytes()),
            "unknown field \"\u{f6}utcome\""
        );
    }

    #[test]
    fn decode_substitutes_unpaired_surrogates() {
        fn decoded(data: &[u8]) -> Packet {
            match decode(data) {
                Ok(p) => p,
                Err(e) => panic!("decode failed: {e}"),
            }
        }
        assert_eq!(decoded(br#"{"packet_id":"\uD800"}"#).packet_id, "\u{FFFD}");
        assert_eq!(decoded(br#"{"packet_id":"\uDC00"}"#).packet_id, "\u{FFFD}");
        // A non-partner escape after a high surrogate is not consumed.
        assert_eq!(
            decoded(br#"{"packet_id":"\uD800\u0041"}"#).packet_id,
            "\u{FFFD}A"
        );
        // Two high surrogates yield two replacement runes.
        assert_eq!(
            decoded(br#"{"packet_id":"\uD800\uD800"}"#).packet_id,
            "\u{FFFD}\u{FFFD}"
        );
        // A valid pair still combines.
        assert_eq!(
            decoded(br#"{"packet_id":"\uD83C\uDF89"}"#).packet_id,
            "\u{1F389}"
        );
    }

    #[test]
    fn decode_strict_matches_the_node_packet_decoder() {
        // Contract validation tolerates a duplicate key (last wins); the strict
        // decoder rejects it.
        assert!(decode(br#"{"packet_id":"a","packet_id":"b"}"#).is_ok());
        assert!(decode_strict(br#"{"packet_id":"a","packet_id":"b"}"#).is_err());
        // Explicit null is rejected.
        assert!(decode_strict(br#"{"issued_at":null}"#).is_err());
        // Raw invalid UTF-8 is rejected.
        assert!(decode_strict(b"{\"packet_id\":\"\xff\"}").is_err());
        // Unpaired surrogate escapes are rejected...
        assert!(decode_strict(br#"{"packet_id":"\uD800"}"#).is_err());
        assert!(decode_strict(br#"{"packet_id":"\uDC00"}"#).is_err());
        // ...but a valid pair and a literal escaped backslash are accepted.
        assert!(decode_strict(br#"{"packet_id":"\uD83C\uDF89"}"#).is_ok());
        assert!(decode_strict(br#"{"packet_id":"\\uD800"}"#).is_ok());
    }

    #[test]
    fn fold_name_covers_the_ascii_reaching_orbits() {
        assert_eq!(fold_name("schema_version"), "SCHEMA_VERSION");
        assert_eq!(fold_name("SCHEMA_VERSION"), "SCHEMA_VERSION");
        // The long s (U+017F) and Kelvin sign (U+212A) orbits reach ASCII.
        assert_eq!(fold_name("\u{017f}chema_version"), "SCHEMA_VERSION");
        assert_eq!(fold_name("pac\u{212a}et_id"), "PACKET_ID");
        // Other non-ASCII runes are left alone; Go folds them within non-ASCII,
        // so neither form can equal an ASCII field name.
        assert_eq!(fold_name("\u{f6}utcome"), "\u{f6}UTCOME");
    }

    #[test]
    fn invalid_utf8_field_name_yields_one_replacement_per_byte() {
        // A truncated four-byte sequence is two invalid bytes, so two U+FFFD.
        assert_eq!(
            decode_error(b"{\"\xf0\x9f\":1}"),
            "unknown field \"\u{fffd}\u{fffd}\""
        );
        // A truncated three-byte sequence behaves the same way.
        assert_eq!(
            decode_error(b"{\"\xe0\xa0\":1}"),
            "unknown field \"\u{fffd}\u{fffd}\""
        );
        // A well-formed non-ASCII name stays literal.
        assert_eq!(
            decode_error("{\"caf\u{e9}\":1}".as_bytes()),
            "unknown field \"caf\u{e9}\""
        );
    }
}
