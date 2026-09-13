import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { goDecodeUtf8, goFoldName } from "./ownscout.ts";

const root = path.resolve(import.meta.dirname, "../..");
const bin = path.join(root, "ports/ts/bin/ownscout");
const fixture = (name: string) => path.join(root, "spec/parity/fixtures", name);

function run(...args: string[]) {
  return spawnSync(bin, args, { cwd: root, encoding: "utf8" });
}

// FIX B: Go's unquoteBytes decodes with utf8.DecodeRune and replaces each
// malformed byte with its own U+FFFD. TextDecoder instead collapses a maximal
// subpart, which is why a truncated multi-byte sequence used to yield too few
// replacement runes.
test("goDecodeUtf8 replaces each malformed byte with one U+FFFD", () => {
  assert.equal(goDecodeUtf8(Buffer.from([0xf0, 0x9f])), "\ufffd\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xe2, 0x82])), "\ufffd\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xe0, 0xa0])), "\ufffd\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xf0, 0x90, 0x80])), "\ufffd\ufffd\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xff])), "\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xc0, 0x80])), "\ufffd\ufffd");
  assert.equal(goDecodeUtf8(Buffer.from([0xed, 0xa0, 0x80])), "\ufffd\ufffd\ufffd");
  // Valid UTF-8, including multi-byte and astral runes, is copied verbatim.
  assert.equal(goDecodeUtf8(Buffer.from("héllo🎉", "utf8")), "héllo🎉");
  assert.equal(goDecodeUtf8(Buffer.from("ascii line\n", "latin1")), "ascii line\n");
});

// FIX A: Go's foldName upper-cases ASCII and maps every other rune to the
// minimum of its SimpleFold orbit. For an ASCII field name only the two runes
// that fold into ASCII can matter, so this is exactly Go's relation here.
test("goFoldName folds ASCII to upper case without toLowerCase semantics", () => {
  assert.equal(goFoldName("schema_version"), "SCHEMA_VERSION");
  assert.equal(goFoldName("SCHEMA_VERSION"), "SCHEMA_VERSION");
  assert.equal(goFoldName("LiNe_StArT"), "LINE_START");
  assert.equal(goFoldName("\u017f"), "S"); // long s folds to S
  assert.equal(goFoldName("\u212a"), "K"); // Kelvin sign folds to K
  assert.equal(goFoldName("öutcome"), "öUTCOME"); // not "OUTCOME"
});

test("contract validate folds field names but node verify stays exact-only", () => {
  assert.equal(run("contract", "validate", "--packet", fixture("edge/packet-edge-case-top.json")).status, 0);
  assert.equal(run("contract", "validate", "--packet", fixture("edge/packet-edge-case-nested.json")).status, 0);
  assert.equal(
    run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-edge-case-top.json")).status,
    0,
  );
  const node = run(
    "node", "verify",
    "--repo", fixture("repo"),
    "--packet", fixture("edge/packet-edge-case-top.json"),
    "--envelope", fixture("envelope-valid.json"),
    "--ledger", "/tmp/ownscout-node-exact-only.jsonl",
  );
  assert.equal(node.status, 2);
  assert.match(node.stdout, /strict packet decoding failed/);
});

// FIX C: a null evidence element is Go's zero-valued Evidence, so the two line
// range rules fire in addition to the ten missing-field/status rules.
test("a null evidence element is a zero-valued struct with 12 violations", () => {
  const result = run("contract", "validate", "--packet", fixture("edge/packet-edge-null-evidence-element.json"));
  assert.equal(result.status, 1);
  assert.match(result.stdout, /packet is invalid \(12 violation\(s\)\)/);
  assert.match(result.stdout, /evidence\[0\]\.line_start: line_start must be at least 1/);
  assert.match(result.stdout, /evidence\[0\]\.line_end: line_end must be at least 1/);
});

test("a raw truncated UTF-8 field name is reported as two U+FFFD", () => {
  const result = run("contract", "validate", "--packet", fixture("edge/packet-edge-unknown-badutf8.json"));
  assert.equal(result.status, 2);
  assert.match(result.stdout, /contains an unknown JSON field: json: unknown field "\ufffd\ufffd"/);
});

