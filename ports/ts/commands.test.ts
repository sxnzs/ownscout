import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "../..");
const bin = path.join(root, "ports/ts/bin/ownscout");
const fixture = (name: string) => path.join(root, "spec/parity/fixtures", name);

function run(...args: string[]) {
  return spawnSync(bin, args, { cwd: root, encoding: "utf8" });
}

test("ledger verify reports an intact chain and its tip", () => {
  const human = run("ledger", "verify", "--ledger", fixture("edge/ledger-seed.jsonl"));
  assert.equal(human.status, 0);
  assert.equal(
    human.stdout,
    "OK: ledger is intact\n  - 1 record(s), tip 15f1081bbfe69a07da19cc3ae59930638980bb944be90ac1e2db2726cad63ed6\nNext action: The ledger chain is intact.\n",
  );
  const json = run("ledger", "verify", "--ledger", fixture("edge/ledger-seed.jsonl"), "--json");
  assert.equal(JSON.parse(json.stdout).details[0], "1 record(s), tip 15f1081bbfe69a07da19cc3ae59930638980bb944be90ac1e2db2726cad63ed6");
});

test("ledger verify distinguishes a malformed chain from an unreadable path", () => {
  const garbage = run("ledger", "verify", "--ledger", fixture("edge/ledger-garbage.jsonl"));
  assert.equal(garbage.status, 1);
  assert.match(garbage.stdout, /^ERROR: ledger verification failed$/m);
  assert.match(garbage.stdout, /line 1: malformed JSON: invalid character 'o' in literal null \(expecting 'u'\)/);

  const absent = run("ledger", "verify", "--ledger", fixture("edge/ledger-absent.jsonl"), "--json");
  assert.equal(absent.status, 2);
  assert.equal(JSON.parse(absent.stdout).summary, "ledger could not be read");
  assert.match(JSON.parse(absent.stdout).details[0], /lstat .*ledger-absent\.jsonl: no such file or directory/);

  const directory = run("ledger", "verify", "--ledger", fixture("repo"), "--json");
  assert.equal(directory.status, 2);
  assert.match(JSON.parse(directory.stdout).details[0], /not a regular file$/);
});

test("ledger subcommand dispatch", () => {
  const none = run("ledger");
  assert.equal(none.status, 2);
  assert.equal(none.stdout, "error: a ledger subcommand is required\nNext action: run 'ownscout ledger --help'.\n");
  const unknown = run("ledger", "bogus", "--json");
  assert.equal(unknown.status, 2);
  assert.equal(JSON.parse(unknown.stdout).summary, "unknown ledger subcommand 'bogus'");
  const missing = run("ledger", "verify");
  assert.equal(missing.status, 2);
  assert.equal(missing.stdout, "error: missing required --ledger value\nNext action: run 'ownscout ledger verify --help'.\n");
});

test("node bind computes the canonical packet binding", () => {
  const human = run("node", "bind", "--packet", fixture("packet-valid.json"));
  assert.equal(human.status, 0);
  assert.equal(
    human.stdout,
    "OK: packet binding computed\n  - 63d0897ae0c01ee58180a5240dfeea403d99a9d7f6d42e079af6330e8e7267ca\nNext action: Use this as packet_binding_sha256 in a node-envelope-v1 document.\n",
  );
  const json = run("node", "bind", "--packet", fixture("packet-valid.json"), "--json");
  assert.equal(json.status, 0);
  assert.equal(JSON.parse(json.stdout).command, "node bind");
});

test("node bind mirrors the strict decoder and contract paths", () => {
  const contract = run("node", "bind", "--packet", fixture("edge/contract-invalid-schema.json"));
  assert.equal(contract.status, 1);
  assert.match(contract.stdout, /^ERROR: packet contract failed \(1 violation\(s\)\)$/m);
  assert.match(contract.stdout, /schema_version: must be v1/);

  const decode = run("node", "bind", "--packet", fixture("edge/packet-edge-node-duplicate-key.json"), "--json");
  assert.equal(decode.status, 2);
  assert.equal(JSON.parse(decode.stdout).summary, "packet could not be decoded");

  const unknown = run("node", "bind", "--packet", fixture("edge/packet-edge-unknown-specials.json"), "--json");
  assert.equal(unknown.status, 2);
  assert.equal(JSON.parse(unknown.stdout).summary, "packet could not be decoded");

  const missing = run("node", "bind");
  assert.equal(missing.status, 2);
  assert.equal(missing.stdout, "error: missing required --packet value\nNext action: run 'ownscout node bind --help'.\n");
});

