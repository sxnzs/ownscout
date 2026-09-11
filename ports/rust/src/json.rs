use std::collections::BTreeMap;

#[derive(Clone, Debug)]
pub enum Value {
    Null,
    Bool(bool),
    Number(String),
    String(String),
    Array(Vec<Value>),
    Object(Vec<(String, Value)>),
}

pub fn parse(data: &[u8]) -> Result<Value, String> {
    let mut p = Parser { data, pos: 0 };
    let value = p.value()?;
    p.ws();
    if p.pos != data.len() {
        return Err("unexpected data after packet".into());
    }
    Ok(value)
}

pub fn object(value: &Value) -> Result<&[(String, Value)], String> {
    match value {
        Value::Object(fields) => Ok(fields),
        _ => Err("expected object".into()),
    }
}

pub fn unique_object(fields: &[(String, Value)]) -> Result<BTreeMap<&str, &Value>, String> {
    let mut result = BTreeMap::new();
    for (key, value) in fields {
        if result.insert(key.as_str(), value).is_some() {
            return Err(format!("duplicate JSON object key {:?}", key));
        }
    }
    Ok(result)
}

pub fn object_map(fields: &[(String, Value)]) -> BTreeMap<&str, &Value> {
    let mut result = BTreeMap::new();
    for (key, value) in fields {
        result.insert(key.as_str(), value);
    }
    result
}