// Go stores a fold-matched value in the canonical field but names the key
// exactly as the document wrote it in a type error.
test("a fold-matched type error names the key as written", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-fold-"));
  try {
    const packet = JSON.parse(readFileSync(fixture("packet-valid.json"), "utf8"));
    packet.PACKET_ID = 5;
    const file = path.join(dir, "packet.json");
    writeFileSync(file, JSON.stringify(packet));
    const result = run("contract", "validate", "--packet", file);
    assert.equal(result.status, 2);
    assert.match(result.stdout, /Go struct field Packet\.PACKET_ID of type string/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// node verify uses internal/nodepacket's strict decoder: exact field names, no
// duplicate keys, no explicit null anywhere, and valid UTF-8 only. Every
// failure collapses to the same boundary message.
test("node verify uses the strict nodepacket decoder", () => {
  for (const name of [
    "packet-edge-node-duplicate-key",
    "packet-edge-node-null-field",
    "packet-edge-node-badutf8",
    "packet-edge-null-evidence-element",
  ]) {
    const result = run(
      "node", "verify",
      "--repo", fixture("repo"),
      "--packet", fixture(`edge/${name}.json`),
      "--envelope", fixture("envelope-valid.json"),
      "--ledger", path.join(tmpdir(), "ownscout-strict-node.jsonl"),
    );
    assert.equal(result.status, 2, name);
    assert.match(result.stdout, /packet could not be decoded/, name);
    assert.match(result.stdout, /strict packet decoding failed/, name);
  }
});

// The same three packets stay on the lenient encoding/json path for contract
// validate: duplicate keys are last-wins, bad UTF-8 is replaced, and null is a
// zero value that only contract validation complains about.
test("contract validate keeps the lenient decoder for the strict fixtures", () => {
  assert.equal(run("contract", "validate", "--packet", fixture("edge/packet-edge-node-duplicate-key.json")).status, 0);
  assert.equal(run("contract", "validate", "--packet", fixture("edge/packet-edge-node-badutf8.json")).status, 0);
  const nullField = run("contract", "validate", "--packet", fixture("edge/packet-edge-node-null-field.json"));
  assert.equal(nullField.status, 1);
  assert.match(nullField.stdout, /packet is invalid \(1 violation\(s\)\)/);
  assert.match(nullField.stdout, /issued_at: required field is missing/);
});

function writePacketId(dir: string, escape: string): string {
  const raw = readFileSync(fixture("packet-valid.json"), "utf8").replace(
    '"packet_id": "packet-1"',
    `"packet_id": "${escape}"`,
  );
  const file = path.join(dir, "packet.json");
  writeFileSync(file, raw);
  return file;
}

function nodeVerify(packet: string) {
  return run(
    "node", "verify",
    "--repo", fixture("repo"),
    "--packet", packet,
    "--envelope", fixture("envelope-valid.json"),
    "--ledger", path.join(tmpdir(), "ownscout-surrogate-ledger.jsonl"),
  );
}

// A valid surrogate pair is a legitimate escape; only unpaired surrogates are
// rejected by nodepacket.validUnicodeEscapes.
test("node verify accepts a valid surrogate pair and rejects unpaired ones", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-surrogate-"));
  try {
    const pair = nodeVerify(writePacketId(dir, "\\ud83c\\udf89"));
    assert.equal(pair.status, 2);
    assert.match(pair.stdout, /envelope validation failed/);
    for (const escape of ["\\ud800", "\\udc00", "\\uZZZZ"]) {
      const lone = nodeVerify(writePacketId(dir, escape));
      assert.equal(lone.status, 2, escape);
      assert.match(lone.stdout, /packet could not be decoded/, escape);
    }
    // An escaped backslash is not an escape introducer.
    const escaped = nodeVerify(writePacketId(dir, "a\\\\ud800b"));
    assert.equal(escaped.status, 2);
    assert.match(escaped.stdout, /envelope validation failed/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// Go reports every non-decode violation as a packet contract failure, even a
// single one; it must not fall through to envelope validation.
test("node verify reports a single packet contract violation", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-one-violation-"));
  try {
    const packet = JSON.parse(readFileSync(fixture("packet-valid.json"), "utf8"));
    packet.freshness.head_commit = "other";
    const file = path.join(dir, "packet.json");
    writeFileSync(file, JSON.stringify(packet));
    const result = nodeVerify(file);
    assert.equal(result.status, 2);
    assert.match(result.stdout, /packet contract failed \(1 violation\(s\)\)/);
    assert.doesNotMatch(result.stdout, /envelope validation failed/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// encoding/json's whitespace is exactly space, tab, LF and CR; JavaScript's \s
// also accepts vertical tab, form feed and NBSP.
test("node verify rejects non-JSON whitespace around the packet", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "ownscout-whitespace-"));
  try {
    const raw = readFileSync(fixture("packet-valid.json"), "utf8");
    for (const ch of ["\u000b", "\u000c", "\u00a0"]) {
      const file = path.join(dir, "packet.json");
      writeFileSync(file, ch + raw);
      const result = nodeVerify(file);
      assert.equal(result.status, 2, JSON.stringify(ch));
      assert.match(result.stdout, /packet could not be decoded/, JSON.stringify(ch));
    }
    const file = path.join(dir, "packet.json");
    writeFileSync(file, " \t\r\n" + raw);
    assert.equal(nodeVerify(file).status, 0);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
