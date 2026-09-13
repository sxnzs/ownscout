#!/usr/bin/env node
import * as fs from "node:fs";
import * as path from "node:path";
import { createHash } from "node:crypto";

const VERSION = "0.1.0";
const MAX_INPUT = 1 << 20;
const rootUsage = `OwnScout — local repository evidence checks

Usage:
  ownscout doctor
  ownscout version
  ownscout contract validate --packet <file> [--json]
  ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]
  ownscout ledger verify --ledger <file> [--json]
  ownscout ledger rotate --ledger <file> [--json]
  ownscout node bind --packet <file> [--json]
  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]

Use "ownscout <command> --help" for command details.`;

type AnyObj = { [key: string]: any };
type Violation = { rule: string; field: string; message: string };

function sha256(data: string | Buffer): string {
  return createHash("sha256").update(data).digest("hex");
}
function goJson(value: any): string {
  return JSON.stringify(value).replace(/[\u2028\u2029&<>]/g, c =>
    c === "&" ? "\\u0026" : c === "<" ? "\\u003c" : c === ">" ? "\\u003e" :
    c === "\u2028" ? "\\u2028" : "\\u2029");
}
// goIsPrint reports whether a rune is printable by Go's definition
// (strconv.IsPrint): the Unicode categories L, M, N, P and S, plus the ASCII
// space. The Latin-1 fast path mirrors Go's table exactly.
function goIsPrint(r:number):boolean {
  if (r === 0x20) return true;
  if (r < 0x20 || r === 0x7f) return false;
  if (r < 0x7f) return true;
  if (r <= 0xff) return r >= 0xa1 && r !== 0xad;
  if (r >= 0xd800 && r <= 0xdfff) return false;
  if (r > 0x10ffff) return false;
  return goPrintRe.test(String.fromCodePoint(r));
}
const goPrintRe=/^[\p{L}\p{M}\p{N}\p{P}\p{S}]$/u;
// goQuote renders a string exactly as Go's strconv.Quote - and therefore the
// %q verb in `json: unknown field %q` - does. The name is decoded Unicode, so
// it is walked by code point; a lone surrogate is not a valid rune and Go's
// decoder has already replaced it with U+FFFD, which is printable.
export function goQuote(s:string):string {
  let out='"';
  for (const ch of s) {
    let r=ch.codePointAt(0)!;
    if (r>=0xd800 && r<=0xdfff) r=0xfffd;
    if (r===0x22 || r===0x5c) { out+="\\"+String.fromCodePoint(r); continue; }
    if (goIsPrint(r)) { out+=String.fromCodePoint(r); continue; }
    switch (r) {
      case 0x07: out+="\\a"; break;
      case 0x08: out+="\\b"; break;
      case 0x0c: out+="\\f"; break;
      case 0x0a: out+="\\n"; break;
      case 0x0d: out+="\\r"; break;
      case 0x09: out+="\\t"; break;
      case 0x0b: out+="\\v"; break;
      default:
        if (r<0x20 || r===0x7f) out+="\\x"+r.toString(16).padStart(2,"0");
        else if (r<0x10000) out+="\\u"+r.toString(16).padStart(4,"0");
        else out+="\\U"+r.toString(16).padStart(8,"0");
    }
  }
  return out+'"';
}
// goFoldName mirrors encoding/json's foldName for matching a decoded key
// against a schema field name. Go folds ASCII a-z to A-Z and every non-ASCII
// rune to the minimum of its unicode.SimpleFold orbit. These schemas use ASCII
// field names, so a non-ASCII rune can only ever help a match when SimpleFold
// folds it into ASCII - exactly two runes do, U+017F (long s) and U+212A (Kelvin
// sign). Every other rune is left unchanged, which is observationally identical
// to Go for an ASCII field name; in particular "öutcome" folds to "Öutcome" in
// Go and "öutcome" here, neither of which equals "OUTCOME".
export function goFoldName(s:string):string {
  let out="";
  for (const ch of s) {
    const r=ch.codePointAt(0)!;
    if (r>=0x61 && r<=0x7a) out+=String.fromCharCode(r-0x20);
    else if (r===0x17f) out+="S";
    else if (r===0x212a) out+="K";
    else out+=ch;
  }
  return out;
}
function utf8(data: Buffer): string {
  try { return new TextDecoder("utf-8", { fatal: true }).decode(data); }
  catch { throw new Error("input is not valid UTF-8"); }
}
// Go's utf8.DecodeRune: a valid rune is returned with its width, and every
// malformed sequence is reported as RuneError with width 1. TextDecoder instead
// collapses a maximal subpart into a single U+FFFD, so a raw "\xf0\x9f" becomes
// one replacement rune where Go produces two. The tables below are the first
// byte-class and continuation-accept tables from Go's unicode/utf8.
const GO_AS=0xf0, GO_XX=0xf1, GO_S1=0x02, GO_S2=0x13, GO_S3=0x03, GO_S4=0x23, GO_S5=0x34, GO_S6=0x04, GO_S7=0x44;
const goAccept=[{lo:0x80,hi:0xbf},{lo:0xa0,hi:0xbf},{lo:0x80,hi:0x9f},{lo:0x90,hi:0xbf},{lo:0x80,hi:0x8f}];
function goFirstByte(b:number):number {
  if (b<0x80) return GO_AS;
  if (b<0xc2) return GO_XX;
  if (b<0xe0) return GO_S1;
  if (b===0xe0) return GO_S2;
  if (b<0xed) return GO_S3;
  if (b===0xed) return GO_S4;
  if (b<0xf0) return GO_S3;
  if (b===0xf0) return GO_S5;
  if (b<0xf4) return GO_S6;
  if (b===0xf4) return GO_S7;
  return GO_XX;
}
export function goDecodeRune(data:Buffer,off:number):{r:number;size:number} {
  const n=data.length-off;
  if (n<1) return {r:0xfffd,size:0};
  const p0=data[off], x=goFirstByte(p0);
  if (x===GO_AS) return {r:p0,size:1};
  if (x===GO_XX) return {r:0xfffd,size:1};
  const sz=x&7, acc=goAccept[x>>4];
  if (n<sz) return {r:0xfffd,size:1};
  const b1=data[off+1];
  if (b1<acc.lo || acc.hi<b1) return {r:0xfffd,size:1};
  if (sz<=2) return {r:((p0&0x1f)<<6)|(b1&0x3f),size:2};
  const b2=data[off+2];
  if (b2<0x80 || 0xbf<b2) return {r:0xfffd,size:1};
  if (sz<=3) return {r:((p0&0x0f)<<12)|((b1&0x3f)<<6)|(b2&0x3f),size:3};
  const b3=data[off+3];
  if (b3<0x80 || 0xbf<b3) return {r:0xfffd,size:1};
  return {r:((p0&0x07)<<18)|((b1&0x3f)<<12)|((b2&0x3f)<<6)|(b3&0x3f),size:4};
}
// goDecodeUtf8 decodes a whole buffer with Go's replacement semantics. ASCII
// runs are copied in bulk so a large all-ASCII packet stays cheap.
export function goDecodeUtf8(data:Buffer):string {
  let out="";
  let i=0;
  while (i<data.length) {
    if (data[i]<0x80) {
      let j=i+1;
      while (j<data.length && data[j]<0x80) j++;
      out+=data.toString("latin1",i,j);
      i=j;
      continue;
    }
    const decoded=goDecodeRune(data,i);
    out+=String.fromCodePoint(decoded.r);
    i+=decoded.size;
  }
  return out;
}

// resolveSchemaField returns the schema key a decoded object key names. Field
// names match exactly; there is no case-fold fallback anywhere now that every
// command decodes through the strict nodepacket boundary.
function resolveSchemaField(schema:AnyObj,key:string):string|undefined {
  return Object.prototype.hasOwnProperty.call(schema,key)?key:undefined;
}

class StrictParser {
  private i = 0;
  private readonly s: string;
  private readonly rejectDuplicates: boolean;
  private readonly goErrors: boolean;
  private readonly rejectNull: boolean;
  constructor(s: string, rejectDuplicates = true, goErrors = false, rejectNull = false) {
    this.s = s; this.rejectDuplicates = rejectDuplicates; this.goErrors = goErrors;
    this.rejectNull = rejectNull;
  }
  // JSON whitespace is exactly space, tab, LF and CR. JavaScript's \s also
  // matches \v, \f and NBSP, which encoding/json rejects.
  private ws() { while (this.i < this.s.length) { const c=this.s.charCodeAt(this.i); if (c===0x20||c===0x09||c===0x0a||c===0x0d) this.i++; else break; } }
  private fail(msg = "invalid JSON"): never { throw new Error(msg); }
  private wireType(): string {
    if (this.s.startsWith("null", this.i)) return "null";
    if (this.s[this.i] === "{") return "object";
    if (this.s[this.i] === "[") return "array";
    if (this.s[this.i] === '"') return "string";
    if (this.s.startsWith("true", this.i) || this.s.startsWith("false", this.i)) return "boolean";
    return "number";
  }
  private requireType(schema: any, fieldPath: string, goType: string): void {
    if (!this.goErrors || this.s.startsWith("null", this.i)) return;
    const actual = this.wireType();
    const expected = schema?.array ? "array" : schema && typeof schema === "object" ? "object" :
      schema === "string" ? "string" : schema === "boolean" ? "boolean" : "number";
    if (actual !== expected) {
      this.fail(this.goTypeError(actual, fieldPath, goType));
    }
  }
  private goTypeError(actual: string, fieldPath: string, goType: string): string {
    const target = /^Packet\.evidence\.\d+$/.test(fieldPath) ? fieldPath :
      `Go struct field ${fieldPath}`;
    return `json: cannot unmarshal ${actual} into ${target} of type ${goType}`;
  }
  parse(schema: any): any {
    this.ws();
    const v = this.value(schema, "Packet", "contract.Packet");
    this.ws();
    if (this.i !== this.s.length) this.fail("unexpected data after packet");
    return v;
  }
  private value(schema: any, fieldPath = "Packet", goType = ""): any {
    this.ws();
    if (this.i >= this.s.length) this.fail();
    this.requireType(schema, fieldPath, goType);
    if (this.s.startsWith("null", this.i)) {
      this.i += 4;
      // The single strict packet boundary rejects an explicit null for any
      // field, matching nodepacket; the envelope parser leaves it as null.
      if (this.rejectNull) this.fail("unexpected null");
      return null;
    }
    if (schema === "string") return this.string();
    if (schema === "boolean") {
      if (this.s.startsWith("true", this.i)) { this.i += 4; return true; }
      if (this.s.startsWith("false", this.i)) { this.i += 5; return false; }
      this.fail("expected boolean");
    }
    if (schema === "int" || schema === "int64") {
      const raw = this.numberRaw();
      let n: bigint;
      try { n = BigInt(raw); } catch {
        if (this.goErrors) this.fail(this.goTypeError("number", fieldPath, goType));
        this.fail("expected integer");
      }
      const min = BigInt("-9223372036854775808"), max = BigInt("9223372036854775807");
      if (raw.includes(".") || /e/i.test(raw) || n < min || n > max) {
        if (this.goErrors) this.fail(this.goTypeError("number", fieldPath, goType));
        this.fail("integer is out of range or not integral");
      }
      return Number(n);
    }
    if (schema?.array) {
      if (this.s[this.i++] !== "[") this.fail("expected array");
      const out: any[] = []; this.ws();
      if (this.s[this.i] === "]") { this.i++; return out; }
      while (true) {
        const itemType = goType === "[]contract.Evidence" ? "contract.Evidence" :
          goType === "[]string" ? "string" : "";
        out.push(this.value(schema.array, `${fieldPath}.${out.length}`, itemType)); this.ws();
        if (this.s[this.i] === "]") { this.i++; return out; }
        if (this.s[this.i++] !== ",") this.fail();
      }
    }
    if (schema && typeof schema === "object") {
      if (this.s[this.i++] !== "{") this.fail("expected object");
      const out: AnyObj = {}; const seen = new Set<string>(); this.ws();
      if (this.s[this.i] === "}") { this.i++; return out; }
      while (true) {
        const key = this.string(); this.ws();
        if (seen.has(key) && this.rejectDuplicates) this.fail(`duplicate key ${goQuote(key)}`);
        seen.add(key);
        // Field names match exactly on every command, packets and envelopes
        // alike; there is no case-fold fallback left.
        const field=resolveSchemaField(schema,key);
        if (field===undefined) this.fail(`unknown field ${goQuote(key)}`);
        this.ws();
        if (this.s[this.i++] !== ":") this.fail();
        // The value is stored under the canonical field, but Go's type-error
        // message names the key exactly as it appeared in the document.
        const childPath = `${fieldPath}.${key}`;
        out[field] = this.value(schema[field], childPath, goFieldType(schema, field)); this.ws();
        if (this.s[this.i] === "}") { this.i++; return out; }
        if (this.s[this.i++] !== ",") this.fail();
        this.ws();
      }
    }
    this.fail("unsupported field type");
  }
  private string(): string {
    if (this.s[this.i++] !== '"') this.fail("expected string");
    const start = this.i - 1;
    while (this.i < this.s.length) {
      const c = this.s.charCodeAt(this.i++);
      if (c === 0x22) {
        const raw = this.s.slice(start, this.i);
        try { return JSON.parse(raw); } catch { this.fail("invalid JSON"); }
      }
      if (c === 0x5c) this.i++;
      else if (c < 0x20) this.fail("invalid JSON");
    }
    this.fail("invalid JSON");
  }
  private numberRaw(): string {
    const m = this.s.slice(this.i).match(/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/);
    if (!m) this.fail("expected integer");
    this.i += m[0].length; return m[0];
  }
}

