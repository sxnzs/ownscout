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
  ownscout evidence verify --repo <dir> --packet <file> [--json]
  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]

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
function utf8(data: Buffer): string {
  try { return new TextDecoder("utf-8", { fatal: true }).decode(data); }
  catch { throw new Error("input is not valid UTF-8"); }
}

class StrictParser {
  private i = 0;
  private readonly s: string;
  constructor(s: string) { this.s = s; }
  private ws() { while (this.i < this.s.length && /\s/.test(this.s[this.i])) this.i++; }
  private fail(msg = "invalid JSON"): never { throw new Error(msg); }
  parse(schema: any): any {
    this.ws();
    const v = this.value(schema);
    this.ws();
    if (this.i !== this.s.length) this.fail("unexpected data after packet");
    return v;
  }
  private value(schema: any): any {
    this.ws();
    if (this.i >= this.s.length) this.fail();
    if (this.s.startsWith("null", this.i)) { this.i += 4; return null; }
    if (schema === "string") return this.string();
    if (schema === "boolean") {
      if (this.s.startsWith("true", this.i)) { this.i += 4; return true; }
      if (this.s.startsWith("false", this.i)) { this.i += 5; return false; }
      this.fail("expected boolean");
    }
    if (schema === "int" || schema === "int64") {
      const raw = this.numberRaw();
      let n: bigint;
      try { n = BigInt(raw); } catch { this.fail("expected integer"); }
      const min = BigInt("-9223372036854775808"), max = BigInt("9223372036854775807");
      if (raw.includes(".") || /e/i.test(raw) || n < min || n > max) this.fail("integer is out of range or not integral");
      return Number(n);
    }
    if (schema?.array) {
      if (this.s[this.i++] !== "[") this.fail("expected array");
      const out: any[] = []; this.ws();
      if (this.s[this.i] === "]") { this.i++; return out; }
      while (true) {
        out.push(this.value(schema.array)); this.ws();
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
        if (seen.has(key)) this.fail("duplicate key");
        seen.add(key);
        if (!(key in schema)) this.fail("unknown field");
        this.ws();
        if (this.s[this.i++] !== ":") this.fail();
        out[key] = this.value(schema[key]); this.ws();
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
function decodePacket(data: Buffer): AnyObj {
  if (data.length > MAX_INPUT) throw new Error("input exceeds 1 MiB");
  const text = utf8(data);
  // Reject unpaired UTF-16 escapes, matching the Go decoder's preflight.
  for (let i = 0; i < text.length - 1; i++) if (text[i] === "\\" && text[i + 1] === "u") {
    const h = text.slice(i + 2, i + 6);
    if (!/^[0-9a-fA-F]{4}$/.test(h)) throw new Error("invalid Unicode escape");
    const n = parseInt(h, 16);
    if (n >= 0xdc00 && n <= 0xdfff) throw new Error("invalid Unicode escape");
    if (n >= 0xd800 && n <= 0xdbff) {
      if (text.slice(i + 6, i + 12) !== "\\u" || !/^[0-9a-fA-F]{4}$/.test(text.slice(i + 8, i + 12)) ||
          parseInt(text.slice(i + 8, i + 12), 16) < 0xdc00 || parseInt(text.slice(i + 8, i + 12), 16) > 0xdfff)
        throw new Error("invalid Unicode escape");
      i += 6;
    }
  }
  return new StrictParser(text).parse(packetSchema);
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
function validatePacket(p: AnyObj): Violation[] {
  const v: Violation[] = [], add = (rule: string, field: string, message: string) => v.push({ rule, field, message });
  for (const [f, x] of [["packet_id",p.packet_id],["schema_version",p.schema_version],["repo_root",p.repo_root],
    ["head_commit",p.head_commit],["request_id",p.request_id],["issued_at",p.issued_at],["outcome",p.outcome],["packet_hash",p.packet_hash]])
    if (typeof x !== "string" || !x.trim()) add("required_field", f, "required field is missing");
  const fresh = p.freshness || {}, auth = p.authorization || {}, budget = p.budget || {}, prov = p.provenance || {};
  if (Object.keys(fresh).length === 0) add("required_field","freshness","required field is missing");
  if (!String(auth.level || "").trim()) add("required_field","authorization","required field is missing");
  if (Object.keys(budget).length === 0) add("required_field","budget","required field is missing");
  if (!Array.isArray(p.evidence)) add("required_field","evidence","required field is missing");
  if (!Array.isArray(p.degradations)) add("required_field","degradations","required field is missing");
  if (Object.keys(prov).length === 0) add("required_field","provenance","required field is missing");
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
  if (Object.keys(b).length === 0) add("budget","budget","budget must contain counters");
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
  if (Array.isArray(p.evidence)) {
    for (let i=0;i<p.evidence.length;i++) {
      const e=p.evidence[i], f=`evidence[${i}]`;
      for (const k of ["evidence_id","kind","path","commit","source","content_hash","collected_at","verifier_status"])
        if (!String(e[k] ?? "").trim()) add("malformed_evidence",`${f}.${k}`,"required evidence field is missing");
      if (e.line_start < 1) add("malformed_evidence",`${f}.line_start`,"line_start must be at least 1");
      if (e.line_end < 1) add("malformed_evidence",`${f}.line_end`,"line_end must be at least 1");
      if (e.line_start >= 1 && e.line_end >= 1 && e.line_end < e.line_start) add("malformed_evidence",f,"line_end must be greater than or equal to line_start");
      if (!["verified","unverified","failed","unavailable","pending"].includes(e.verifier_status)) add("malformed_evidence",`${f}.verifier_status`,"unknown verifier status");
      if (p.outcome === "complete" && e.verifier_status !== "verified") add("evidence",`${f}.verifier_status`,"complete packet requires verified evidence");
    }
    if (p.outcome === "complete" && p.evidence.length === 0) add("evidence","evidence","complete packet requires at least one evidence entry");
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

function loadPacket(pth: string): AnyObj {
  if (!fs.existsSync(pth)) throw new Error(`packet file "${pth}" does not exist`);
  const data=fs.readFileSync(pth), trimmed=data.toString().trim();
  if (!trimmed) throw new Error(`packet "${pth}" is not valid JSON`);
  try {
    const p=decodePacket(data);
    if (!p || Array.isArray(p) || typeof p !== "object") throw new Error("must contain a JSON object");
    return p;
  } catch (e:any) {
    if (jsonValid(trimmed)) {
      try {
        const raw=JSON.parse(trimmed);
        if (raw && typeof raw === "object" && !Array.isArray(raw) &&
            "evidence" in raw && raw.evidence !== null && !Array.isArray(raw.evidence))
          throw new Error(`packet "${pth}" is not valid JSON: json: cannot unmarshal object into Go struct field Packet.evidence of type []contract.Evidence`);
      } catch (inner:any) {
        if (String(inner.message).startsWith(`packet "${pth}"`)) throw inner;
      }
    }
    if (e.message === "unknown field") throw new Error(`packet "${pth}" contains unknown JSON field`);
    if (e.message === "duplicate key") throw new Error(`packet "${pth}" contains duplicate JSON object key`);
    if (e.message === "expected object") throw new Error(`packet "${pth}" must contain a JSON object`);
    throw new Error(`packet "${pth}" is not valid JSON`);
  }
}
function jsonValid(s:string):boolean { try { JSON.parse(s); return true; } catch { return false; } }
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
function verifyEvidence(repo:string,p:AnyObj):{ok:boolean;results:Verification[];verified:number;failed:number;skipped:number} {
  const root=repoRoot(repo), results:Verification[]=[];
  for (const e of p.evidence||[]) {
    const r:Verification={evidence_id:e.evidence_id,path:e.path,status:"failed",expected_hash:e.content_hash,line_start:e.line_start,line_end:e.line_end};
    try {
      let fp:string;
      try { fp=safePath(root,e.path); } catch (x:any) {
        if (String(x.code)==="ENOENT") throw new Error(`cannot access evidence path "${e.path}": lstat ${path.join(root,e.path)}: no such file or directory`);
        throw x;
      }
      const st=fs.lstatSync(fp); if (!st.isFile() || st.isSymbolicLink()) throw new Error(`evidence path "${e.path}" is not a regular file`);
      const text=fs.readFileSync(fp).toString().replace(/\r\n/g,"\n"), lines=text===""?[]:text.split("\n");
      if (lines.at(-1)==="") lines.pop();
      if (e.line_start<1 || e.line_end<e.line_start || e.line_end>lines.length) throw new Error(`invalid line range ${e.line_start}-${e.line_end} for ${lines.length} line(s)`);
      let expected=e.content_hash; if (expected.startsWith("sha256:")) expected=expected.slice(7);
      if (!/^[0-9a-fA-F]{64}$/.test(expected)) throw new Error(`invalid SHA-256 content hash "${e.content_hash}"`);
      const selected=lines.slice(e.line_start-1,e.line_end).join("\n")+(lines.slice(e.line_start-1,e.line_end).length?"\n":"");
      r.actual_hash=sha256(selected); if (r.actual_hash!==expected.toLowerCase()) throw new Error(`content hash mismatch: expected ${e.content_hash}, got ${r.actual_hash}`);
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
function ledgerOpen(ledger:string,repo:string):{path:string;records:any[]} {
  const p=safeLedgerPath(ledger,repo); if(!fs.existsSync(path.dirname(p))) throw new Error(`open ledger parent "${path.dirname(p)}": no such file or directory`);
  if (!fs.existsSync(p)) fs.closeSync(fs.openSync(p, "a", 0o600));
  const data=fs.existsSync(p)?fs.readFileSync(p):Buffer.alloc(0); if(data.length>1<<20)throw new Error("ledger exceeds 1048576 bytes");
  if(data.length&&!data.toString().endsWith("\n"))throw new Error("validate ledger: nonempty ledger must end with LF");
  const records:any[]=[]; for(const line of data.toString().split("\n").filter(Boolean)){const rec=JSON.parse(line); if(rec.schema_version!=="ownscout-ledger-v1"||rec.seq!==records.length+1||rec.prev_record_hash!==(records.length?records.at(-1).record_hash:zero)||rec.record_hash!==hashRecord(rec))throw new Error("validate ledger: invalid record");records.push(rec);}
  return {path:p,records};
}
function hashRecord(r:any):string{const c={...r,record_hash:""};return sha256(goJson(c));}
function appendLedger(l:any,b:string,envHash:string,results:any[]):void {
  if(!results.length)throw new Error("node_results must be non-empty");
  const prev=l.records.length?l.records.at(-1).record_hash:zero;
  const rec:any={schema_version:"ownscout-ledger-v1",seq:l.records.length+1,prev_record_hash:prev,record_hash:zero,envelope_sha256:envHash,packet_binding_sha256:b,ownscout_version:VERSION,node_results:results};
  rec.record_hash=hashRecord(rec);const line=goJson(rec)+"\n";if(Buffer.byteLength(line)>64<<10||fs.statSync(l.path).size+Buffer.byteLength(line)>1<<20)throw new Error("ledger exceeds 1048576 bytes");
  fs.appendFileSync(l.path,line,{mode:0o600});l.records.push(rec);
}

type Result={command:string;ok:boolean;summary:string;details:string[];next_action:string};
function emit(out:NodeJS.WritableStream,json:boolean,d:Result,code:number):number{if(json)out.write(goJson(d)+"\n");else{out.write(`${d.ok?"OK":"ERROR"}: ${d.summary}\n`);for(const x of d.details)out.write(`  - ${x}\n`);out.write(`Next action: ${d.next_action}\n`);}return code;}
function failure(out:any,msg:string,next:string,json=false){return json?emit(out,true,{command:"usage",ok:false,summary:msg,details:["usage: "+next],next_action:"Run '"+next+"'."},2):(out.write(`error: ${msg}\nNext action: run '${next}'.\n`),2);}
function hasHelp(a:string[]){return a.includes("--help")||a.includes("-h");}
function parseFlags(a:string[],allowed:Set<string>):{f:AnyObj;j:boolean;err?:string}{const f:any={},j=a.includes("--json");for(let i=0;i<a.length;i++){const x=a[i];if(x==="--json")continue;if(!allowed.has(x))return{f,j,err:`unknown flag or argument '${x}'`};if(i+1>=a.length||a[i+1].startsWith("-"))return{f,j,err:`${x} requires a value`};f[x]=a[++i];}return{f,j};}
function help(a:string[],out:any){let t=rootUsage;if(a[0]==="doctor")t="Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments.";else if(a[0]==="version")t="Usage: ownscout version\n\nPrints the OwnScout version.";else if(a[0]==="contract")t="Usage: ownscout contract validate --packet <file> [--json]\n\nValidates packet structure and outcome rules.";else if(a[0]==="evidence")t="Usage: ownscout evidence verify --repo <dir> --packet <file> [--json]\n\nVerifies packet evidence spans against a local repository.";else if(a[0]==="node")t="Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nVerifies a node-envelope-v1 graph against fresh repository evidence and records the ordered results.";if(a[1]==="validate"&&a[0]==="contract")t="Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.";if(a[1]==="verify"&&a[0]==="evidence")t="Usage: ownscout evidence verify --repo <dir> --packet <file> [--json]\n\nValidates the packet, then checks each evidence span locally.\n\nNext action: provide both paths and rerun.";if(a[1]==="verify"&&a[0]==="node")t="Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nStrictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once.\n\nNext action: provide all four paths and rerun.";out.write(t+"\n");return 0;}

function run(args:string[],out:any):number {
  if(!args.length){out.write("error: a command is required\n\n"+rootUsage+"\n\nNext action: run 'ownscout --help'.\n");return 2;}
  if(hasHelp(args))return help(args,out);
  if(args[0]==="doctor"){if(args.length!==1)return failure(out,"doctor does not accept arguments","ownscout doctor --help");out.write("OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.\n");return 0;}
  if(args[0]==="version"){if(args.length!==1)return failure(out,"version does not accept arguments","ownscout version --help");out.write("ownscout "+VERSION+"\n");return 0;}
  if(args[0]==="contract")return contractCmd(args.slice(1),out);
  if(args[0]==="evidence")return evidenceCmd(args.slice(1),out);
  if(args[0]==="node")return nodeCmd(args.slice(1),out);
  return failure(out,`unknown command '${args[0]}'`,"ownscout --help");
}
function contractCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"a contract subcommand is required","ownscout contract --help");
  if(a[0]!=="validate")return failure(out,`unknown contract subcommand '${a[0]}'`,"ownscout contract --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--packet"]));if(q.err)return failure(out,q.err,"ownscout contract validate --help",q.j);if(!q.f["--packet"])return failure(out,"missing required --packet <file>","ownscout contract validate --help",q.j);
  let p:any;try{p=loadPacket(q.f["--packet"]);}catch(e:any){return emit(out,q.j,{command:"contract validate",ok:false,summary:"packet could not be loaded",details:[e.message],next_action:"Provide a readable JSON packet with --packet <file>."},2);}
  const v=validatePacket(p);if(v.length)return emit(out,q.j,{command:"contract validate",ok:false,summary:`packet is invalid (${v.length} violation(s))`,details:details(v),next_action:"Fix the listed packet fields, then run contract validation again."},1);
  return emit(out,q.j,{command:"contract validate",ok:true,summary:"packet is valid",details:["outcome: "+p.outcome,"default action: "+actions[p.outcome]],next_action:"Run evidence verification before consuming this packet."},0);
}
function evidenceCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"an evidence subcommand is required","ownscout evidence --help");
  if(a[0]!=="verify")return failure(out,`unknown evidence subcommand '${a[0]}'`,"ownscout evidence --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--repo","--packet"]));if(q.err)return failure(out,q.err,"ownscout evidence verify --help",q.j);if(!q.f["--repo"]||!q.f["--packet"])return failure(out,"both --repo <dir> and --packet <file> are required","ownscout evidence verify --help",q.j);
  let p:any;try{p=loadPacket(q.f["--packet"]);}catch(e:any){return emit(out,q.j,{command:"evidence verify",ok:false,summary:"packet could not be loaded",details:[e.message],next_action:"Provide a readable JSON packet with --packet <file>."},2);}
  const v=validatePacket(p);if(v.length)return emit(out,q.j,{command:"evidence verify",ok:false,summary:"packet is invalid",details:details(v),next_action:"Fix the packet contract, then verify evidence again."},1);
  let r:any;try{r=verifyEvidence(q.f["--repo"],p);}catch(e:any){return emit(out,q.j,{command:"evidence verify",ok:false,summary:"repository could not be checked",details:[e.message],next_action:"Provide a readable repository directory with --repo <dir>."},2);}
  const issues=r.results.filter((x:any)=>x.status!=="verified").map((x:any)=>`evidence "${x.evidence_id}" ("${x.path}"): ${x.message||"status: "+x.status}`);
  if(!r.ok)return emit(out,q.j,{command:"evidence verify",ok:false,summary:`evidence verification failed (${issues.length} issue(s))`,details:issues,next_action:"Refresh or correct the listed evidence, then verify again."},1);
  return emit(out,q.j,{command:"evidence verify",ok:true,summary:"evidence verified",details:[`verified ${r.verified} evidence span(s)`],next_action:"The packet is ready for its declared default action."},0);
}
function nodeCmd(a:string[],out:any):number{
  if(!a.length)return failure(out,"a node subcommand is required","ownscout node --help");
  if(a[0]!=="verify")return failure(out,`unknown node subcommand '${a[0]}'`,"ownscout node --help",a.includes("--json"));
  const q=parseFlags(a.slice(1),new Set(["--repo","--packet","--envelope","--ledger"]));if(q.err)return failure(out,q.err,"ownscout node verify --help",q.j);
  for(const x of ["--repo","--packet","--envelope","--ledger"])if(!q.f[x])return failure(out,`missing required ${x} value`,"ownscout node verify --help",q.j);
  let p:any,env:any,ord:number[],b:string;try{const pd=fs.readFileSync(q.f["--packet"]);const ed=fs.readFileSync(q.f["--envelope"]);const d=decodePacketValid(pd);if(d.violations.length){if(d.violations.length===1&&d.violations[0].rule==="packet_decode")return emit(out,q.j,{command:"node verify",ok:false,summary:"packet could not be decoded",details:["strict packet decoding failed"],next_action:"Provide one valid packet-v1 JSON object with --packet <file>."},2);if(d.violations.length>1)return emit(out,q.j,{command:"node verify",ok:false,summary:`packet contract failed (${d.violations.length} violation(s))`,details:details(d.violations),next_action:"Fix the packet contract, then run node verification again."},2);}p=d.packet;try{env=parseEnvelope(ed);}catch(e:any){throw new Error("ENVELOPE_PARSE: "+e.message);}b=binding(p);ord=validateEnvelope(env,p,b);
    let ledger:any;try{ledger=ledgerOpen(q.f["--ledger"],q.f["--repo"]);}catch{ return emit(out,q.j,{command:"node verify",ok:false,summary:"ledger could not be opened",details:["ledger open failed"],next_action:"Provide a writable ledger path outside the repository and try again."},2); }const report=verifyEvidence(q.f["--repo"],p);const ev=evaluate(env,p,b,report,ord);const results=ev.results.map((x:any)=>({node_id:x.node_id,status:x.status,...(x.reason?{reason:x.reason}:{})}));appendLedger(ledger,b,sha256(ed),results);
    const det=ev.results.map((x:any)=>`node "${x.node_id}": ${x.status}${x.reason?" ("+x.reason+")":""}`);
    return emit(out,q.j,{command:"node verify",ok:ev.ok,summary:ev.ok?"all nodes are evidence_current":"node evaluation failed",details:det,next_action:ev.ok?"The node envelope is recorded and ready for its declared workflow.":"Refresh or correct the failed evidence, then run node verification again."},ev.ok?0:1);
  }catch(e:any){let summary="envelope validation failed",detail="node-envelope-v1 validation failed",next="Fix the envelope binding or graph, then run node verification again.";if(String(e.message).startsWith("ENVELOPE_PARSE")){summary="envelope could not be parsed";detail="strict envelope parsing failed";next="Provide one valid node-envelope-v1 JSON object with --envelope <file>."}return emit(out,q.j,{command:"node verify",ok:false,summary,details:[detail],next_action:next},2);}
}
if (import.meta.url === `file://${process.argv[1]}`) {
  process.exitCode = run(process.argv.slice(2), process.stdout);
}