test("node verify --relocate appends evidence details without touching the ledger", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-node-relocate-"));
  try {
    const base = ["node", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-moved.json"), "--envelope", fixture("edge/envelope-relocate-moved.json")];
    const off = spawnSync(bin, [...base, "--ledger", path.join(dir, "off.jsonl")], { cwd: root, encoding: "utf8" });
    const on = spawnSync(bin, [...base, "--ledger", path.join(dir, "on.jsonl"), "--relocate"], { cwd: root, encoding: "utf8" });
    assert.equal(off.status, 1);
    assert.equal(on.status, 1);
    assert.doesNotMatch(off.stdout, /content relocates/);
    assert.match(on.stdout, /content relocates to lines 4-5 \(shift \+3; nearest matching window\)/);
    // Relocation is diagnostic: the ledger record is byte-identical either way.
    assert.equal(readFileSync(path.join(dir, "off.jsonl"), "utf8"), readFileSync(path.join(dir, "on.jsonl"), "utf8"));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("root and node help advertise the new commands", () => {
  const rootHelp = run("--help");
  assert.match(rootHelp.stdout, /ownscout ledger verify --ledger <file> \[--json\]/);
  assert.match(rootHelp.stdout, /ownscout ledger rotate --ledger <file> \[--json\]/);
  assert.match(rootHelp.stdout, /ownscout node bind --packet <file> \[--json\]/);
  const nodeHelp = run("node", "--help");
  assert.match(nodeHelp.stdout, /Usage: ownscout node bind --packet <file> \[--json\]\n       ownscout node verify/);
  const bindHelp = run("node", "bind", "--help");
  assert.match(bindHelp.stdout, /^Usage: ownscout node bind --packet <file> \[--json\]\n\nComputes the canonical packet binding for a strictly decoded packet\./);
  const ledgerHelp = run("ledger", "--help");
  assert.match(ledgerHelp.stdout, /^Usage: ownscout ledger verify --ledger <file> \[--json\]\n       ownscout ledger rotate --ledger <file> \[--json\]\n\nAudits or archives an append-only ledger\./);
  // There is no rotate-specific help text in the reference, so it falls back
  // to the ledger subcommand help.
  assert.equal(run("ledger", "rotate", "--help").stdout, ledgerHelp.stdout);
});

test("ledger rotate archives the chain under its tip", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-rotate-"));
  try {
    const ledger = path.join(dir, "ledger.jsonl");
    writeFileSync(ledger, readFileSync(fixture("edge/ledger-seed.jsonl")));
    const result = run("ledger", "rotate", "--ledger", ledger, "--json");
    assert.equal(result.status, 0, result.stdout);
    const value = JSON.parse(result.stdout);
    assert.equal(value.summary, "ledger rotated");
    assert.equal(value.details[0], "1 record(s), tip 15f1081bbfe69a07da19cc3ae59930638980bb944be90ac1e2db2726cad63ed6");
    assert.equal(value.details[1], `archived to ${ledger}.15f1081b`);
    assert.equal(existsSync(ledger), false);
    assert.equal(existsSync(`${ledger}.15f1081b`), true);
    // The archive is itself a valid ledger.
    assert.equal(run("ledger", "verify", "--ledger", `${ledger}.15f1081b`).status, 0);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("ledger rotate reports collisions, garbage, missing and empty ledgers", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-rotate-"));
  try {
    const ledger = path.join(dir, "ledger.jsonl");
    writeFileSync(ledger, readFileSync(fixture("edge/ledger-seed.jsonl")));
    writeFileSync(`${ledger}.15f1081b`, "already here");
    const collision = run("ledger", "rotate", "--ledger", ledger, "--json");
    assert.equal(collision.status, 2);
    assert.equal(JSON.parse(collision.stdout).summary, "ledger could not be rotated");
    assert.equal(JSON.parse(collision.stdout).details[0], `archive "${ledger}.15f1081b" already exists`);
    assert.equal(existsSync(ledger), true);

    const garbage = run("ledger", "rotate", "--ledger", fixture("edge/ledger-garbage.jsonl"), "--json");
    assert.equal(garbage.status, 1);
    assert.equal(JSON.parse(garbage.stdout).summary, "ledger verification failed");

    const missing = run("ledger", "rotate", "--ledger", path.join(dir, "absent.jsonl"), "--json");
    assert.equal(missing.status, 2);
    assert.match(JSON.parse(missing.stdout).details[0], /lstat .*absent\.jsonl: no such file or directory/);

    const empty = path.join(dir, "empty.jsonl");
    writeFileSync(empty, "");
    const emptyResult = run("ledger", "rotate", "--ledger", empty, "--json");
    assert.equal(emptyResult.status, 2);
    assert.equal(JSON.parse(emptyResult.stdout).details[0], `ledger "${empty}" is empty`);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// Open hardens against symlinked ancestors; Verify does not. The same ledger
// path is accepted by ledger verify and rejected by node verify.
test("append rejects a symlinked ledger ancestor while ledger verify accepts it", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-symlink-"));
  try {
    const real = path.join(dir, "real");
    const linked = path.join(dir, "linked");
    mkdirSync(real);
    const ledger = path.join(real, "out.jsonl");
    writeFileSync(ledger, readFileSync(fixture("edge/ledger-seed.jsonl")));
    symlinkSync(real, linked, "dir");
    const viaLink = path.join(linked, "out.jsonl");
    assert.equal(run("ledger", "verify", "--ledger", viaLink).status, 0);
    const node = run(
      "node", "verify",
      "--repo", fixture("repo"),
      "--packet", fixture("packet-valid.json"),
      "--envelope", fixture("envelope-valid.json"),
      "--ledger", viaLink,
    );
    assert.equal(node.status, 2);
    assert.match(node.stdout, /ledger could not be opened/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