const evidenceSchema = {
  evidence_id: "string", kind: "string", path: "string", commit: "string",
  line_start: "int", line_end: "int", source: "string", content_hash: "string",
  collected_at: "string", verifier_status: "string"
};
const packetSchema = {
  packet_id: "string", schema_version: "string", repo_root: "string", head_commit: "string",
  request_id: "string", issued_at: "string", outcome: "string",
  freshness: { head_commit: "string", head_anchor: "string", status: "string", current: "boolean", is_current: "boolean", checked_at: "string" },
  authorization: { level: "string", reason: "string" },
  budget: { max_evidence: "int", used_evidence: "int", max_bytes: "int64", used_bytes: "int64" },
  evidence: { array: evidenceSchema }, degradations: { array: "string" },
  provenance: { collector: "string", tool: "string", version: "string", tool_version: "string" },
  packet_hash: "string"
};
function goFieldType(schema: any, key: string): string {
  if (schema === packetSchema) {
    const types: AnyObj = {
      packet_id: "string", schema_version: "string", repo_root: "string", head_commit: "string",
      request_id: "string", issued_at: "string", outcome: "string", packet_hash: "string",
      freshness: "contract.Freshness", authorization: "contract.Authorization",
      budget: "contract.Budget", evidence: "[]contract.Evidence", degradations: "[]string",
      provenance: "contract.Provenance"
    };
    return types[key] || "";
  }
  if (schema === evidenceSchema) {
    return key === "line_start" || key === "line_end" ? "int" : "string";
  }
  if (key === "current" || key === "is_current") return "bool";
  if (key === "max_evidence" || key === "used_evidence") return "int";
  if (key === "max_bytes" || key === "used_bytes") return "int64";
  return "string";
}
// hexEscape4 reads four hex digits at s[at..at+3], or -1 when any is not hex,
// mirroring nodepacket.hexEscape.
function hexEscape4(s:string,at:number):number {
  if (at+4>s.length) return -1;
  let value=0;
  for (let k=0;k<4;k++) {
    const c=s.charCodeAt(at+k);
    value<<=4;
    if (c>=0x30 && c<=0x39) value|=c-0x30;
    else if (c>=0x61 && c<=0x66) value|=c-0x61+10;
    else if (c>=0x41 && c<=0x46) value|=c-0x41+10;
    else return -1;
  }
  return value;
}
// validUnicodeEscapes mirrors nodepacket.validUnicodeEscapes: encoding/json
// replaces unpaired surrogate escapes with U+FFFD, but the strict node decoder
// rejects them before tokenization. An escaped backslash is skipped exactly as
// Go skips it, so "\ud83c\udf89" is accepted and a lone "\ud800" is not.
function validUnicodeEscapes(s:string):boolean {
  for (let index=0; index<s.length; index++) {
    if (s[index] !== "\\") continue;
    index++;
    if (index >= s.length || s[index] !== "u") continue;
    const value=hexEscape4(s,index+1);
    if (value < 0) return false;
    index += 4;
    if (value >= 0xdc00 && value <= 0xdfff) return false;
    if (value >= 0xd800 && value <= 0xdbff) {
      if (index+6 >= s.length || s[index+1] !== "\\" || s[index+2] !== "u") return false;
      const low=hexEscape4(s,index+3);
      if (low < 0 || low < 0xdc00 || low > 0xdfff) return false;
      index += 6;
    }
  }
  return true;
}
// decodePacket is the one strict packet boundary every command shares: the
// 1 MiB cap, valid UTF-8, unpaired-surrogate rejection, exact field names,
// duplicate keys rejected and explicit null rejected, matching
// internal/nodepacket.Decode.
function decodePacket(data: Buffer): AnyObj {
  if (data.length > MAX_INPUT) throw new Error("input exceeds 1 MiB");
  const text = utf8(data);
  if (!validUnicodeEscapes(text)) throw new Error("invalid Unicode escape");
  return new StrictParser(text, true, false, true).parse(packetSchema);
}
function decodePacketValid(data: Buffer): { packet: AnyObj; violations: Violation[] } {
  try {
    const packet = decodePacket(data);
    return { packet, violations: validatePacket(packet) };
  } catch (e: any) {
    return { packet: {}, violations: [{ rule: "packet_decode", field: "packet", message: "nodepacket: packet: " + e.message }] };
  }
}

