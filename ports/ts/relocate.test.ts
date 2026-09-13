import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import {
  countLines,
  contentEnd,
  hashSelectedLines,
  lineStarts,
  locationClause,
  relocationClause,
  resolveAnchor,
  windowHash,
} from "./ownscout.ts";

const root = path.resolve(import.meta.dirname, "../..");
const bin = path.join(root, "ports/ts/bin/ownscout");
const fixture = (name: string) => path.join(root, "spec/parity/fixtures", name);

function run(...args: string[]) {
  return spawnSync(bin, args, { cwd: root, encoding: "utf8" });
}

// The relocation path locates lines through a precomputed start index, while
// verification walks the file from byte zero. They must agree on every range of
// every awkward payload, or a relocation could name a window that verification
// would have hashed differently. This is the differential reference for that
// equivalence, matching TestWindowHashMatchesReferenceHashing in the reference.
test("window hashing equals reference hashing for awkward payloads", () => {
  const payloads: Buffer[] = [
    Buffer.alloc(0),
    Buffer.from("\n"),
    Buffer.from("\n\n"),
    Buffer.from("a"),
    Buffer.from("a\n"),
    Buffer.from("a\r\nb\r\n"), // CRLF terminators
    Buffer.from("a\r\nb"),
    Buffer.from("a\nb"), // unterminated tail
    Buffer.from("a\nb\r"), // unterminated tail ending in a lone CR (kept as content)
    Buffer.from("b\r"),
    Buffer.from("\r"),
    Buffer.from("a\r\nb\r"),
    Buffer.from("a\rb"),
    Buffer.from("\n"), // a single empty line
    Buffer.from("one\r\ntwo\r\nthree\r\nfour\r\nfive"),
    Buffer.from(" \n\t\n\n  \n"),
    Buffer.concat([Buffer.from("x".repeat(200) + "\n")].concat(Array(40).fill(Buffer.from("line\r\n")))),
  ];

  for (const data of payloads) {
    const starts = lineStarts(data);
    const total = countLines(data);
    assert.equal(total, starts.length, `line model disagrees for ${JSON.stringify(data.toString("latin1"))}`);
    for (let start = 1; start <= total; start++) {
      for (let end = start; end <= total; end++) {
        assert.equal(
          windowHash(data, starts, start, end),
          hashSelectedLines(data, start, end),
          `payload ${JSON.stringify(data.toString("latin1"))} range ${start}-${end}`,
        );
      }
    }
  }
});

test("contentEnd strips CR only as part of a CRLF terminator", () => {
  // A terminated line drops both terminator bytes...
  const crlf = Buffer.from("a\r\nb\r\n");
  const crlfStarts = lineStarts(crlf);
  assert.equal(contentEnd(crlf, crlfStarts, 1), 1);
  assert.equal(contentEnd(crlf, crlfStarts, 2), 4);
  const lf = Buffer.from("a\nb\n");
  assert.equal(contentEnd(lf, lineStarts(lf), 1), 1);
  // ...but an unterminated final line keeps a trailing CR as content, exactly
  // as hashSelectedLines keeps it. Stripping it here would let a relocation
  // name a window that verification hashes differently.
  const lone = Buffer.from("a\r\nb\r");
  assert.equal(contentEnd(lone, lineStarts(lone), 2), 5);
  assert.equal(contentEnd(Buffer.from("b\r"), lineStarts(Buffer.from("b\r")), 1), 2);
  assert.equal(contentEnd(Buffer.from("\r"), lineStarts(Buffer.from("\r")), 1), 1);
});

test("resolveAnchor finds the nearest matching window with a signed shift", () => {
  const data = Buffer.from("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n");
  const starts = lineStarts(data);
  const total = countLines(data);
  const expected = windowHash(data, starts, 5, 6);
  const got = resolveAnchor(data, starts, total, 2, 3, expected);
  assert.deepEqual(got, { found: true, lineStart: 5, lineEnd: 6, shift: 3, exhaustive: false });
});

test("resolveAnchor reports absence only when the whole file was probed", () => {
  const data = Buffer.from("alpha\nbeta\ngamma\ndelta\n");
  const starts = lineStarts(data);
  const total = countLines(data);
  const got = resolveAnchor(data, starts, total, 1, 2, "0".repeat(64));
  assert.equal(got.found, false);
  assert.equal(got.exhaustive, true);
  assert.equal(relocationClause(data, total, 1, 2, "0".repeat(64)), "; content not found elsewhere in this file");
});

test("resolveAnchor prefers the lower line number on a tie", () => {
  const data = Buffer.from("same\nx\nsame\n");
  const starts = lineStarts(data);
  const expected = windowHash(data, starts, 1, 1);
  const got = resolveAnchor(data, starts, countLines(data), 2, 2, expected);
  assert.equal(got.found, true);
  assert.equal(got.lineStart, 1);
  assert.equal(got.shift, -1);
});