struct Parser<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Parser<'a> {
    fn ws(&mut self) {
        while self.pos < self.data.len()
            && matches!(self.data[self.pos], b' ' | b'\n' | b'\r' | b'\t')
        {
            self.pos += 1;
        }
    }

    fn value(&mut self) -> Result<Value, String> {
        self.ws();
        match self.data.get(self.pos).copied() {
            Some(b'n') => self.literal(b"null", Value::Null),
            Some(b't') => self.literal(b"true", Value::Bool(true)),
            Some(b'f') => self.literal(b"false", Value::Bool(false)),
            Some(b'"') => Ok(Value::String(self.string()?)),
            Some(b'[') => self.array(),
            Some(b'{') => self.object(),
            Some(b'-' | b'0'..=b'9') => Ok(Value::Number(self.number()?)),
            _ => Err("invalid JSON".into()),
        }
    }

    fn literal(&mut self, expected: &[u8], value: Value) -> Result<Value, String> {
        if self.data.get(self.pos..self.pos + expected.len()) == Some(expected) {
            self.pos += expected.len();
            Ok(value)
        } else {
            Err("invalid JSON".into())
        }
    }

    fn string(&mut self) -> Result<String, String> {
        if self.data.get(self.pos) != Some(&b'"') {
            return Err("expected string".into());
        }
        self.pos += 1;
        let mut out = String::new();
        while self.pos < self.data.len() {
            let c = self.data[self.pos];
            self.pos += 1;
            match c {
                b'"' => return Ok(out),
                b'\\' => {
                    let escaped = *self.data.get(self.pos).ok_or("invalid JSON")?;
                    self.pos += 1;
                    match escaped {
                        b'"' => out.push('"'),
                        b'\\' => out.push('\\'),
                        b'/' => out.push('/'),
                        b'b' => out.push('\u{0008}'),
                        b'f' => out.push('\u{000c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let hi = self.hex4()?;
                            if (0xd800..=0xdbff).contains(&hi) {
                                if self.data.get(self.pos..self.pos + 2) != Some(b"\\u") {
                                    return Err("invalid Unicode escape".into());
                                }
                                self.pos += 2;
                                let lo = self.hex4()?;
                                if !(0xdc00..=0xdfff).contains(&lo) {
                                    return Err("invalid Unicode escape".into());
                                }
                                let code =
                                    0x10000 + ((hi - 0xd800) as u32) * 0x400 + (lo - 0xdc00) as u32;
                                out.push(char::from_u32(code).ok_or("invalid Unicode escape")?);
                            } else if (0xdc00..=0xdfff).contains(&hi) {
                                return Err("invalid Unicode escape".into());
                            } else {
                                out.push(
                                    char::from_u32(hi as u32).ok_or("invalid Unicode escape")?,
                                );
                            }
                        }
                        _ => return Err("invalid JSON".into()),
                    }
                }
                0..=0x1f => return Err("invalid JSON".into()),
                _ => {
                    let start = self.pos - 1;
                    while self.pos < self.data.len() && self.data[self.pos] >= 0x80 {
                        self.pos += 1;
                    }
                    let bytes = if self.pos == start + 1 {
                        &self.data[start..self.pos]
                    } else {
                        &self.data[start..self.pos]
                    };
                    out.push_str(&String::from_utf8_lossy(bytes));
                }
            }
        }
        Err("invalid JSON".into())
    }

    fn hex4(&mut self) -> Result<u16, String> {
        if self.pos + 4 > self.data.len() {
            return Err("invalid Unicode escape".into());
        }
        let mut n = 0u16;
        for c in &self.data[self.pos..self.pos + 4] {
            n = n.checked_mul(16).ok_or("invalid Unicode escape")?
                + match c {
                    b'0'..=b'9' => (c - b'0') as u16,
                    b'a'..=b'f' => (c - b'a' + 10) as u16,
                    b'A'..=b'F' => (c - b'A' + 10) as u16,
                    _ => return Err("invalid Unicode escape".into()),
                };
        }
        self.pos += 4;
        Ok(n)
    }

    fn number(&mut self) -> Result<String, String> {
        let start = self.pos;
        if self.data.get(self.pos) == Some(&b'-') {
            self.pos += 1;
        }
        match self.data.get(self.pos) {
            Some(b'0') => self.pos += 1,
            Some(b'1'..=b'9') => {
                self.pos += 1;
                while matches!(self.data.get(self.pos), Some(b'0'..=b'9')) {
                    self.pos += 1;
                }
            }
            _ => return Err("invalid JSON".into()),
        }
        if self.data.get(self.pos) == Some(&b'.') {
            self.pos += 1;
            let before = self.pos;
            while matches!(self.data.get(self.pos), Some(b'0'..=b'9')) {
                self.pos += 1;
            }
            if before == self.pos {
                return Err("invalid JSON".into());
            }
        }
        if matches!(self.data.get(self.pos), Some(b'e' | b'E')) {
            self.pos += 1;
            if matches!(self.data.get(self.pos), Some(b'+' | b'-')) {
                self.pos += 1;
            }
            let before = self.pos;
            while matches!(self.data.get(self.pos), Some(b'0'..=b'9')) {
                self.pos += 1;
            }
            if before == self.pos {
                return Err("invalid JSON".into());
            }
        }
        Ok(String::from_utf8(self.data[start..self.pos].to_vec()).unwrap())
    }

    fn array(&mut self) -> Result<Value, String> {
        self.pos += 1;
        let mut values = Vec::new();
        self.ws();
        if self.data.get(self.pos) == Some(&b']') {
            self.pos += 1;
            return Ok(Value::Array(values));
        }
        loop {
            values.push(self.value()?);
            self.ws();
            match self.data.get(self.pos) {
                Some(b',') => {
                    self.pos += 1;
                }
                Some(b']') => {
                    self.pos += 1;
                    return Ok(Value::Array(values));
                }
                _ => return Err("invalid JSON".into()),
            }
        }
    }

    fn object(&mut self) -> Result<Value, String> {
        self.pos += 1;
        let mut fields = Vec::new();
        self.ws();
        if self.data.get(self.pos) == Some(&b'}') {
            self.pos += 1;
            return Ok(Value::Object(fields));
        }
        loop {
            self.ws();
            let key = self.string()?;
            self.ws();
            if self.data.get(self.pos) != Some(&b':') {
                return Err("invalid JSON".into());
            }
            self.pos += 1;
            fields.push((key, self.value()?));
            self.ws();
            match self.data.get(self.pos) {
                Some(b',') => self.pos += 1,
                Some(b'}') => {
                    self.pos += 1;
                    return Ok(Value::Object(fields));
                }
                _ => return Err("invalid JSON".into()),
            }
        }
    }
}