const actions: AnyObj = {
  complete: "autonomous_proceed", partial: "bounded_more_evidence", partial_degraded: "autonomous_proceed",
  no_match: "autonomous_proceed", stale: "bounded_refresh", unavailable: "blocked", blocked: "blocked",
  needs_more_evidence: "bounded_more_evidence", failed_verification: "quarantine", budget_exhausted: "human_approval"
};
function freshnessEmpty(f: AnyObj): boolean {
  return !String(f.head_commit || "").trim() && !String(f.head_anchor || "").trim() &&
    !String(f.status || "").trim() && !f.current && !f.is_current && !String(f.checked_at || "").trim();
}
function budgetEmpty(b: AnyObj): boolean {
  return !b.max_evidence && !b.used_evidence && !b.max_bytes && !b.used_bytes;
}
function provenanceEmpty(p: AnyObj): boolean {
  return !String(p.collector || "").trim() && !String(p.tool || "").trim() &&
    !String(p.version || "").trim() && !String(p.tool_version || "").trim();
}
function validatePacket(p: AnyObj): Violation[] {
  const v: Violation[] = [], add = (rule: string, field: string, message: string) => v.push({ rule, field, message });
  for (const [f, x] of [["packet_id",p.packet_id],["schema_version",p.schema_version],["repo_root",p.repo_root],
    ["head_commit",p.head_commit],["request_id",p.request_id],["issued_at",p.issued_at],["outcome",p.outcome],["packet_hash",p.packet_hash]])
    if (typeof x !== "string" || !x.trim()) add("required_field", f, "required field is missing");
  const fresh = p.freshness || {}, auth = p.authorization || {}, budget = p.budget || {}, prov = p.provenance || {};
  if (freshnessEmpty(fresh)) add("required_field","freshness","required field is missing");
  if (!String(auth.level || "").trim()) add("required_field","authorization","required field is missing");
  if (budgetEmpty(budget)) add("required_field","budget","required field is missing");
  if (!Array.isArray(p.evidence)) add("required_field","evidence","required field is missing");
  if (!Array.isArray(p.degradations)) add("required_field","degradations","required field is missing");
  if (provenanceEmpty(prov)) add("required_field","provenance","required field is missing");
  if (p.schema_version !== "v1") add("schema_version","schema_version","must be v1");
  if (typeof p.outcome !== "string" || !(p.outcome in actions)) add("outcome","outcome","unknown outcome");
  let anchor = String(fresh.head_commit || "").trim() || String(fresh.head_anchor || "").trim();
  if (p.head_commit && anchor && anchor !== p.head_commit) add("freshness","freshness.head_commit","freshness head anchor does not match packet head_commit");
  if (fresh.head_commit && fresh.head_anchor && fresh.head_commit !== fresh.head_anchor) add("freshness","freshness.head_anchor","freshness head anchors disagree");
  if (p.outcome === "complete") {
    if (!anchor) add("freshness","freshness.head_commit","complete packet requires a freshness head anchor");
    const current = ["current","fresh"].includes(String(fresh.status || "").trim().toLowerCase()) || !!fresh.current || !!fresh.is_current;
    if (!current) add("freshness","freshness.status","complete packet requires current freshness");
  }
  const b = budget;
  if (budgetEmpty(b)) add("budget","budget","budget must contain counters");
  else {
    for (const [f, x] of [["max_evidence",b.max_evidence],["used_evidence",b.used_evidence],["max_bytes",b.max_bytes],["used_bytes",b.used_bytes]])
      if (x < 0) add("budget","budget."+f,"budget counter cannot be negative");
    if (b.used_evidence > b.max_evidence) add("budget","budget.used_evidence","used evidence exceeds max_evidence");
    if (b.used_bytes > b.max_bytes) add("budget","budget.used_bytes","used bytes exceeds max_bytes");
  }
  if (Array.isArray(p.degradations)) {
    p.degradations.forEach((x: string, i: number) => { if (!x.trim()) add("degradations",`degradations[${i}]`,"degradation must not be empty"); });
    if (p.outcome === "complete" && p.degradations.length) add("degradations","degradations","complete packet cannot contain degradations");
  }
  {
    const evidenceList = Array.isArray(p.evidence) ? p.evidence : [];
    for (let i=0;i<evidenceList.length;i++) {
      // A null element decodes to Go's zero-valued struct: an empty object whose
      // integer fields are 0, so the line range rules below fire.
      const e=evidenceList[i]||{}, f=`evidence[${i}]`;
      const lineStart=typeof e.line_start==="number"?e.line_start:0;
      const lineEnd=typeof e.line_end==="number"?e.line_end:0;
      for (const k of ["evidence_id","kind","path","commit","source","content_hash","collected_at","verifier_status"])
        if (!String(e[k] ?? "").trim()) add("malformed_evidence",`${f}.${k}`,"required evidence field is missing");
      if (lineStart < 1) add("malformed_evidence",`${f}.line_start`,"line_start must be at least 1");
      if (lineEnd < 1) add("malformed_evidence",`${f}.line_end`,"line_end must be at least 1");
      if (lineStart >= 1 && lineEnd >= 1 && lineEnd < lineStart) add("malformed_evidence",f,"line_end must be greater than or equal to line_start");
      if (!["verified","unverified","failed","unavailable","pending"].includes(e.verifier_status)) add("malformed_evidence",`${f}.verifier_status`,"unknown verifier status");
      if (p.outcome === "complete" && e.verifier_status !== "verified") add("evidence",`${f}.verifier_status`,"complete packet requires verified evidence");
    }
    if (p.outcome === "complete" && evidenceList.length === 0) add("evidence","evidence","complete packet requires at least one evidence entry");
  }
  if (["autonomous","autonomous_proceed","autonomous-proceed"].includes(String(auth.level || "").trim().toLowerCase()) &&
      ["failed_verification","blocked","unavailable","budget_exhausted"].includes(p.outcome))
    add("authorization","authorization.level","autonomous authorization is forbidden for this outcome");
  return v;
}
function packetCanonical(p: AnyObj): AnyObj {
  const f=p.freshness||{}, a=p.authorization||{}, b=p.budget||{}, pr=p.provenance||{};
  const freshness: AnyObj = {head_commit:f.head_commit||""};
  if (f.head_anchor) freshness.head_anchor=f.head_anchor;
  if (f.status) freshness.status=f.status;
  if (f.current) freshness.current=f.current;
  if (f.is_current) freshness.is_current=f.is_current;
  if (f.checked_at) freshness.checked_at=f.checked_at;
  const auth: AnyObj={level:a.level||""}; if (a.reason) auth.reason=a.reason;
  const provenance: AnyObj={}; for (const k of ["collector","tool","version","tool_version"]) if (pr[k]) provenance[k]=pr[k];
  return {packet_id:p.packet_id||"",schema_version:p.schema_version||"",repo_root:p.repo_root||"",head_commit:p.head_commit||"",
    request_id:p.request_id||"",issued_at:p.issued_at||"",outcome:p.outcome||"",freshness,authorization:auth,
    budget:{max_evidence:b.max_evidence||0,used_evidence:b.used_evidence||0,max_bytes:b.max_bytes||0,used_bytes:b.used_bytes||0},
    evidence:(p.evidence||[]).map((e:any)=>({evidence_id:e.evidence_id||"",kind:e.kind||"",path:e.path||"",commit:e.commit||"",line_start:e.line_start||0,line_end:e.line_end||0,source:e.source||"",content_hash:e.content_hash||"",collected_at:e.collected_at||"",verifier_status:e.verifier_status||""})),
    degradations:p.degradations||[],provenance,packet_hash:""};
}

// loadPacket is the single packet boundary shared by every command: a read
// followed by the strict nodepacket decode. The decoder owns the 1 MiB limit,
// so an oversized packet is a hard decode failure rather than a load error.
function loadPacket(pth: string): {packet:AnyObj;violations:Violation[]} {
  let data:Buffer;
  try{data=fs.readFileSync(pth);}
  catch(e:any){
    if(e&&e.code==="ENOENT") throw new Error(`packet file "${pth}" does not exist`);
    if(e&&e.code==="EISDIR") throw new Error(`read packet "${pth}": read ${pth}: is a directory`);
    throw new Error(`open packet "${pth}": ${e&&e.message||e}`);
  }
  return decodePacketValid(data);
}
function isDecodeFailure(violations:Violation[]):boolean{return violations.length===1&&violations[0].rule==="packet_decode";}
function details(v: Violation[]): string[] { return v.map(x=>`${x.rule}: ${x.field}: ${x.message}`); }

type Verification={evidence_id:string;path:string;status:string;expected_hash:string;actual_hash?:string;line_start:number;line_end:number;message?:string};
function repoRoot(raw:string):string {
  if (!raw) throw new Error("repository root is empty");
  const absolute=path.resolve(raw);
  try { const r=fs.realpathSync(absolute); if (!fs.statSync(r).isDirectory()) throw new Error(`repository "${raw}" is not a directory`); return path.normalize(r); }
  catch (e:any) { if (e.message.includes("is not a directory")) throw e; throw new Error(`resolve repository root "${raw}": lstat ${absolute}: no such file or directory`); }
}
function safePath(root:string, raw:string):string {
  if (!raw || path.isAbsolute(raw) || path.parse(raw).root !== "") throw new Error(`evidence path "${raw}" must be repository-relative`);
  if (raw.split(path.sep).includes("..")) throw new Error(`evidence path "${raw}" contains a parent component`);
  const full=path.join(root,raw), rel=path.relative(root,full);
  if (rel===".." || rel.startsWith(".."+path.sep)) throw new Error(`evidence path "${raw}" is outside the repository`);
  let cur=root; for (const c of rel.split(path.sep)) { if (!c || c===".") continue; cur=path.join(cur,c); const st=fs.lstatSync(cur); if (st.isSymbolicLink()) throw new Error(`evidence path "${raw}" contains a symlink`); }
  return full;
}
// Line counting and range hashing mirror the reference: a "\r\n" pair is one
// terminator, a final terminator adds no trailing line, hashing joins the
// selected lines with "\n" and appends a final "\n" for non-empty content,
// and all bytes are raw — the file is never decoded as text.
export function countLines(data:Buffer):number {
  if (data.length===0) return 0;
  let count=0, i=0;
  while ((i=data.indexOf(10,i))!==-1) { count++; i++; }
  if (data[data.length-1]!==10) count++;
  return count;
}
export function hashSelectedLines(data:Buffer,lineStart:number,lineEnd:number):string {
  const h=createHash("sha256");
  let line=1, start=0, firstNonEmpty=false;
  while (line<=lineEnd && start<data.length) {
    const nl=data.indexOf(10,start);
    let end, next=-1;
    if (nl===-1) { end=data.length; }
    else { end=nl; next=nl+1; if (end>start && data[end-1]===13) end--; }
    if (line>=lineStart) {
      if (line>lineStart) h.update("\n");
      h.update(data.subarray(start,end));
      if (line===lineStart) firstNonEmpty=end>start;
    }
    if (next===-1) break;
    start=next; line++;
  }
  if (lineEnd>lineStart || firstNonEmpty) h.update("\n");
  return h.digest("hex");
}
// Anchor re-resolution, mirroring internal/evidence/relocate.go. Relocation is
// diagnostic only: it never changes a status, a counter, the exit code, or a
// ledger byte. The window whose SHA-256 fingerprint matches the recorded one is
// called a relocation, and the clause appended to the failure names where the
// content now lives.
const RELOCATE_BYTE_BUDGET = 8 << 20;
export type Relocation = {found:boolean;lineStart:number;lineEnd:number;shift:number;exhaustive:boolean};

