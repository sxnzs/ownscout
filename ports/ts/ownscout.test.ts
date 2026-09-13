import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";

const root = path.resolve(import.meta.dirname, "../..");
const bin = path.join(root, "ports/ts/bin/ownscout");
const fixture = (name: string) => path.join(root, "spec/parity/fixtures", name);

function run(...args: string[]) {
  return spawnSync(bin, args, { cwd: root, encoding: "utf8" });
}

test("version reports the CLI version", () => {
  const result = run("version");
  assert.equal(result.status, 0);
  assert.match(result.stdout, /^ownscout /);
});

test("contract validation accepts a valid packet", () => {
  const result = run("contract", "validate", "--packet", fixture("packet-valid.json"), "--json");
  assert.equal(result.status, 0);
  assert.equal(JSON.parse(result.stdout).ok, true);
});

test("contract validation reports malformed JSON", () => {
  const result = run("contract", "validate", "--packet", fixture("packet-malformed.json"));
  assert.equal(result.status, 2);
  assert.match(result.stdout, /packet could not be decoded/);
  assert.match(result.stdout, /strict packet decoding failed/);
});

test("evidence verification reports stale content", () => {
  const result = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("packet-stale.json"));
  assert.equal(result.status, 1);
  assert.match(result.stdout, /content hash mismatch/);
});

test("node verification records a valid envelope", () => {
  const ledgerDir = mkdtempSync(path.join(tmpdir(), "ownscout-test-"));
  try {
    const result = run(
      "node", "verify",
      "--repo", fixture("repo"),
      "--packet", fixture("packet-valid.json"),
      "--envelope", fixture("envelope-valid.json"),
      "--ledger", path.join(ledgerDir, "ledger.jsonl"),
      "--json",
    );
    assert.equal(result.status, 0);
    assert.equal(JSON.parse(result.stdout).ok, true);
  } finally {
    rmSync(ledgerDir, { recursive: true, force: true });
  }
});
