#[derive(Clone)]
pub struct ResultData {
    pub command: String,
    pub ok: bool,
    pub summary: String,
    pub details: Vec<String>,
    pub next_action: String,
}

fn json_escape(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{08}' => out.push_str("\\b"),
            '\u{0c}' => out.push_str("\\f"),
            '<' => out.push_str("\\u003c"),
            '>' => out.push_str("\\u003e"),
            '&' => out.push_str("\\u0026"),
            c if c.is_control() => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

pub fn render(data: &ResultData, json: bool, code: i32) -> (String, i32) {
    if json {
        let details = data
            .details
            .iter()
            .map(|v| json_escape(v))
            .collect::<Vec<_>>()
            .join(",");
        (
            format!(
                "{{\"command\":{},\"ok\":{},\"summary\":{},\"details\":[{}],\"next_action\":{}}}\n",
                json_escape(&data.command),
                data.ok,
                json_escape(&data.summary),
                details,
                json_escape(&data.next_action)
            ),
            code,
        )
    } else {
        let status = if data.ok { "OK" } else { "ERROR" };
        let mut out = format!("{}: {}\n", status, data.summary);
        for detail in &data.details {
            out.push_str(&format!("  - {}\n", detail));
        }
        out.push_str(&format!("Next action: {}\n", data.next_action));
        (out, code)
    }
}

pub fn usage(message: &str, next: &str) -> String {
    format!("error: {}\nNext action: run '{}'.\n", message, next)
}