// lineStarts returns the byte offset at which each line begins. Line numbers are
// 1-based, so starts[i-1] is the first byte of line i. Empty input has no lines.
// This is the same line model as countLines: a "\r\n" pair is one terminator,
// and a final terminator does not introduce a trailing empty line.
export function lineStarts(data:Buffer):number[] {
  if (data.length===0) return [];
  const starts:number[]=[];
  let offset=0;
  while (offset<data.length) {
    starts.push(offset);
    const index=data.indexOf(10,offset);
    if (index<0) break;
    offset=index+1;
  }
  return starts;
}
// contentEnd returns the offset just past the last content byte of 1-based line
// number, excluding its terminator. The "\r" of a "\r\n" pair belongs to the
// terminator, but a lone "\r" on an unterminated final line is content and is
// kept - exactly as hashSelectedLines keeps it.
export function contentEnd(data:Buffer,starts:number[],line:number):number {
  const begin=starts[line-1];
  let end=data.length;
  if (line<starts.length) end=starts[line];
  // Strip "\r\n" or "\n" only when the line is actually terminated. An
  // unterminated final line keeps a trailing "\r" as content; stripping it
  // here would let a relocation match a window that verification hashes
  // differently.
  if (end>begin && data[end-1]===10) {
    end--;
    if (end>begin && data[end-1]===13) end--;
  }
  return end;
}
// windowHash hashes lines [lineStart,lineEnd] exactly as hashSelectedLines does,
// locating them from a precomputed start index instead of re-walking the file.
export function windowHash(data:Buffer,starts:number[],lineStart:number,lineEnd:number):string {
  const h=createHash("sha256");
  for (let line=lineStart;line<=lineEnd;line++) {
    if (line>lineStart) h.update("\n");
    h.update(data.subarray(starts[line-1],contentEnd(data,starts,line)));
  }
  if (lineEnd>lineStart || contentEnd(data,starts,lineStart)>starts[lineStart-1]) h.update("\n");
  return h.digest("hex");
}
// resolveAnchor searches for the recorded fingerprint. Candidate windows keep
// the cited line count and are probed nearest-first: the cited start, then one
// line below, one line above, and so on outward, preferring the lower line
// number on a tie. A window that does not fit in the file is skipped, so a file
// that shrank below the cited extent is still searched honestly.
export function resolveAnchor(data:Buffer,starts:number[],totalLines:number,lineStart:number,lineEnd:number,expected:string):Relocation {
  const extent=lineEnd-lineStart+1;
  if (extent<1 || totalLines<extent) return {found:false,lineStart:0,lineEnd:0,shift:0,exhaustive:true};
  // Every valid start is probed at most once, so counting probes against the
  // number of valid starts tells us exactly whether the search covered the
  // whole file, including when it stops early on the byte budget.
  const validStarts=totalLines-extent+1;
  // Order probes by distance from the cited start, but clamp the origin into
  // the range of windows that actually fit. When the cited range lies past the
  // end of a file that shrank, the nearest fitting windows are the last ones.
  let origin=lineStart;
  if (origin<1) origin=1;
  if (origin>validStarts) origin=validStarts;
  let probes=0, used=0;
  for (let distance=0;;distance++) {
    const low=origin-distance, high=origin+distance;
    if (low<1 && high>validStarts) break;
    const candidates=[low,high];
    const count=distance===0?1:2;
    for (let index=0;index<count;index++) {
      const candidate=candidates[index];
      if (candidate<1 || candidate>validStarts) continue;
      const cost=contentEnd(data,starts,candidate+extent-1)-starts[candidate-1]+extent;
      if (used+cost>RELOCATE_BYTE_BUDGET) return {found:false,lineStart:0,lineEnd:0,shift:0,exhaustive:probes===validStarts};
      used+=cost;
      probes++;
      if (windowHash(data,starts,candidate,candidate+extent-1)===expected) return {found:true,lineStart:candidate,lineEnd:candidate+extent-1,shift:candidate-lineStart,exhaustive:false};
    }
  }
  return {found:false,lineStart:0,lineEnd:0,shift:0,exhaustive:probes===validStarts};
}
// signedShift renders a line shift with an explicit sign so that a relocation
// upwards is never mistaken for a downwards one.
function signedShift(shift:number):string { return shift<0?String(shift):"+"+String(shift); }
// relocationClause renders the diagnostic appended to a failure message. It
// returns "" when no statement can honestly be made, which keeps the message
// byte-identical to the pre-relocation behaviour in that case.
export function relocationClause(data:Buffer,totalLines:number,lineStart:number,lineEnd:number,expected:string):string {
  if (lineEnd-lineStart+1<1) return "";
  const result=resolveAnchor(data,lineStarts(data),totalLines,lineStart,lineEnd,expected);
  if (result.found) return "; content relocates to lines "+result.lineStart+"-"+result.lineEnd+" (shift "+signedShift(result.shift)+"; nearest matching window)";
  if (result.exhaustive) return "; content not found elsewhere in this file";
  return "; relocation search stopped after its byte budget";
}
// locationClause renders the relocation diagnostic for a failed span, or "" when
// relocation is disabled or the recorded fingerprint is unusable.
export function locationClause(data:Buffer,totalLines:number,lineStart:number,lineEnd:number,contentHash:string,options:{relocate:boolean}):string {
  if (!options.relocate) return "";
  let expected=contentHash;
  if (expected.startsWith("sha256:")) expected=expected.slice(7);
  if (!/^[0-9a-fA-F]{64}$/.test(expected)) return "";
  return relocationClause(data,totalLines,lineStart,lineEnd,expected.toLowerCase());
}
function verifyEvidence(repo:string,p:AnyObj,options:{relocate:boolean}={relocate:false}):{ok:boolean;results:Verification[];verified:number;failed:number;skipped:number} {  const root=repoRoot(repo), results:Verification[]=[];
  for (const e of p.evidence||[]) {
    const r:Verification={evidence_id:e.evidence_id,path:e.path,status:"failed",expected_hash:e.content_hash,line_start:e.line_start,line_end:e.line_end};
    try {
      let fp:string;
      try { fp=safePath(root,e.path); } catch (x:any) {
        if (String(x.code)==="ENOENT") throw new Error(`cannot access evidence path "${e.path}": lstat ${path.join(root,e.path)}: no such file or directory`);
        throw x;
      }
      const st=fs.lstatSync(fp); if (!st.isFile() || st.isSymbolicLink()) throw new Error(`evidence path "${e.path}" is not a regular file`);
      const data=fs.readFileSync(fp);
      const total=countLines(data);
      if (e.line_start<1 || e.line_end<e.line_start || e.line_end>total) throw new Error(`invalid line range ${e.line_start}-${e.line_end} for ${total} line(s)`+locationClause(data,total,e.line_start,e.line_end,e.content_hash,options));
      let expected=e.content_hash; if (expected.startsWith("sha256:")) expected=expected.slice(7);
      if (!/^[0-9a-fA-F]{64}$/.test(expected)) throw new Error(`invalid SHA-256 content hash "${e.content_hash}"`);
      r.actual_hash=hashSelectedLines(data,e.line_start,e.line_end); if (r.actual_hash!==expected.toLowerCase()) throw new Error(`content hash mismatch: expected ${e.content_hash}, got ${r.actual_hash}`+locationClause(data,total,e.line_start,e.line_end,e.content_hash,options));
      r.status="verified";
    } catch (x:any) { r.message=x.message; }
    results.push(r);
  }
  const verified=results.filter(x=>x.status==="verified").length, failed=results.filter(x=>x.status==="failed").length, skipped=results.filter(x=>x.status==="skipped").length;
  return {ok:failed===0&&skipped===0,results,verified,failed,skipped};
}

const envSchema={schema_version:"string",envelope_id:"string",packet_id:"string",packet_binding_sha256:"string",
  nodes:{array:{node_id:"string",depends_on:{array:"string"},verifier:"string",evidence_ids:{array:"string"}}}};
function parseEnvelope(data:Buffer):AnyObj {
  if(data.length>MAX_INPUT) throw new Error("envelope exceeds 1048576 byte input limit");
  const e=new StrictParser(utf8(data)).parse(envSchema);
  for (const k of ["schema_version","envelope_id","packet_id","packet_binding_sha256","nodes"]) if (!(k in e)) throw new Error("missing required fields");
  if (e.nodes.length === 0) throw new Error("nodes must contain at least one node");
  if (typeof e.schema_version !== "string" || typeof e.envelope_id !== "string" || typeof e.packet_id !== "string" ||
      typeof e.packet_binding_sha256 !== "string" || !Array.isArray(e.nodes)) throw new Error("invalid envelope field type");
  for (const n of e.nodes) {
    for (const k of ["node_id","depends_on","verifier","evidence_ids"]) if (!(k in n)) throw new Error("missing required fields");
    if (typeof n.node_id !== "string" || typeof n.verifier !== "string" ||
        !Array.isArray(n.depends_on) || !Array.isArray(n.evidence_ids) ||
        n.depends_on.some((x:any)=>typeof x !== "string") || n.evidence_ids.some((x:any)=>typeof x !== "string"))
      throw new Error("invalid envelope field type");
  }
  return e;
}
function ident(field:string,x:string):void { if(!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(x)) throw new Error(`${field} "${x}" is not a valid identifier`); }
function binding(p:AnyObj):string{return sha256(goJson(packetCanonical(p)));}
function validateEnvelope(env:AnyObj,p:AnyObj,b:string):number[] {
  if(env.nodes.length===0) throw new Error("nodes must contain at least one node");
  if(env.schema_version!=="node-envelope-v1") throw new Error('schema_version must equal "node-envelope-v1"');
  ident("envelope_id",env.envelope_id); ident("packet_id",env.packet_id);
  if(env.packet_id!==p.packet_id) throw new Error("packet_id does not match packet");
  const calc=binding(p); if(b!==calc) throw new Error("binding does not match computed packet binding");
  if(env.packet_binding_sha256!==calc) throw new Error("packet_binding_sha256 does not match computed packet binding");
  const eids=new Set<string>(); for(const e of p.evidence||[]){ident("packet evidence_id",e.evidence_id);if(eids.has(e.evidence_id))throw new Error(`duplicate packet evidence_id "${e.evidence_id}"`);eids.add(e.evidence_id);}
  const ix=new Map<string,number>(); for(let i=0;i<env.nodes.length;i++){const n=env.nodes[i];ident("node_id",n.node_id);if(ix.has(n.node_id))throw new Error(`duplicate node_id "${n.node_id}"`);ix.set(n.node_id,i);if(n.verifier!=="evidence.current")throw new Error(`node "${n.node_id}": verifier must equal "evidence.current"`);checkRefs("depends_on",n.depends_on);checkRefs("evidence_ids",n.evidence_ids);for(const id of n.evidence_ids)if(!eids.has(id))throw new Error(`node "${n.node_id}": evidence "${id}" is not present in packet`);}
  for(const n of env.nodes)for(const id of n.depends_on){if(id===n.node_id)throw new Error(`node "${n.node_id}": self-dependency`);if(!ix.has(id))throw new Error(`node "${n.node_id}": missing dependency "${id}"`);}
  const deg=env.nodes.map((n:any)=>n.depends_on.length), depend=env.nodes.map(()=>[] as number[]), ready:number[]=[];
  env.nodes.forEach((n:any,i:number)=>{if(!deg[i])ready.push(i);for(const d of n.depends_on)depend[ix.get(d)!].push(i);});
  const order:number[]=[]; while(ready.length){ready.sort((a,z)=>env.nodes[a].node_id.localeCompare(env.nodes[z].node_id));const i=ready.shift()!;order.push(i);for(const c of depend[i])if(--deg[c]===0)ready.push(c);}
  if(order.length!==env.nodes.length)throw new Error("node dependency graph contains a cycle"); return order;
}
function checkRefs(field:string,refs:string[]){const s=new Set<string>();for(const x of refs){ident(field,x);if(s.has(x))throw new Error(`duplicate ${field} reference "${x}"`);s.add(x);}}
function evaluate(env:AnyObj,p:AnyObj,b:string,report:any,order:number[]):any {
  const current=new Map<string,boolean>(); for(const r of report.results){const old=current.get(r.evidence_id);current.set(r.evidence_id,r.status==="verified"&&(old===undefined||old));}
  const statuses=new Map<string,string>(), results:any[]=[]; let ok=true;
  for(const i of order){const n=env.nodes[i];let status="evidence_current",reason="";for(const d of n.depends_on)if(statuses.get(d)!=="evidence_current"){status="blocked";reason=`dependency "${d}" is not evidence_current`;break;}if(status==="evidence_current")for(const id of n.evidence_ids)if(!current.get(id)){status="failed";reason=`evidence "${id}" is missing or not verified`;break;}statuses.set(n.node_id,status);const r:any={node_id:n.node_id,status};if(reason)r.reason=reason;results.push(r);if(status!=="evidence_current")ok=false;}
  return {ok,results};
}

