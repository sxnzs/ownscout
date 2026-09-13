import test from "node:test";
import assert from "node:assert/strict";
import { goQuote } from "./ownscout.ts";

// Go's strconv.Quote, which is what encoding/json's `json: unknown field %q`
// uses, has exactly three escape widths: \xNN for control bytes and DEL, \uNNNN
// for non-printable runes up to U+FFFF, and \UNNNNNNNN above it. Printable runes
// (by strconv.IsPrint) are emitted literally, including non-ASCII and astral
// ones. These tests pin each row of that behaviour.

test("quotes and backslashes are always backslashed", () => {
  assert.equal(goQuote('"'), '"\\""');
  assert.equal(goQuote("\\"), '"\\\\"');
  assert.equal(goQuote('a"b\\c'), '"a\\"b\\\\c"');
});

test("the C escapes use their short forms", () => {
  assert.equal(goQuote("\u0007"), '"\\a"');
  assert.equal(goQuote("\u0008"), '"\\b"');
  assert.equal(goQuote("\u000c"), '"\\f"');
  assert.equal(goQuote("\u000a"), '"\\n"');
  assert.equal(goQuote("\u000d"), '"\\r"');
  assert.equal(goQuote("\u0009"), '"\\t"');
  assert.equal(goQuote("\u000b"), '"\\v"');
  assert.equal(goQuote("\u0007\u0008\u000c\u000a\u000d\u0009\u000b"), '"\\a\\b\\f\\n\\r\\t\\v"');
});

test("other control bytes and DEL use two-digit lowercase \\xNN", () => {
  assert.equal(goQuote("\u0000"), '"\\x00"');
  assert.equal(goQuote("\u0001"), '"\\x01"');
  assert.equal(goQuote("\u001f"), '"\\x1f"');
  assert.equal(goQuote("\u007f"), '"\\x7f"');
  assert.equal(goQuote("x\u0001y\u001fz\u000dw"), '"x\\x01y\\x1fz\\rw"');
});

test("printable non-ASCII runes stay literal", () => {
  assert.equal(goQuote("é"), '"é"');
  assert.equal(goQuote("🎉"), '"🎉"');
  assert.equal(goQuote("¡ÿ"), '"¡ÿ"');
  assert.equal(goQuote("héllo🎉"), '"héllo🎉"');
});

test("non-printable BMP runes use four-digit lowercase \\uNNNN", () => {
  assert.equal(goQuote("\u00a0"), '"\\u00a0"');
  assert.equal(goQuote("\u2028"), '"\\u2028"');
  assert.equal(goQuote("\u00ad"), '"\\u00ad"'); // soft hyphen
  assert.equal(goQuote("héllo🎉\u00a0\u2028"), '"héllo🎉\\u00a0\\u2028"');
});

test("non-printable astral runes use eight-digit lowercase \\UNNNNNNNN", () => {
  assert.equal(goQuote("\u{10fffe}"), '"\\U0010fffe"');
  assert.equal(goQuote("astral\u{10fffe}end"), '"astral\\U0010fffeend"');
  // A printable astral rune (the emoji above) is literal, not escaped.
  assert.equal(goQuote("astral🎉end"), '"astral🎉end"');
});

test("a lone surrogate becomes Go's replacement rune, which is printable", () => {
  assert.equal(goQuote("\ud800"), '"\uFFFD"');
  assert.equal(goQuote("\udc00"), '"\uFFFD"');
});

test("the fixture keys quote exactly as the reference reports them", () => {
  assert.equal(goQuote('a"b\\c\td\ne\u007ff'), '"a\\"b\\\\c\\td\\ne\\x7ff"');
  assert.equal(goQuote("x\u0001y\u001fz\rw"), '"x\\x01y\\x1fz\\rw"');
  assert.equal(goQuote("héllo🎉\u00a0\u2028"), '"héllo🎉\\u00a0\\u2028"');
  assert.equal(goQuote("astral\u{10fffe}end"), '"astral\\U0010fffeend"');
});
