import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  existsSync,
  lstatSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";

const root = path.resolve(import.meta.dirname, "../..");
const bin = path.join(root, "ports/ts/bin/ownscout");
const fixtures = path.join(root, "spec/parity/fixtures");
const edge = path.join(fixtures, "edge");
const repoFixture = path.join(fixtures, "repo");

type JsonObject = { [key: string]: any };

function run(...args: string[]) {
  const result = spawnSync(bin, args, { cwd: root, encoding: "utf8" });
  return { code: result.status ?? -1, stdout: result.stdout, stderr: result.stderr };
}

function jsonFile(name: string): JsonObject {
  return JSON.parse(readFileSync(name, "utf8"));
}

function writeJson(dir: string, name: string, value: any): string {
  const file = path.join(dir, name);
  writeFileSync(file, JSON.stringify(value), { mode: 0o600 });
  return file;
}

function tempDir(prefix = "ownscout-ts-"): string {
  return mkdtempSync(path.join(tmpdir(), prefix));
}

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function packetVariant(mutate: (packet: JsonObject) => void): string {
  const dir = tempDir();
  const packet = jsonFile(path.join(fixtures, "packet-valid.json"));
  mutate(packet);
  return writeJson(dir, "packet.json", packet);
}

function envelopeVariant(mutate: (envelope: JsonObject) => void): string {
  const dir = tempDir();
  const envelope = jsonFile(path.join(fixtures, "envelope-valid.json"));
  mutate(envelope);
  return writeJson(dir, "envelope.json", envelope);
}

function assertJsonResult(output: string, command: string, ok: boolean) {
  assert.equal(output.endsWith("\n"), true);
  assert.equal(output.trim().split("\n").length, 1);
  const value = JSON.parse(output);
  assert.deepEqual(Object.keys(value).sort(), ["command", "details", "next_action", "ok", "summary"]);
  assert.equal(value.command, command);
  assert.equal(value.ok, ok);
  assert.equal(typeof value.summary, "string");
  assert.ok(value.summary.length > 0);
  assert.ok(Array.isArray(value.details));
  assert.ok(value.details.length > 0);
  assert.ok(value.next_action.length > 0);
  return value;
}

test("contract default actions cover every declared outcome", () => {
  const cases: Record<string, string> = {
    complete: "autonomous_proceed",
    partial: "bounded_more_evidence",
    partial_degraded: "autonomous_proceed",
    no_match: "autonomous_proceed",
    stale: "bounded_refresh",
    unavailable: "blocked",
    blocked: "blocked",
    needs_more_evidence: "bounded_more_evidence",
    failed_verification: "quarantine",
    budget_exhausted: "human_approval",
  };
  for (const [outcome, action] of Object.entries(cases)) {
    test(`outcome ${outcome}`, () => {
      const packet = packetVariant((p) => {
        p.outcome = outcome;
        p.authorization.level = "bounded";
        if (outcome === "partial_degraded") p.degradations = ["limited scope"];
      });
      const result = run("contract", "validate", "--packet", packet, "--json");
      assert.equal(result.code, 0, result.stdout);
      const value = assertJsonResult(result.stdout, "contract validate", true);
      assert.ok(value.details.includes(`default action: ${action}`));
    });
  }
});

test("contract rejects every invalid parity variant", () => {
  const cases = [
    ["contract-invalid-missing-required.json", "required field"],
    ["contract-invalid-schema.json", "schema_version"],
    ["contract-invalid-outcome.json", "unknown outcome"],
    ["contract-invalid-freshness.json", "freshness"],
    ["contract-invalid-budget.json", "budget"],
    ["contract-invalid-degradations.json", "degradations"],
    ["contract-invalid-unverified-evidence.json", "verified evidence"],
    ["contract-invalid-forbidden-autonomy.json", "authorization"],
    ["contract-invalid-evidence.json", "required evidence field"],
  ] as const;
  for (const [file, message] of cases) {
    test(file, () => {
      const result = run("contract", "validate", "--packet", path.join(edge, file), "--json");
      assert.equal(result.code, 1, result.stdout);
      const value = assertJsonResult(result.stdout, "contract validate", false);
      assert.ok(`${value.summary}\n${value.details.join("\n")}`.includes(message));
    });
  }
});