const zero="0".repeat(64);
function safeLedgerPath(ledger:string,repo:string):string {
  const r=repoRoot(repo), p=path.resolve(ledger), rel=path.relative(r,p);
  if(rel==="."||(!rel.startsWith(".."+path.sep)&&rel!=="..")) throw new Error(`ledger path "${p}" is inside repository "${r}"`);
  if(fs.existsSync(p)&&fs.lstatSync(p).isSymbolicLink())throw new Error(`ledger path "${p}" is a symlink`);
  return p;
}
// rejectSymlinkAncestors is ledger.Open's hardening: every component of the
// ledger path must be a real directory, except macOS's /var symlink. The read
// path (ledger.Verify and ledger.Rotate's read) does not harden, so only the
// append path given to node verify rejects a symlinked ancestor.
function permittedSystemSymlink(p:string):boolean{
  if(path.resolve(p)!=="/var") return false;
  try{return fs.realpathSync(p)==="/private/var";}catch{return false;}
}
function rejectSymlinkAncestors(pth:string):void{
  const absolute=path.resolve(pth);
  const root=path.parse(absolute).root;
  let current=root;
  for(const part of absolute.slice(root.length).split(path.sep)){
    if(part===""||part===".") continue;
    current=path.join(current,part);
    let st:fs.Stats;
    try{st=fs.lstatSync(current);}
    catch(e:any){ if(e&&e.code==="ENOENT") continue; throw new Error(`inspect ledger path ancestor "${current}": ${e&&e.message||e}`); }
    if(st.isSymbolicLink()){
      if(permittedSystemSymlink(current)) continue;
      throw new Error(`ledger path "${absolute}" has symlink ancestor "${current}"`);
    }
  }
}
// hashRecord hashes exactly what Go's json.Marshal emits for a ledger Record:
// struct field order, record_hash blanked, and the omitempty reason omitted.
function ledgerRecordCanonical(r:any):any{
  const results=Array.isArray(r.node_results)?r.node_results:[];
  return {schema_version:r.schema_version,seq:r.seq,prev_record_hash:r.prev_record_hash,record_hash:"",envelope_sha256:r.envelope_sha256,packet_binding_sha256:r.packet_binding_sha256,ownscout_version:r.ownscout_version,node_results:results.map((n:any)=>({node_id:n.node_id,status:n.status,...(n.reason?{reason:n.reason}:{})}))};
}
function hashRecord(r:any):string{return sha256(goJson(ledgerRecordCanonical(r)));}
function ledgerOpen(ledger:string,repo:string):{path:string;list:any[]} {
  const p=safeLedgerPath(ledger,repo);
  rejectSymlinkAncestors(p);
  if(!fs.existsSync(path.dirname(p))) throw new Error(`open ledger parent "${path.dirname(p)}": no such file or directory`);
  if (!fs.existsSync(p)) fs.closeSync(fs.openSync(p, "a", 0o600));
  const data=fs.readFileSync(p); if(data.length>MAX_LEDGER)throw new Error("ledger exceeds 1048576 bytes");
  return {path:p,list:validateLedger(data).list};
}
function appendLedger(l:any,b:string,envHash:string,results:any[]):void {
  if(!results.length)throw new Error("node_results must be non-empty");
  const prev=l.list.length?l.list[l.list.length-1].record_hash:zero;
  const rec:any={schema_version:"ownscout-ledger-v1",seq:l.list.length+1,prev_record_hash:prev,record_hash:zero,envelope_sha256:envHash,packet_binding_sha256:b,ownscout_version:VERSION,node_results:results};
  rec.record_hash=hashRecord(rec);const line=goJson(rec)+"\n";if(Buffer.byteLength(line)>64<<10||fs.statSync(l.path).size+Buffer.byteLength(line)>1<<20)throw new Error("ledger exceeds 1048576 bytes");
  fs.appendFileSync(l.path,line,{mode:0o600});l.list.push(rec);
}