test("exact-fit window: a single valid start is found or reported absent", () => {
  const data = Buffer.from("a\nb\nc\n");
  const starts = lineStarts(data);
  const total = countLines(data);
  const found = resolveAnchor(data, starts, total, 1, 3, windowHash(data, starts, 1, 3));
  assert.deepEqual(found, { found: true, lineStart: 1, lineEnd: 3, shift: 0, exhaustive: false });
  assert.equal(
    relocationClause(data, total, 1, 3, windowHash(data, starts, 1, 3)),
    "; content relocates to lines 1-3 (shift +0; nearest matching window)",
  );
  const missing = resolveAnchor(data, starts, total, 1, 3, "0".repeat(64));
  assert.equal(missing.found, false);
  assert.equal(missing.exhaustive, true);
});

test("a window longer than the file is exhaustively absent and a reversed range yields no clause", () => {
  const data = Buffer.from("a\nb\n");
  const total = countLines(data);
  assert.equal(resolveAnchor(data, lineStarts(data), total, 1, 5, "0".repeat(64)).exhaustive, true);
  assert.equal(relocationClause(data, total, 1, 5, "0".repeat(64)), "; content not found elsewhere in this file");
  assert.equal(relocationClause(data, total, 2, 1, "0".repeat(64)), "");
});

test("resolveAnchor stops on the byte budget without claiming absence", () => {
  const line = Buffer.from("x".repeat(63) + "\n");
  const data = Buffer.concat(Array(40000).fill(line));
  const starts = lineStarts(data);
  const total = countLines(data);
  const got = resolveAnchor(data, starts, total, 20000, 20019, "0".repeat(64));
  assert.equal(got.found, false);
  assert.equal(got.exhaustive, false);
  assert.equal(relocationClause(data, total, 20000, 20019, "0".repeat(64)), "; relocation search stopped after its byte budget");
});

test("relocate annotates a moved span without changing the verdict", () => {
  const moved = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-moved.json"), "--relocate");
  assert.equal(moved.status, 1);
  assert.match(moved.stdout, /^ERROR: evidence verification failed \(1 issue\(s\)\)$/m);
  assert.match(moved.stdout, /content relocates to lines 4-5 \(shift \+3; nearest matching window\)/);

  const plain = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-moved.json"));
  assert.equal(plain.status, 1);
  assert.doesNotMatch(plain.stdout, /relocates/);
});

test("relocate reports content that is gone and content in a shrunken file", () => {
  const gone = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-gone.json"), "--relocate", "--json");
  assert.equal(gone.status, 1);
  assert.match(gone.stdout, /content not found elsewhere in this file/);

  const shrink = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-shrink.json"), "--relocate");
  assert.equal(shrink.status, 1);
  assert.match(shrink.stdout, /invalid line range 9000-9001 for 3 line\(s\); content relocates to lines 2-3 \(shift -8998; nearest matching window\)/);
});

test("relocate stops after its byte budget", () => {
  const budget = run("evidence", "verify", "--repo", fixture("repo"), "--packet", fixture("edge/packet-evidence-relocate-budget.json"), "--relocate");
  assert.equal(budget.status, 1);
  assert.match(budget.stdout, /relocation search stopped after its byte budget/);
});

test("an unusable fingerprint or disabled option yields no relocation clause", () => {
  // A content hash that is not a valid SHA-256 cannot anchor anything, so the
  // failure keeps the pre-relocation message byte-for-byte, as does --relocate
  // being absent.
  const data = Buffer.from("alpha\nbeta\n");
  const total = countLines(data);
  const hash = windowHash(data, lineStarts(data), 1, 1);
  assert.equal(locationClause(data, total, 1, 1, "not-a-hash", { relocate: true }), "");
  assert.equal(locationClause(data, total, 1, 1, "zz".repeat(32), { relocate: true }), "");
  assert.equal(locationClause(data, total, 1, 1, hash, { relocate: false }), "");
  // A valid fingerprint is accepted with the sha256: prefix and uppercase hex.
  assert.equal(
    locationClause(data, total, 2, 2, "sha256:" + windowHash(data, lineStarts(data), 2, 2).toUpperCase(), { relocate: true }),
    "; content relocates to lines 2-2 (shift +0; nearest matching window)",
  );
});

test("--relocate is rejected by contract validate and node verify", () => {
  const contract = run("contract", "validate", "--packet", fixture("packet-valid.json"), "--relocate");
  assert.equal(contract.status, 2);
  assert.equal(contract.stdout, "error: unknown flag or argument '--relocate'\nNext action: run 'ownscout contract validate --help'.\n");
  const node = run("node", "verify", "--repo", fixture("repo"), "--packet", fixture("packet-valid.json"), "--envelope", fixture("envelope-valid.json"), "--ledger", "/tmp/does-not-matter.jsonl", "--relocate");
  assert.equal(node.status, 2);
  assert.match(node.stdout, /unknown flag or argument '--relocate'/);
});

test("help text advertises --relocate on evidence verify only", () => {
  const rootHelp = run("--help");
  assert.match(rootHelp.stdout, /ownscout evidence verify --repo <dir> --packet <file> \[--relocate\] \[--json\]/);
  const evidenceHelp = run("evidence", "verify", "--help");
  assert.equal(
    evidenceHelp.stdout,
    "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nValidates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun.\n",
  );
  const subcommandHelp = run("evidence", "--help");
  assert.match(subcommandHelp.stdout, /Usage: ownscout evidence verify --repo <dir> --packet <file> \[--relocate\] \[--json\]/);
});