test("contract enforces complete packet requirements", () => {
  const cases = [
    ["stale freshness", (p: JsonObject) => { p.freshness.status = "stale"; }, "freshness.status"],
    ["empty evidence", (p: JsonObject) => { p.evidence = []; }, "complete packet requires at least one evidence"],
    ["negative budget", (p: JsonObject) => { p.budget.used_evidence = -1; }, "budget.used_evidence"],
    ["used evidence over limit", (p: JsonObject) => { p.budget.used_evidence = 2; }, "used evidence exceeds"],
    ["degradation", (p: JsonObject) => { p.degradations = ["limited"]; }, "complete packet cannot contain degradations"],
    ["forbidden autonomous failure", (p: JsonObject) => {
      p.outcome = "blocked";
    }, "authorization.level"],
  ] as const;
  for (const [name, mutate, message] of cases) {
    test(name, () => {
      const packet = packetVariant(mutate);
      const result = run("contract", "validate", "--packet", packet);
      assert.equal(result.code, 1, result.stdout);
      assert.match(result.stdout, new RegExp(message.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    });
  }
});

test("strict packet boundaries reject malformed roots and trailing data", () => {
  const valid = readFileSync(path.join(fixtures, "packet-valid.json"), "utf8");
  const cases: Array<[string, string, string]> = [
    ["empty", "", "not valid JSON"],
    ["whitespace", " \n\t", "not valid JSON"],
    ["null root", "null", "not valid JSON"],
    ["array root", "[]", "must contain a JSON object"],
    ["string root", JSON.stringify("DO_NOT_PRINT"), "must contain a JSON object"],
    ["number root", "1", "must contain a JSON object"],
    ["truncated", valid.slice(0, -2), "not valid JSON"],
    ["trailing object", `${valid}{}`, "not valid JSON"],
    ["trailing null", `${valid} null`, "not valid JSON"],
    ["trailing comment", `${valid} // comment`, "not valid JSON"],
    ["unknown field", valid.replace('"packet_id":', '"unexpected_secret":"DO_NOT_PRINT","packet_id":'), "unknown JSON field"],
    ["duplicate field", valid.replace('"packet_id":', '"packet_id":"other","packet_id":'), "duplicate JSON object key"],
  ];
  for (const [name, input, message] of cases) {
    test(name, () => {
      const dir = tempDir();
      const packet = path.join(dir, "packet.json");
      writeFileSync(packet, input, { mode: 0o600 });
      const result = run("contract", "validate", "--packet", packet, "--json");
      assert.equal(result.code, 2, result.stdout);
      const value = assertJsonResult(result.stdout, "contract validate", false);
      assert.ok(`${value.summary}\n${value.details.join("\n")}`.includes(message));
      assert.doesNotMatch(result.stdout, /DO_NOT_PRINT|unexpected_secret/);
    });
  }
});

test("strict packet decoding rejects malformed field types", () => {
  const valid = readFileSync(path.join(fixtures, "packet-valid.json"), "utf8");
  const cases = [
    ["numeric packet id", valid.replace('"packet_id": "packet-1"', '"packet_id": 1')],
    ["numeric evidence", valid.replace('"evidence": [', '"evidence": {}')],
    ["numeric budget", valid.replace('"budget": {', '"budget": 1')],
    ["fractional integer", valid.replace('"max_evidence": 1', '"max_evidence": 1.5')],
    ["out of range integer", valid.replace('"max_bytes": 1000', '"max_bytes": 9223372036854775808')],
    ["invalid UTF-8", Buffer.from(valid.replace('"packet_hash": "packet-hash"', '"packet_hash": "packet-'), "utf8").toString("utf8") + "\xff"],
  ] as const;
  for (const [name, input] of cases) {
    test(name, () => {
      const dir = tempDir();
      const packet = path.join(dir, "packet.json");
      writeFileSync(packet, input, { mode: 0o600 });
      const result = run("contract", "validate", "--packet", packet);
      assert.equal(result.code, 2, result.stdout);
      assert.match(result.stdout, /packet could not be loaded/);
    });
  }
});

test("evidence verification covers hashes, ranges, and repository path safety", () => {
  const cases = [
    ["verified span", "notes.txt", "alpha\nbeta\n", 1, 2, sha256("alpha\nbeta\n"), 0, "verified 1 evidence span"],
    ["CRLF normalization", "notes.txt", "alpha\r\nbeta\r\n", 1, 2, sha256("alpha\nbeta\n"), 0, "verified 1 evidence span"],
    ["hash mismatch", "notes.txt", "alpha\nchanged\n", 1, 2, "0".repeat(64), 1, "content hash mismatch"],
    ["missing file", "missing.txt", "alpha\n", 1, 1, "0".repeat(64), 1, "cannot access evidence path"],
    ["absolute path", "/outside.txt", "alpha\n", 1, 1, "0".repeat(64), 1, "repository-relative"],
    ["parent path", "../outside.txt", "alpha\n", 1, 1, "0".repeat(64), 1, "parent component"],
    ["invalid range", "notes.txt", "alpha\nbeta\n", 2, 1, sha256("beta\n"), 1, "line_end must be greater than or equal to line_start"],
    ["range past end", "notes.txt", "alpha\nbeta\n", 1, 3, sha256("alpha\nbeta\n"), 1, "invalid line range"],
    ["invalid hash", "notes.txt", "alpha\nbeta\n", 1, 2, "not-a-hash", 1, "invalid SHA-256"],
  ] as const;
  for (const [name, evidencePath, contents, start, end, hash, code, message] of cases) {
    test(name, () => {
      const repo = tempDir("ownscout-repo-");
      writeFileSync(path.join(repo, "notes.txt"), contents);
      const packet = packetVariant((p) => {
        p.evidence[0].path = evidencePath;
        p.evidence[0].line_start = start;
        p.evidence[0].line_end = end;
        p.evidence[0].content_hash = hash;
      });
      const result = run("evidence", "verify", "--repo", repo, "--packet", packet, "--json");
      assert.equal(result.code, code, result.stdout);
      const value = assertJsonResult(result.stdout, "evidence verify", code === 0);
      assert.ok(`${value.summary}\n${value.details.join("\n")}`.includes(message));
      rmSync(repo, { recursive: true, force: true });
    });
  }
});

test("evidence rejects symlink files and directories", (t) => {
  for (const [name, evidencePath, linkName, linkTarget] of [
    ["symlink file", "linked.txt", "linked.txt", "notes.txt"],
    ["symlink directory", "linked/notes.txt", "linked", "."],
  ] as const) {
    test(name, () => {
      const repo = tempDir("ownscout-repo-");
      writeFileSync(path.join(repo, "notes.txt"), "alpha\nbeta\n");
      try {
        symlinkSync(linkTarget, path.join(repo, linkName));
      } catch (error) {
        rmSync(repo, { recursive: true, force: true });
        t.skip(`symlinks unavailable: ${error}`);
        return;
      }
      const packet = packetVariant((p) => {
        p.evidence[0].path = evidencePath;
        p.evidence[0].content_hash = sha256("alpha\nbeta\n");
      });
      const result = run("evidence", "verify", "--repo", repo, "--packet", packet);
      assert.equal(result.code, 1, result.stdout);
      assert.match(result.stdout, /symlink/);
      rmSync(repo, { recursive: true, force: true });
    });
  }
});

test("node envelope rejects strict parse and graph validation failures", () => {
  const cases = [
    ["envelope-cycle.json", "node-envelope-v1 validation failed"],
    ["envelope-duplicate-id.json", "node-envelope-v1 validation failed"],
    ["envelope-empty-nodes.json", "strict envelope parsing failed"],
    ["envelope-missing-dependency.json", "node-envelope-v1 validation failed"],
    ["envelope-nodes-not-array.json", "strict envelope parsing failed"],
    ["envelope-null-node-id.json", "strict envelope parsing failed"],
    ["envelope-self-dependency.json", "node-envelope-v1 validation failed"],
    ["envelope-unknown-evidence.json", "node-envelope-v1 validation failed"],
    ["envelope-unknown-field.json", "strict envelope parsing failed"],
    ["envelope-unknown-verifier.json", "node-envelope-v1 validation failed"],
    ["envelope-wrong-binding.json", "node-envelope-v1 validation failed"],
    ["envelope-wrong-packet-id.json", "node-envelope-v1 validation failed"],
    ["envelope-wrong-schema.json", "node-envelope-v1 validation failed"],
    ["envelope-duplicate-key.json", "strict envelope parsing failed"],
    ["envelope-trailing-json.json", "strict envelope parsing failed"],
  ] as const;
  for (const [file, message] of cases) {
    test(file, () => {
      const ledgerDir = tempDir();
      const result = run(
        "node", "verify", "--repo", repoFixture,
        "--packet", path.join(fixtures, "packet-valid.json"),
        "--envelope", path.join(edge, file),
        "--ledger", path.join(ledgerDir, "ledger.jsonl"), "--json",
      );
      assert.equal(result.code, 2, result.stdout);
      const value = assertJsonResult(result.stdout, "node verify", false);
      assert.ok(`${value.summary}\n${value.details.join("\n")}`.includes(message), result.stdout);
      assert.equal(existsSync(path.join(ledgerDir, "ledger.jsonl")), false);
      rmSync(ledgerDir, { recursive: true, force: true });
    });
  }
});

test("node evaluates independent graph branches in deterministic order", () => {
  const dir = tempDir();
  const envelope = jsonFile(path.join(fixtures, "envelope-valid.json"));
  envelope.nodes = [
    { node_id: "z", depends_on: [], verifier: "evidence.current", evidence_ids: ["evidence-1"] },
    { node_id: "d", depends_on: ["c", "b"], verifier: "evidence.current", evidence_ids: ["evidence-1"] },
    { node_id: "b", depends_on: ["a"], verifier: "evidence.current", evidence_ids: ["evidence-1"] },
    { node_id: "c", depends_on: ["a"], verifier: "evidence.current", evidence_ids: ["evidence-1"] },
    { node_id: "a", depends_on: [], verifier: "evidence.current", evidence_ids: ["evidence-1"] },
  ];
  const envelopePath = writeJson(dir, "envelope.json", envelope);
  const ledger = path.join(dir, "ledger.jsonl");
  const result = run(
    "node", "verify", "--repo", repoFixture,
    "--packet", path.join(fixtures, "packet-valid.json"),
    "--envelope", envelopePath, "--ledger", ledger, "--json",
  );
  assert.equal(result.code, 0, result.stdout);
  const value = assertJsonResult(result.stdout, "node verify", true);
  assert.deepEqual(value.details.map((x: string) => x.match(/node "([^"]+)"/)?.[1]), ["a", "b", "c", "d", "z"]);
  const record = JSON.parse(readFileSync(ledger, "utf8"));
  assert.deepEqual(record.node_results.map((x: JsonObject) => x.node_id), ["a", "b", "c", "d", "z"]);
  assert.deepEqual(record.node_results.map((x: JsonObject) => x.status), Array(5).fill("evidence_current"));
  rmSync(dir, { recursive: true, force: true });
});

test("node marks failed evidence and downstream nodes blocked", () => {
  const dir = tempDir();
  const ledger = path.join(dir, "ledger.jsonl");
  const result = run(
    "node", "verify", "--repo", repoFixture,
    "--packet", path.join(fixtures, "packet-stale.json"),
    "--envelope", path.join(edge, "envelope-stale-evidence.json"),
    "--ledger", ledger, "--json",
  );
  assert.equal(result.code, 1, result.stdout);
  const value = assertJsonResult(result.stdout, "node verify", false);
  assert.match(value.details[0], /node "a".*failed/);
  assert.match(value.details[1], /node "b".*blocked/);
  const record = JSON.parse(readFileSync(ledger, "utf8"));
  assert.deepEqual(record.node_results.map((x: JsonObject) => x.status), ["failed", "blocked"]);
  rmSync(dir, { recursive: true, force: true });
});

test("node appends a valid ledger chain and reopens it", () => {
  const dir = tempDir();
  const ledger = path.join(dir, "ledger.jsonl");
  const args = [
    "node", "verify", "--repo", repoFixture,
    "--packet", path.join(fixtures, "packet-valid.json"),
    "--envelope", path.join(fixtures, "envelope-valid.json"),
    "--ledger", ledger, "--json",
  ];
  assert.equal(run(...args).code, 0);
  assert.equal(run(...args).code, 0);
  assert.equal(lstatSync(ledger).mode & 0o777, 0o600);
  const records = readFileSync(ledger, "utf8").trim().split("\n").map(JSON.parse);
  assert.equal(records.length, 2);
  assert.equal(records[0].seq, 1);
  assert.equal(records[1].seq, 2);
  assert.equal(records[0].prev_record_hash, "0".repeat(64));
  assert.equal(records[1].prev_record_hash, records[0].record_hash);
  rmSync(dir, { recursive: true, force: true });
});

test("node protects ledger location and rejects invalid existing records", () => {
  const cases = [
    ["garbage", path.join(edge, "ledger-garbage.jsonl"), "ledger could not be opened"],
    ["inside repository", path.join(repoFixture, "ledger.jsonl"), "ledger could not be opened"],
    ["missing parent", path.join(tempDir(), "missing", "ledger.jsonl"), "ledger could not be opened"],
  ] as const;
  for (const [name, ledger, message] of cases) {
    test(name, () => {
      const result = run(
        "node", "verify", "--repo", repoFixture,
        "--packet", path.join(fixtures, "packet-valid.json"),
        "--envelope", path.join(fixtures, "envelope-valid.json"),
        "--ledger", ledger, "--json",
      );
      assert.equal(result.code, 2, result.stdout);
      const value = assertJsonResult(result.stdout, "node verify", false);
      assert.ok(`${value.summary}\n${value.details.join("\n")}`.includes(message));
    });
  }
});

test("CLI boundary commands preserve exit codes and output shape", () => {
  const cases: Array<[string[], number, string]> = [
    [["--help"], 0, "Usage:"],
    [["doctor"], 0, "OwnScout doctor: ok"],
    [["version"], 0, "ownscout 0.1.0"],
    [[], 2, "a command is required"],
    [["nope"], 2, "unknown command"],
    [["contract"], 2, "subcommand is required"],
    [["contract", "validate"], 2, "missing required"],
    [["contract", "validate", "--packet", "x", "--wat"], 2, "unknown flag"],
    [["evidence", "verify", "--repo", "x"], 2, "both --repo"],
    [["node", "verify", "--repo", "x"], 2, "missing required --packet"],
    [["doctor", "--json"], 2, "does not accept"],
    [["version", "--wat"], 2, "does not accept"],
  ];
  for (const [args, code, message] of cases) {
    test(args.length ? args.join(" ") : "no command", () => {
      const result = run(...args);
      assert.equal(result.code, code, result.stdout);
      assert.match(result.stdout, new RegExp(message.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
      if (args.length && args[0] !== "--help" && args[0] !== "version") assert.match(result.stdout, /Next action:/);
    });
  }
});

test("CLI JSON output is stable and does not leak packet contents", () => {
  const packet = packetVariant((p) => {
    p.packet_id = "DO_NOT_PRINT";
    p.untrusted = "DO_NOT_PRINT";
  });
  const result = run("contract", "validate", "--packet", packet, "--json");
  assert.equal(result.code, 2);
  const value = assertJsonResult(result.stdout, "contract validate", false);
  assert.equal(value.ok, false);
  assert.doesNotMatch(result.stdout, /DO_NOT_PRINT|untrusted/);
});

test("node invalid packet and envelope never create a ledger", () => {
  const cases = [
    ["invalid packet", path.join(fixtures, "packet-invalid.json"), path.join(fixtures, "envelope-valid.json")],
    ["malformed packet", path.join(fixtures, "packet-malformed.json"), path.join(fixtures, "envelope-valid.json")],
    ["invalid envelope", path.join(fixtures, "packet-valid.json"), path.join(fixtures, "envelope-invalid.json")],
  ] as const;
  for (const [name, packet, envelope] of cases) {
    test(name, () => {
      const dir = tempDir();
      const ledger = path.join(dir, "ledger.jsonl");
      const result = run("node", "verify", "--repo", repoFixture, "--packet", packet, "--envelope", envelope, "--ledger", ledger);
      assert.equal(result.code, 2, result.stdout);
      assert.equal(existsSync(ledger), false);
      rmSync(dir, { recursive: true, force: true });
    });
  }
});