// --- ledger verify -------------------------------------------------------
// The reference validates a ledger line with internal/ledger's decodeRecord,
// which walks the document the way Go 1.27's encoding/json v2 token reader
// does. GoDec reproduces that walk (structure, duplicate/unknown fields,
// node_results shape) and buildLedgerRecord reproduces the typed conversion
// and its error text.
class LedgerValidationError extends Error {}
const MAX_LEDGER=1<<20, MAX_NODE_RESULTS=4096;
const RECORD_FIELDS=["schema_version","seq","prev_record_hash","record_hash","envelope_sha256","packet_binding_sha256","ownscout_version","node_results"];
const NODE_RESULT_FIELDS=["node_id","status","reason"];
function isJSONSpace(c:number):boolean{return c===0x20||c===0x09||c===0x0a||c===0x0d;}
function isHexDigit(d:number):boolean{return (d>=0x30&&d<=0x39)||(d>=0x61&&d<=0x66)||(d>=0x41&&d<=0x46);}
function hexDigit(d:number):number{return d<=0x39?d-0x30:(d>=0x61?d-0x61+10:d-0x41+10);}
function validCodepoint(v:number):boolean{return v>=0&&v<=0x10ffff&&!(v>=0xd800&&v<=0xdfff);}
function literalContext(word:string,index:number):string{
  if(word==="true") return index===1?"in literal true (expecting 'r')":index===2?"in literal true (expecting 'u')":"in literal true (expecting 'e')";
  if(word==="false") return index===1?"in literal false (expecting 'a')":index===2?"in literal false (expecting 'l')":index===3?"in literal false (expecting 's')":"in literal false (expecting 'e')";
  return index===1?"in literal null (expecting 'u')":"in literal null (expecting 'l')";
}
function nonCanonicalOrUnknown(key:string,allowed:string[]):string{
  const folded=goFoldName(key);
  for(const candidate of allowed) if(goFoldName(candidate)===folded) return `non-canonical JSON field ${goQuote(key)}; use ${goQuote(candidate)}`;
  return `unknown JSON field ${goQuote(key)}`;
}
function goQuoteChar(c:number):string{
  if(c===0x27) return "'\\''";
  if(c===0x22) return "'\"'";
  if(c===0x5c) return "'\\\\'";
  if(c>=0x20 && c<=0x7e) return "'"+String.fromCharCode(c)+"'";
  if(c===0x07) return "'\\a'";
  if(c===0x08) return "'\\b'";
  if(c===0x09) return "'\\t'";
  if(c===0x0a) return "'\\n'";
  if(c===0x0b) return "'\\v'";
  if(c===0x0c) return "'\\f'";
  if(c===0x0d) return "'\\r'";
  return "'\\x"+c.toString(16).padStart(2,"0")+"'";
}
class GoDec {
  data:Buffer; pos=0;
  constructor(data:Buffer){this.data=data;}
  peek():number{return this.pos<this.data.length?this.data[this.pos]:-1;}
  skipWs(){while(this.pos<this.data.length&&isJSONSpace(this.data[this.pos]))this.pos++;}
  private err(c:number,context:string):Error{return new Error(`invalid character ${goQuoteChar(c)} ${context}`);}
  token():{kind:"delim"|"str"|"other";delim?:number;str?:string}{
    this.skipWs();
    const c=this.peek();
    if(c<0) throw new Error("EOF");
    if(c===0x7b){this.pos++;return {kind:"delim",delim:0x7b};}
    if(c===0x5b){this.pos++;return {kind:"delim",delim:0x5b};}
    if(c===0x22) return {kind:"str",str:this.string()};
    if(c===0x74){this.literal("true");return {kind:"other"};}
    if(c===0x66){this.literal("false");return {kind:"other"};}
    if(c===0x6e){this.literal("null");return {kind:"other"};}
    if(c===0x2d||(c>=0x30&&c<=0x39)){this.number();return {kind:"other"};}
    throw this.err(c,"looking for beginning of value");
  }
  private literal(word:string){
    for(let i=0;i<word.length;i++){
      const c=this.peek();
      if(c<0) throw new Error("unexpected EOF");
      if(c!==word.charCodeAt(i)) throw this.err(c,literalContext(word,i));
      this.pos++;
    }
  }
  private number(){
    if(this.peek()===0x2d) this.pos++;
    let c=this.peek();
    if(c<0) throw new Error("unexpected EOF");
    if(c===0x30) this.pos++;
    else if(c>=0x31&&c<=0x39) this.digits();
    else throw this.err(c,"in numeric literal");
    if(this.peek()===0x2e){
      this.pos++;
      c=this.peek();
      if(c<0) throw new Error("unexpected EOF");
      if(c>=0x30&&c<=0x39) this.digits(); else throw this.err(c,"in numeric literal");
    }
    c=this.peek();
    if(c===0x65||c===0x45){
      this.pos++;
      c=this.peek();
      if(c===0x2b||c===0x2d) this.pos++;
      c=this.peek();
      if(c<0) throw new Error("unexpected EOF");
      if(c>=0x30&&c<=0x39) this.digits(); else throw this.err(c,"in numeric literal");
    }
  }
  private digits(){for(;;){const c=this.peek(); if(c>=0x30&&c<=0x39) this.pos++; else break;}}
  private string():string{
    this.pos++;
    let out="";
    for(;;){
      const c=this.peek();
      if(c<0) throw new Error("unexpected EOF");
      this.pos++;
      if(c===0x22) return out;
      if(c===0x5c){
        const escape=this.pos-1;
        const e=this.peek();
        if(e<0) throw new Error("unexpected EOF");
        this.pos++;
        if(e===0x22) out+='"';
        else if(e===0x5c) out+="\\";
        else if(e===0x2f) out+="/";
        else if(e===0x62) out+="\b";
        else if(e===0x66) out+="\f";
        else if(e===0x6e) out+="\n";
        else if(e===0x72) out+="\r";
        else if(e===0x74) out+="\t";
        else if(e===0x75){
          const digits:number[]=[]; let seen=0;
          while(seen<4){ const d=this.peek(); if(d<0) break; digits.push(d); seen++; this.pos++; }
          if(seen<4 || !digits.every(isHexDigit)) throw new Error(`invalid escape sequence \`${goDecodeUtf8(this.data.subarray(escape,this.pos))}\` in string`);
          let value=digits.reduce((a,d)=>a*16+hexDigit(d),0);
          // A high surrogate followed by a low-surrogate escape is one rune;
          // an unpaired one is the replacement rune.
          if(value>=0xd800&&value<=0xdbff){
            const save=this.pos;
            let combined=-1;
            if(this.peek()===0x5c){
              this.pos++;
              if(this.peek()===0x75){
                this.pos++;
                const low:number[]=[]; let seenLow=0;
                while(seenLow<4){ const d=this.peek(); if(d<0) break; low.push(d); seenLow++; this.pos++; }
                if(seenLow===4&&low.every(isHexDigit)){
                  const lowValue=low.reduce((a,d)=>a*16+hexDigit(d),0);
                  if(lowValue>=0xdc00&&lowValue<=0xdfff) combined=0x10000+((value-0xd800)<<10)+(lowValue-0xdc00);
                }
              }
            }
            if(combined>=0){ out+=String.fromCodePoint(combined); continue; }
            this.pos=save;
            out+="\ufffd";
            continue;
          }
          out+=String.fromCodePoint(validCodepoint(value)?value:0xfffd);
        }
        else throw new Error(`invalid escape sequence \`${goDecodeUtf8(this.data.subarray(escape,this.pos))}\` in string`);
        continue;
      }
      if(c<0x20) throw this.err(c,"in string");
      const start=this.pos-1;
      while(this.pos<this.data.length && this.data[this.pos]>=0x80) this.pos++;
      out+=goDecodeUtf8(this.data.subarray(start,this.pos));
    }
  }
  afterComma(){
    this.pos++; this.skipWs();
    const c=this.peek();
    if(c<0) throw new Error("EOF");
    if(c===0x5d||c===0x7d) throw this.err(0x2c,"looking for beginning of value");
  }
  consumeValue(){
    const t=this.token();
    if(t.kind==="delim"&&t.delim===0x7b) this.consumeObject(null);
    else if(t.kind==="delim"&&t.delim===0x5b) this.consumeArray();
  }
  consumeArray(){
    this.skipWs();
    if(this.peek()===0x5d){this.pos++;return;}
    for(;;){
      this.consumeValue();
      this.skipWs();
      const c=this.peek();
      if(c<0) throw new Error("unexpected end of JSON input");
      if(c===0x2c) this.afterComma();
      else if(c===0x5d){this.pos++;return;}
      else throw this.err(c,"after array element");
    }
  }
  consumeObject(allowed:string[]|null){
    const seen=new Set<string>();
    for(;;){
      this.skipWs();
      const c=this.peek();
      if(c<0) throw new Error("unexpected end of JSON input");
      if(c===0x7d){this.pos++;return;}
      const t=this.token();
      if(t.kind!=="str") throw new Error("object member name must be a string");
      const key=t.str!;
      if(seen.has(key)) throw new Error(`duplicate JSON field ${goQuote(key)}`);
      seen.add(key);
      if(allowed&&!allowed.includes(key)) throw new Error(nonCanonicalOrUnknown(key,allowed));
      this.skipWs();
      const c2=this.peek();
      if(c2<0) throw new Error("EOF");
      if(c2===0x3a) this.pos++;
      else throw this.err(c2,"after object key");
      this.skipWs();
      const c3=this.peek();
      if(c3<0) throw new Error("EOF");
      if(c3===0x7d) throw new Error("missing value after object key");
      if(allowed&&key==="node_results") this.consumeNodeResults();
      else this.consumeValue();
      this.skipWs();
      const c4=this.peek();
      if(c4<0) throw new Error("unexpected end of JSON input");
      if(c4===0x2c) this.afterComma();
      else if(c4===0x7d){this.pos++;return;}
      else throw this.err(c4,"after object key:value pair");
    }
  }
  consumeNodeResult(){
    const t=this.token();
    if(t.kind==="delim"&&t.delim===0x7b) this.consumeObject(NODE_RESULT_FIELDS);
    else throw new Error("node result must be a JSON object");
  }
  consumeNodeResults(){
    const t=this.token();
    if(!(t.kind==="delim"&&t.delim===0x5b)) throw new Error("node_results must be an array");
    this.skipWs();
    if(this.peek()===0x5d){this.pos++;return;}
    for(;;){
      this.consumeNodeResult();
      this.skipWs();
      const c=this.peek();
      if(c<0) throw new Error("unexpected end of JSON input");
      if(c===0x2c) this.afterComma();
      else if(c===0x5d){this.pos++;return;}
      else throw this.err(c,"after array element");
    }
  }
}
function goValidateObject(data:Buffer):void{
  const dec=new GoDec(data);
  let first;
  try{first=dec.token();}catch(e:any){throw new Error(`malformed JSON: ${e.message}`);}
  if(!(first.kind==="delim"&&first.delim===0x7b)) throw new Error("record must be a JSON object");
  try{dec.consumeObject(RECORD_FIELDS);}catch(e:any){throw new Error(`malformed JSON: ${e.message}`);}
  dec.skipWs();
  if(dec.pos<dec.data.length){
    try{dec.token();}catch(e:any){throw new Error(`trailing data: ${e.message}`);}
    throw new Error("trailing data");
  }
}
// ledgerParseValue reparses an already-validated record, keeping number
// literals verbatim so the typed errors can name them.
function ledgerParseValue(text:string):any{
  let i=0;
  const ws=()=>{while(i<text.length&&isJSONSpace(text.charCodeAt(i)))i++;};
  const str=():string=>{
    const start=i; i++;
    while(i<text.length){ const c=text[i++]; if(c==='"') break; if(c==="\\"){ if(text[i]==="u") i+=5; else i++; } }
    return JSON.parse(text.slice(start,i));
  };
  const val=():any=>{
    ws();
    const c=text[i];
    if(c==="{"){ i++; const o:AnyObj={}; ws(); if(text[i]==="}"){i++;return o;} for(;;){ ws(); const k=str(); ws(); i++; const v=val(); o[k]=v; ws(); if(text[i]===","){i++;continue;} i++; return o; } }
    if(c==="["){ i++; const a:any[]=[]; ws(); if(text[i]==="]"){i++;return a;} for(;;){ a.push(val()); ws(); if(text[i]===","){i++;continue;} i++; return a; } }
    if(c==='"') return str();
    if(c==="t"){ i+=4; return true; }
    if(c==="f"){ i+=5; return false; }
    if(c==="n"){ i+=4; return null; }
    const m=/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(text.slice(i))!;
    i+=m[0].length; return {__num:m[0]};
  };
  return val();
}
function ledgerKind(v:any):string{
  if(v===null||v===undefined) return "null";
  if(typeof v==="boolean") return "bool";
  if(typeof v==="string") return "string";
  if(Array.isArray(v)) return "array";
  if(v.__num!==undefined) return "number";
  return "object";
}
function ledgerStringField(v:any,path:string):string{
  if(v===undefined||v===null) return "";
  if(typeof v==="string") return v;
  throw new Error(`malformed record: json: cannot unmarshal ${ledgerKind(v)} into Go struct field ${path} of type string`);
}
function buildLedgerRecord(v:any):any{
  let seq=0;
  const seqv=v.seq;
  if(seqv!==undefined&&seqv!==null){
    if(seqv.__num!==undefined){
      const lit=seqv.__num;
      let ok=/^\d+$/.test(lit);
      if(ok){ try{ if(BigInt(lit)>18446744073709551615n) ok=false; }catch{ ok=false; } }
      if(!ok) throw new Error(`malformed record: json: cannot unmarshal number ${lit} into Go struct field Record.seq of type uint64`);
      seq=Number(BigInt(lit));
    } else {
      throw new Error(`malformed record: json: cannot unmarshal ${ledgerKind(seqv)} into Go struct field Record.seq of type uint64`);
    }
  }
  const results:any[]=[];
  const nrv=v.node_results;
  if(nrv!==undefined&&nrv!==null){
    for(let idx=0;idx<nrv.length;idx++){
      const item=nrv[idx];
      if(item===null||typeof item!=="object"||Array.isArray(item)||item.__num!==undefined) throw new Error(`malformed record: json: cannot unmarshal ${ledgerKind(item)} into Go value of type ledger.NodeResult`);
      results.push({node_id:ledgerStringField(item.node_id,`Record.node_results.${idx}.node_id`),status:ledgerStringField(item.status,`Record.node_results.${idx}.status`),reason:ledgerStringField(item.reason,`Record.node_results.${idx}.reason`)});
    }
  }
  return {schema_version:ledgerStringField(v.schema_version,"Record.schema_version"),seq,prev_record_hash:ledgerStringField(v.prev_record_hash,"Record.prev_record_hash"),record_hash:ledgerStringField(v.record_hash,"Record.record_hash"),envelope_sha256:ledgerStringField(v.envelope_sha256,"Record.envelope_sha256"),packet_binding_sha256:ledgerStringField(v.packet_binding_sha256,"Record.packet_binding_sha256"),ownscout_version:ledgerStringField(v.ownscout_version,"Record.ownscout_version"),node_results:results};
}
function decodeLedgerRecord(data:Buffer):any{
  goValidateObject(data);
  return buildLedgerRecord(ledgerParseValue(goDecodeUtf8(data)));
}
function validateLedgerIdentifier(value:any):void{
  if(typeof value!=="string"||value.length<1||value.length>128) throw new Error("must contain 1-128 ASCII identifier characters");
  for(let i=0;i<value.length;i++){
    const c=value.charCodeAt(i);
    const alnum=(c>=0x61&&c<=0x7a)||(c>=0x41&&c<=0x5a)||(c>=0x30&&c<=0x39);
    if(i===0){if(!alnum)throw new Error("must start with an ASCII alphanumeric character");continue;}
    if(!alnum&&c!==0x2e&&c!==0x5f&&c!==0x2d&&c!==0x3a)throw new Error("contains an invalid character");
  }
}
function validateLedgerSHA256(field:string,value:any):void{
  if(typeof value!=="string"||!/^[0-9a-f]{64}$/.test(value)) throw new Error(`${field} must be exactly 64 lowercase hexadecimal characters`);
}
function containsNUL(value:any):boolean{return typeof value==="string"&&value.indexOf("\u0000")>=0;}
function validateLedgerNodeResult(r:any):void{
  try{validateLedgerIdentifier(r&&r.node_id);}catch(e:any){throw new Error(`node_id: ${e.message}`);}
  if(!["evidence_current","failed","blocked"].includes(r&&r.status)) throw new Error(`status ${goQuote(r&&r.status)} is invalid`);
  if(containsNUL(r&&r.node_id)||containsNUL(r&&r.status)||containsNUL(r&&r.reason)) throw new Error("contains a NUL byte");
}
function validateLedgerRecord(r:any,expectedSeq:number,expectedPrev:string):void{
  if(r.schema_version!=="ownscout-ledger-v1") throw new Error('schema_version must be "ownscout-ledger-v1"');
  if(r.seq!==expectedSeq) throw new Error(`seq ${r.seq} does not follow expected sequence ${expectedSeq}`);
  if(r.prev_record_hash!==expectedPrev) throw new Error("prev_record_hash does not match hash chain");
  validateLedgerSHA256("prev_record_hash",r.prev_record_hash);
  validateLedgerSHA256("record_hash",r.record_hash);
  validateLedgerSHA256("envelope_sha256",r.envelope_sha256);
  validateLedgerSHA256("packet_binding_sha256",r.packet_binding_sha256);
  if(!r.ownscout_version) throw new Error("ownscout_version must be non-empty");
  const results=Array.isArray(r.node_results)?r.node_results:[];
  if(!results.length) throw new Error("node_results must be non-empty");
  if(results.length>MAX_NODE_RESULTS) throw new Error(`node_results exceeds ${MAX_NODE_RESULTS} results`);
  if(containsNUL(r.schema_version)||containsNUL(r.prev_record_hash)||containsNUL(r.record_hash)||containsNUL(r.envelope_sha256)||containsNUL(r.packet_binding_sha256)||containsNUL(r.ownscout_version)) throw new Error("record contains a NUL byte");
  const seen=new Set<string>();
  for(let i=0;i<results.length;i++){
    try{validateLedgerNodeResult(results[i]);}catch(e:any){throw new Error(`node_results[${i}]: ${e.message}`);}
    const id=results[i].node_id;
    if(seen.has(id)) throw new Error(`duplicate node_id ${goQuote(id)}`);
    seen.add(id);
  }
}
function isGoSpaceRune(r:number):boolean{
  if(r===0x09||r===0x0a||r===0x0b||r===0x0c||r===0x0d||r===0x20||r===0x85||r===0xa0) return true;
  if(r>0xa0) return /\p{Zs}/u.test(String.fromCodePoint(r));
  return false;
}
function goTrimSpaceBytes(buf:Buffer):Buffer{
  let start=0;
  while(start<buf.length){ const d=goDecodeRune(buf,start); if(d.size===0||!isGoSpaceRune(d.r)) break; start+=d.size; }
  let lastEnd=start, i=start;
  while(i<buf.length){ const d=goDecodeRune(buf,i); const sz=d.size||1; if(!isGoSpaceRune(d.r)) lastEnd=i+sz; i+=sz; }
  return buf.subarray(start,lastEnd);
}
function validateLedger(data:Buffer):{list:any[];tip:string}{
  if(data.length&&data[data.length-1]!==0x0a) throw new LedgerValidationError("nonempty ledger must end with LF");
  const list:any[]=[];
  let start=0;
  while(start<data.length){
    let end=data.indexOf(0x0a,start);
    if(end<0) end=data.length;
    let line=data.subarray(start,end);
    if(line.length&&line[line.length-1]===0x0d) line=line.subarray(0,line.length-1);
    const trimmed=goTrimSpaceBytes(line);
    if(trimmed.length===0) throw new LedgerValidationError("empty or blank line");
    let record:any;
    try{record=decodeLedgerRecord(trimmed);}catch(e:any){throw new LedgerValidationError(`line ${list.length+1}: ${e.message}`);}
    const expectedSeq=list.length+1, expectedPrev=list.length?list[list.length-1].record_hash:zero;
    try{validateLedgerRecord(record,expectedSeq,expectedPrev);}catch(e:any){throw new LedgerValidationError(`line ${list.length+1}: ${e.message}`);}
    if(record.record_hash!==hashRecord(record)) throw new LedgerValidationError(`line ${list.length+1}: record_hash does not match canonical record`);
    list.push(record);
    start=end+1;
  }
  return {list,tip:list.length?list[list.length-1].record_hash:""};
}
function ledgerVerify(pth:string):{list:any[];tip:string}{
  const ledgerPath=path.resolve(pth);
  let st:fs.Stats;
  try{st=fs.lstatSync(ledgerPath);}
  catch(e:any){
    const reason=e&&e.code==="ENOENT"?"no such file or directory":e&&e.code==="EACCES"?"permission denied":String(e&&e.message||e);
    throw new Error(`read ledger "${ledgerPath}": lstat ${ledgerPath}: ${reason}`);
  }
  if(!st.isFile()) throw new Error(`read ledger "${ledgerPath}": not a regular file`);
  let data:Buffer;
  try{data=fs.readFileSync(ledgerPath);}catch(e:any){
    const reason=e&&e.code==="ENOENT"?"no such file or directory":String(e&&e.message||e);
    throw new Error(`read ledger "${ledgerPath}": ${reason}`);
  }
  if(data.length>MAX_LEDGER) throw new Error(`read ledger "${ledgerPath}": ledger exceeds ${MAX_LEDGER} bytes`);
  try{return validateLedger(data);}
  catch(e:any){
    if(e instanceof LedgerValidationError) throw new LedgerValidationError(`validate ledger "${ledgerPath}": ${e.message}`);
    throw e;
  }
}
// ledgerRotate validates the ledger and renames it to "<path>.<tip8>". It
// hardens symlink ancestors like Open, but reads and renames on the live name.
function ledgerRotate(pth:string):{records:number;tip:string;archive:string}{
  const ledgerPath=path.resolve(pth);
  rejectSymlinkAncestors(ledgerPath);
  let st:fs.Stats;
  try{st=fs.lstatSync(ledgerPath);}
  catch(e:any){
    const reason=e&&e.code==="ENOENT"?"no such file or directory":e&&e.code==="EACCES"?"permission denied":String(e&&e.message||e);
    throw new Error(`read ledger "${ledgerPath}": lstat ${ledgerPath}: ${reason}`);
  }
  if(!st.isFile()) throw new Error(`read ledger "${ledgerPath}": not a regular file`);
  let data:Buffer;
  try{data=fs.readFileSync(ledgerPath);}catch(e:any){
    const reason=e&&e.code==="ENOENT"?"no such file or directory":String(e&&e.message||e);
    throw new Error(`read ledger "${ledgerPath}": ${reason}`);
  }
  if(data.length>MAX_LEDGER) throw new Error(`read ledger "${ledgerPath}": ledger exceeds ${MAX_LEDGER} bytes`);
  let result:{list:any[];tip:string};
  try{result=validateLedger(data);}
  catch(e:any){
    if(e instanceof LedgerValidationError) throw new LedgerValidationError(`validate ledger "${ledgerPath}": ${e.message}`);
    throw e;
  }
  if(result.list.length===0) throw new Error(`ledger "${ledgerPath}" is empty`);
  const archivePath=`${ledgerPath}.${result.tip.slice(0,8)}`;
  let exists=false;
  try{fs.lstatSync(archivePath);exists=true;}
  catch(e:any){ if(!(e&&e.code==="ENOENT")) throw new Error(`inspect archive "${archivePath}": ${e&&e.message||e}`); }
  if(exists) throw new Error(`archive "${archivePath}" already exists`);
  try{fs.renameSync(ledgerPath,archivePath);}catch(e:any){throw new Error(`rename ledger "${ledgerPath}": ${e&&e.message||e}`);}
  return {records:result.list.length,tip:result.tip,archive:archivePath};
}
// readBounded mirrors cli.readBounded's messages for a named input kind.
function readBounded(pth:string,kind="packet"):Buffer{
  let data:Buffer;
  try{data=fs.readFileSync(pth);}
  catch(e:any){
    if(e&&e.code==="ENOENT") throw new Error(`${kind} file "${pth}" does not exist`);
    if(e&&e.code==="EISDIR") throw new Error(`read ${kind} "${pth}": read ${pth}: is a directory`);
    throw new Error(`open ${kind} "${pth}": ${e&&e.message||e}`);
  }
  if(data.length>MAX_INPUT) throw new Error(`${kind} exceeds ${MAX_INPUT} byte input limit`);
  return data;
}
function evidenceIssues(r:any):string[]{
  return r.results.filter((x:any)=>x.status!=="verified").map((x:any)=>`evidence "${x.evidence_id}" ("${x.path}"): ${x.message||"status: "+x.status}`);
}
function ledgerCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"a ledger subcommand is required","ownscout ledger --help");
  if(a[0]==="rotate")return ledgerRotateCmd(a.slice(1),out);
  if(a[0]!=="verify")return failure(out,`unknown ledger subcommand '${a[0]}'`,"ownscout ledger --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--ledger"]));if(q.err)return failure(out,q.err,"ownscout ledger verify --help",q.j);
  if(!q.f["--ledger"])return failure(out,"missing required --ledger value","ownscout ledger verify --help",q.j);
  let s:any;
  try{s=ledgerVerify(q.f["--ledger"]);}
  catch(e:any){
    if(e instanceof LedgerValidationError)return emit(out,q.j,{command:"ledger verify",ok:false,summary:"ledger verification failed",details:[e.message],next_action:"Repair the ledger, then run ledger verification again."},1);
    return emit(out,q.j,{command:"ledger verify",ok:false,summary:"ledger could not be read",details:[e.message],next_action:"Provide a readable ledger file with --ledger <file>."},2);
  }
  const tip=s.tip||"none";
  return emit(out,q.j,{command:"ledger verify",ok:true,summary:"ledger is intact",details:[`${s.list.length} record(s), tip ${tip}`],next_action:"The ledger chain is intact."},0);
}
function ledgerRotateCmd(a:string[],out:any):number{
  const q=parseFlags(a,new Set(["--ledger"]));if(q.err)return failure(out,q.err,"ownscout ledger rotate --help",q.j);
  if(!q.f["--ledger"])return failure(out,"missing required --ledger value","ownscout ledger rotate --help",q.j);
  let r:any;
  try{r=ledgerRotate(q.f["--ledger"]);}
  catch(e:any){
    if(e instanceof LedgerValidationError)return emit(out,q.j,{command:"ledger rotate",ok:false,summary:"ledger verification failed",details:[e.message],next_action:"Repair the ledger before rotating; a broken chain keeps its live name."},1);
    return emit(out,q.j,{command:"ledger rotate",ok:false,summary:"ledger could not be rotated",details:[e.message],next_action:"Provide a readable, non-empty ledger file with --ledger <file>."},2);
  }
  return emit(out,q.j,{command:"ledger rotate",ok:true,summary:"ledger rotated",details:[`${r.records} record(s), tip ${r.tip}`,"archived to "+r.archive],next_action:"The next node verify starts a fresh chain; audit the archive with ownscout ledger verify."},0);
}
function nodeBindCmd(a:string[],out:any):number{
  const q=parseFlags(a,new Set(["--packet"]));
  if(q.err)return failure(out,q.err,"ownscout node bind --help",q.j);
  if(!q.f["--packet"])return failure(out,"missing required --packet value","ownscout node bind --help",q.j);
  let data:Buffer;
  try{data=readBounded(q.f["--packet"]);}
  catch{return emit(out,q.j,{command:"node bind",ok:false,summary:"packet could not be loaded",details:["packet input could not be read"],next_action:"Provide a readable packet file with --packet <file>."},2);}
  const d=decodePacketValid(data);
  if(d.violations.length){
    if(d.violations.length===1&&d.violations[0].rule==="packet_decode")return emit(out,q.j,{command:"node bind",ok:false,summary:"packet could not be decoded",details:["strict packet decoding failed"],next_action:"Provide one valid packet-v1 JSON object with --packet <file>."},2);
    return emit(out,q.j,{command:"node bind",ok:false,summary:`packet contract failed (${d.violations.length} violation(s))`,details:details(d.violations),next_action:"Fix the packet contract, then run node binding again."},1);
  }
  return emit(out,q.j,{command:"node bind",ok:true,summary:"packet binding computed",details:[binding(d.packet)],next_action:"Use this as packet_binding_sha256 in a node-envelope-v1 document."},0);
}

type Result={command:string;ok:boolean;summary:string;details:string[];next_action:string};
function emit(out:NodeJS.WritableStream,json:boolean,d:Result,code:number):number{if(json)out.write(goJson(d)+"\n");else{out.write(`${d.ok?"OK":"ERROR"}: ${d.summary}\n`);for(const x of d.details)out.write(`  - ${x}\n`);out.write(`Next action: ${d.next_action}\n`);}return code;}
function failure(out:any,msg:string,next:string,json=false){return json?emit(out,true,{command:"usage",ok:false,summary:msg,details:["usage: "+next],next_action:"Run '"+next+"'."},2):(out.write(`error: ${msg}\nNext action: run '${next}'.\n`),2);}
function hasHelp(a:string[]){return a.includes("--help")||a.includes("-h");}
function parseFlags(a:string[],allowed:Set<string>,switches:Set<string>=new Set()):{f:AnyObj;j:boolean;err?:string}{const f:any={},j=a.includes("--json");for(let i=0;i<a.length;i++){const x=a[i];if(x==="--json")continue;if(switches.has(x)){f[x]="true";continue;}if(!allowed.has(x))return{f,j,err:`unknown flag or argument '${x}'`};if(i+1>=a.length||a[i+1].startsWith("-"))return{f,j,err:`${x} requires a value`};f[x]=a[++i];}return{f,j};}
function help(a:string[],out:any){
  let t=rootUsage;
  if(a[0]==="doctor")t="Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments.";
  else if(a[0]==="version")t="Usage: ownscout version\n\nPrints the OwnScout version.";
  else if(a[0]==="contract")t="Usage: ownscout contract validate --packet <file> [--json]\n\nValidates packet structure and outcome rules.";
  else if(a[0]==="evidence")t="Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nVerifies packet evidence spans against a local repository.";
  else if(a[0]==="ledger")t="Usage: ownscout ledger verify --ledger <file> [--json]\n       ownscout ledger rotate --ledger <file> [--json]\n\nAudits or archives an append-only ledger.";
  else if(a[0]==="node")t="Usage: ownscout node bind --packet <file> [--json]\n       ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]\n\nBinds packets or verifies a node-envelope-v1 graph against fresh repository evidence.";
  if(a[1]==="validate"&&a[0]==="contract")t="Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.";
  if(a[1]==="verify"&&a[0]==="evidence")t="Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nValidates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun.";
  if(a[1]==="verify"&&a[0]==="ledger")t="Usage: ownscout ledger verify --ledger <file> [--json]\n\nReplays the SHA-256 ledger chain without opening or modifying it.\n\nNext action: provide --ledger with a readable ledger file.";
  if(a[1]==="bind"&&a[0]==="node")t="Usage: ownscout node bind --packet <file> [--json]\n\nComputes the canonical packet binding for a strictly decoded packet.\n\nNext action: provide --packet with a readable packet file.";
  if(a[1]==="verify"&&a[0]==="node")t="Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]\n\nStrictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once. With --relocate, failed evidence details include matching locations when available.\n\nNext action: provide all four paths and rerun.";
  out.write(t+"\n");return 0;
}

function run(args:string[],out:any):number {
  if(!args.length){out.write("error: a command is required\n\n"+rootUsage+"\n\nNext action: run 'ownscout --help'.\n");return 2;}
  if(hasHelp(args))return help(args,out);
  if(args[0]==="doctor"){if(args.length!==1)return failure(out,"doctor does not accept arguments","ownscout doctor --help");out.write("OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.\n");return 0;}
  if(args[0]==="version"){if(args.length!==1)return failure(out,"version does not accept arguments","ownscout version --help");out.write("ownscout "+VERSION+"\n");return 0;}
  if(args[0]==="contract")return contractCmd(args.slice(1),out);
  if(args[0]==="evidence")return evidenceCmd(args.slice(1),out);
  if(args[0]==="ledger")return ledgerCmd(args.slice(1),out);
  if(args[0]==="node")return nodeCmd(args.slice(1),out);
  return failure(out,`unknown command '${args[0]}'`,"ownscout --help");
}
function contractCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"a contract subcommand is required","ownscout contract --help");
  if(a[0]!=="validate")return failure(out,`unknown contract subcommand '${a[0]}'`,"ownscout contract --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--packet"]));if(q.err)return failure(out,q.err,"ownscout contract validate --help",q.j);if(!q.f["--packet"])return failure(out,"missing required --packet <file>","ownscout contract validate --help",q.j);
  let d:any;try{d=loadPacket(q.f["--packet"]);}catch(e:any){return emit(out,q.j,{command:"contract validate",ok:false,summary:"packet could not be loaded",details:[e.message],next_action:"Provide a readable JSON packet with --packet <file>."},2);}
  if(isDecodeFailure(d.violations))return emit(out,q.j,{command:"contract validate",ok:false,summary:"packet could not be decoded",details:["strict packet decoding failed"],next_action:"Provide one valid packet-v1 JSON object with --packet <file>."},2);
  if(d.violations.length)return emit(out,q.j,{command:"contract validate",ok:false,summary:`packet is invalid (${d.violations.length} violation(s))`,details:details(d.violations),next_action:"Fix the listed packet fields, then run contract validation again."},1);
  const p:any=d.packet;
  return emit(out,q.j,{command:"contract validate",ok:true,summary:"packet is valid",details:["outcome: "+p.outcome,"default action: "+actions[p.outcome]],next_action:"Run evidence verification before consuming this packet."},0);
}
function evidenceCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"an evidence subcommand is required","ownscout evidence --help");
  if(a[0]!=="verify")return failure(out,`unknown evidence subcommand '${a[0]}'`,"ownscout evidence --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--repo","--packet"]),new Set(["--relocate"]));if(q.err)return failure(out,q.err,"ownscout evidence verify --help",q.j);if(!q.f["--repo"]||!q.f["--packet"])return failure(out,"both --repo <dir> and --packet <file> are required","ownscout evidence verify --help",q.j);
  let d:any;try{d=loadPacket(q.f["--packet"]);}catch(e:any){return emit(out,q.j,{command:"evidence verify",ok:false,summary:"packet could not be loaded",details:[e.message],next_action:"Provide a readable JSON packet with --packet <file>."},2);}
  if(isDecodeFailure(d.violations))return emit(out,q.j,{command:"evidence verify",ok:false,summary:"packet could not be decoded",details:["strict packet decoding failed"],next_action:"Provide one valid packet-v1 JSON object with --packet <file>."},2);
  if(d.violations.length)return emit(out,q.j,{command:"evidence verify",ok:false,summary:"packet is invalid",details:details(d.violations),next_action:"Fix the packet contract, then verify evidence again."},1);
  let r:any;try{r=verifyEvidence(q.f["--repo"],d.packet,{relocate:q.f["--relocate"]==="true"});}catch(e:any){return emit(out,q.j,{command:"evidence verify",ok:false,summary:"repository could not be checked",details:[e.message],next_action:"Provide a readable repository directory with --repo <dir>."},2);}
  const issues=evidenceIssues(r);
  if(!r.ok)return emit(out,q.j,{command:"evidence verify",ok:false,summary:`evidence verification failed (${issues.length} issue(s))`,details:issues,next_action:"Refresh or correct the listed evidence, then verify again."},1);
  return emit(out,q.j,{command:"evidence verify",ok:true,summary:"evidence verified",details:[`verified ${r.verified} evidence span(s)`],next_action:"The packet is ready for its declared default action."},0);
}
function nodeCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"a node subcommand is required","ownscout node --help");
  if(a[0]==="bind")return nodeBindCmd(a.slice(1),out);
  if(a[0]!=="verify")return failure(out,`unknown node subcommand '${a[0]}'`,"ownscout node --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--repo","--packet","--envelope","--ledger"]),new Set(["--relocate"]));if(q.err)return failure(out,q.err,"ownscout node verify --help",q.j);
  for(const x of ["--repo","--packet","--envelope","--ledger"])if(!q.f[x])return failure(out,`missing required ${x} value`,"ownscout node verify --help",q.j);
  const relocate=q.f["--relocate"]==="true";
  let p:any,env:any,ord:number[],b:string;try{const pd=fs.readFileSync(q.f["--packet"]);const ed=fs.readFileSync(q.f["--envelope"]);const d=decodePacketValid(pd);if(d.violations.length){if(d.violations.length===1&&d.violations[0].rule==="packet_decode")return emit(out,q.j,{command:"node verify",ok:false,summary:"packet could not be decoded",details:["strict packet decoding failed"],next_action:"Provide one valid packet-v1 JSON object with --packet <file>."},2);return emit(out,q.j,{command:"node verify",ok:false,summary:`packet contract failed (${d.violations.length} violation(s))`,details:details(d.violations),next_action:"Fix the packet contract, then run node verification again."},2);}p=d.packet;try{env=parseEnvelope(ed);}catch(e:any){throw new Error("ENVELOPE_PARSE: "+e.message);}b=binding(p);ord=validateEnvelope(env,p,b);
    let ledger:any;try{ledger=ledgerOpen(q.f["--ledger"],q.f["--repo"]);}catch(e:any){ if(e.message==="ledger exceeds 1048576 bytes")return emit(out,q.j,{command:"node verify",ok:false,summary:"ledger is full",details:[e.message],next_action:"Archive the full ledger aside (mv) and rerun to start a fresh chain; audit archives with ownscout ledger verify."},2); return emit(out,q.j,{command:"node verify",ok:false,summary:"ledger could not be opened",details:["ledger open failed"],next_action:"Provide a writable ledger path outside the repository and try again."},2); }const report=verifyEvidence(q.f["--repo"],p,{relocate});const ev=evaluate(env,p,b,report,ord);const results=ev.results.map((x:any)=>({node_id:x.node_id,status:x.status,...(x.reason?{reason:x.reason}:{})}));try{appendLedger(ledger,b,sha256(ed),results);}catch(e:any){if(e.message==="ledger exceeds 1048576 bytes")return emit(out,q.j,{command:"node verify",ok:false,summary:"ledger is full",details:[e.message],next_action:"Archive the full ledger aside (mv) and rerun to start a fresh chain; audit archives with ownscout ledger verify."},2);return emit(out,q.j,{command:"node verify",ok:false,summary:"ledger append failed",details:["node results could not be appended"],next_action:"Check the ledger and try again; no result was consumed."},2);}
    let det=ev.results.map((x:any)=>`node "${x.node_id}": ${x.status}${x.reason?" ("+x.reason+")":""}`);
    if(!ev.ok&&relocate)det=det.concat(evidenceIssues(report));
    return emit(out,q.j,{command:"node verify",ok:ev.ok,summary:ev.ok?"all nodes are evidence_current":"node evaluation failed",details:det,next_action:ev.ok?"The node envelope is recorded and ready for its declared workflow.":"Refresh or correct the failed evidence, then run node verification again."},ev.ok?0:1);
  }catch(e:any){let summary="envelope validation failed",detail="node-envelope-v1 validation failed",next="Fix the envelope binding or graph, then run node verification again.";if(String(e.message).startsWith("ENVELOPE_PARSE")){summary="envelope could not be parsed";detail="strict envelope parsing failed";next="Provide one valid node-envelope-v1 JSON object with --envelope <file>."}return emit(out,q.j,{command:"node verify",ok:false,summary,details:[detail],next_action:next},2);}
}
if (import.meta.url === `file://${process.argv[1]}`) {
  process.exitCode = run(process.argv.slice(2), process.stdout);
}
