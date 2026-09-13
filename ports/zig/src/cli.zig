const std = @import("std");
const result = @import("result.zig");
const json = @import("json.zig");
const contract = @import("contract.zig");
const gofold = @import("gofold.zig");
const Sha256 = std.crypto.hash.sha2.Sha256;

pub const RootUsage =
    "OwnScout — local repository evidence checks\n\n" ++
    "Usage:\n  ownscout doctor\n  ownscout version\n  ownscout contract validate --packet <file> [--json]\n  ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\n" ++
    "Use \"ownscout <command> --help\" for command details.";

pub const RunResult = struct { output: []u8, code: u8 };

fn has(args: []const []const u8, needle: []const u8) bool {
    for (args) |arg| if (std.mem.eql(u8, arg, needle)) return true;
    return false;
}

fn usage(allocator: std.mem.Allocator, args: []const []const u8) ![]u8 {
    var text: []const u8 = RootUsage;
    if (args.len >= 1) {
        if (std.mem.eql(u8, args[0], "doctor")) text = "Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments.";
        if (std.mem.eql(u8, args[0], "version")) text = "Usage: ownscout version\n\nPrints the OwnScout version.";
        if (std.mem.eql(u8, args[0], "contract")) text = "Usage: ownscout contract validate --packet <file> [--json]\n\nValidates packet structure and outcome rules.";
        if (std.mem.eql(u8, args[0], "evidence")) text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nVerifies packet evidence spans against a local repository.";
        if (std.mem.eql(u8, args[0], "node")) text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nVerifies a node-envelope-v1 graph against fresh repository evidence and records the ordered results.";
    }
    if (args.len >= 2 and std.mem.eql(u8, args[0], "contract") and std.mem.eql(u8, args[1], "validate")) text = "Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.";
    if (args.len >= 2 and std.mem.eql(u8, args[0], "evidence") and std.mem.eql(u8, args[1], "verify")) text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nValidates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun.";
    if (args.len >= 2 and std.mem.eql(u8, args[0], "node") and std.mem.eql(u8, args[1], "verify")) text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nStrictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once.\n\nNext action: provide all four paths and rerun.";
    return try std.fmt.allocPrint(allocator, "{s}\n", .{text});
}

fn fail(allocator: std.mem.Allocator, message: []const u8, next: []const u8, json_output: bool) !RunResult {
    return .{ .output = try result.usage(allocator, message, next, json_output), .code = 2 };
}

pub fn run(allocator: std.mem.Allocator, args: []const []const u8) !RunResult {
    if (args.len == 0) {
        const prefix = "error: a command is required\n\n";
        return .{ .output = try std.fmt.allocPrint(allocator, "{s}{s}\n\nNext action: run 'ownscout --help'.\n", .{ prefix, RootUsage }), .code = 2 };
    }
    if (has(args, "--help") or has(args, "-h")) return .{ .output = try usage(allocator, args), .code = 0 };
    if (std.mem.eql(u8, args[0], "doctor")) {
        if (args.len != 1) return fail(allocator, "doctor does not accept arguments", "ownscout doctor --help", false);
        return .{ .output = try std.fmt.allocPrint(allocator, "OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.\n", .{}), .code = 0 };
    }
    if (std.mem.eql(u8, args[0], "version")) {
        if (args.len != 1) return fail(allocator, "version does not accept arguments", "ownscout version --help", false);
        return .{ .output = try std.fmt.allocPrint(allocator, "ownscout 0.1.0\n", .{}), .code = 0 };
    }
    if (std.mem.eql(u8, args[0], "contract")) return subcommand(allocator, args[1..], "contract");
    if (std.mem.eql(u8, args[0], "evidence")) return subcommand(allocator, args[1..], "evidence");
    if (std.mem.eql(u8, args[0], "node")) return subcommand(allocator, args[1..], "node");
    return fail(allocator, try std.fmt.allocPrint(allocator, "unknown command '{s}'", .{args[0]}), "ownscout --help", false);
}

fn subcommand(allocator: std.mem.Allocator, args: []const []const u8, name: []const u8) !RunResult {
    const json_output = has(args, "--json");
    if (args.len == 0) {
        var next = std.ArrayList(u8).empty;
        try next.appendSlice(allocator, "ownscout ");
        try next.appendSlice(allocator, name);
        try next.appendSlice(allocator, " --help");
        return fail(allocator, if (std.mem.eql(u8, name, "contract")) "a contract subcommand is required" else if (std.mem.eql(u8, name, "evidence")) "an evidence subcommand is required" else "a node subcommand is required", try next.toOwnedSlice(allocator), false);
    }
    const wanted = if (std.mem.eql(u8, name, "contract")) "validate" else "verify";
    if (!std.mem.eql(u8, args[0], wanted)) {
        var message = std.ArrayList(u8).empty;
        try message.appendSlice(allocator, "unknown ");
        try message.appendSlice(allocator, name);
        try message.appendSlice(allocator, " subcommand '");
        try message.appendSlice(allocator, args[0]);
        try message.append(allocator, '\'');
        var next = std.ArrayList(u8).empty;
        try next.appendSlice(allocator, "ownscout ");
        try next.appendSlice(allocator, name);
        try next.appendSlice(allocator, " --help");
        return fail(allocator, try message.toOwnedSlice(allocator), try next.toOwnedSlice(allocator), json_output);
    }
    if (std.mem.eql(u8, name, "contract")) return contractValidate(allocator, args[1..], json_output);
    if (std.mem.eql(u8, name, "evidence")) return evidenceVerify(allocator, args[1..], json_output);
    return nodeVerify(allocator, args[1..], json_output);
}

const LoadError = error{NotFound, ReadFailed, InvalidJson, ObjectRequired, UnknownField, WrongType};

fn normalizeJsonUtf8(allocator: std.mem.Allocator, data: []const u8) ![]const u8 {
    if (std.unicode.utf8ValidateSlice(data)) return data;
    var normalized: std.ArrayList(u8) = .empty;
    var i: usize = 0;
    while (i < data.len) {
        const length = std.unicode.utf8ByteSequenceLength(data[i]) catch 1;
        if (length <= data.len - i and std.unicode.utf8ValidateSlice(data[i .. i + length])) {
            try normalized.appendSlice(allocator, data[i .. i + length]);
            i += length;
        } else {
            try normalized.appendSlice(allocator, "\xef\xbf\xbd");
            i += 1;
        }
    }
    return try normalized.toOwnedSlice(allocator);
}

fn unknownPacketField(root: json.Value) ?[]const u8 {
    if (root != .object) return null;
    for (root.object) |field| {
        // This scanner only runs for the contract/evidence path, which folds
        // field names, so a case-varied spelling of a known field is known.
        const canonical = topKeyMatch(field.key, true) orelse return field.key;
        const nested_members: []const Member = if (std.mem.eql(u8, canonical, "freshness"))
            &freshness_members
        else if (std.mem.eql(u8, canonical, "authorization"))
            &authorization_members
        else if (std.mem.eql(u8, canonical, "budget"))
            &budget_members
        else if (std.mem.eql(u8, canonical, "provenance"))
            &provenance_members
        else
            &[_]Member{};
        if (field.value == .object) {
            for (field.value.object) |nested| {
                if (memberSpec(nested_members, nested.key, true) == null) return nested.key;
            }
        }
        if (std.mem.eql(u8, canonical, "evidence") and field.value == .array) {
            for (field.value.array) |item| {
                if (item != .object) continue;
                for (item.object) |nested| {
                    if (memberSpec(&evidence_members, nested.key, true) == null) return nested.key;
                }
            }
        }
    }
    return null;
}

fn loadPacket(allocator: std.mem.Allocator, path: []const u8) LoadError!contract.Packet {
    const data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20)) catch |err| switch (err) {
        error.FileNotFound => return error.NotFound,
        else => return error.ReadFailed,
    };
    const trimmed = std.mem.trim(u8, data, " \t\r\n");
    const normalized = normalizeJsonUtf8(allocator, trimmed) catch return error.InvalidJson;
    const root = json.Parser.parseAllowDuplicateKeys(allocator, normalized) catch return error.InvalidJson;
    if (trimmed.len == 0 or trimmed[0] != '{') return error.ObjectRequired;
    // The contract/evidence path decodes through encoding/json, which matches a
    // field name exactly first and case-insensitively (Unicode simple fold)
    // second. Only the matching is folded - messages keep the input spelling,
    // which is what the reference prints.
    //
    // The reference reports whichever decode problem comes first in the
    // document, so the ordered walk decides the error kind. The message itself
    // is still produced by the same scanners as before.
    switch (firstIssueKind(root, true)) {
        .unknown => return error.UnknownField,
        .wrong_type => return error.WrongType,
        .none => {},
    }
    return contract.decodePacket(allocator, root) catch return error.WrongType;
}

// ---------------------------------------------------------------------------
// Strict node packet decoding (internal/nodepacket/decode.go).
//
// node verify does not share encoding/json's leniency. The raw bytes must be
// valid UTF-8, surrogate escapes must be well formed, field names must match
// exactly (no case folding), duplicate keys and explicit null are rejected, and
// integer fields must be integral and in range. Every failure is reported to
// the CLI as one generic "strict packet decoding failed".
// ---------------------------------------------------------------------------

fn hexEscape(data: []const u8) ?u16 {
    if (data.len < 4) return null;
    var value: u16 = 0;
    for (data[0..4]) |digit| {
        value <<= 4;
        if (digit >= '0' and digit <= '9') {
            value |= digit - '0';
        } else if (digit >= 'a' and digit <= 'f') {
            value |= digit - 'a' + 10;
        } else if (digit >= 'A' and digit <= 'F') {
            value |= digit - 'A' + 10;
        } else {
            return null;
        }
    }
    return value;
}

/// Mirrors nodepacket.validUnicodeEscapes: every \uXXXX must be present and
/// every surrogate must be part of a valid pair. encoding/json would instead
/// substitute U+FFFD, which is why node verify checks the raw bytes itself.
fn validUnicodeEscapes(data: []const u8) bool {
    var index: usize = 0;
    while (index < data.len) : (index += 1) {
        if (data[index] != '\\') continue;
        index += 1;
        if (index >= data.len or data[index] != 'u') continue;
        const value = hexEscape(data[index + 1 ..]) orelse return false;
        index += 4;
        if (value >= 0xdc00 and value <= 0xdfff) return false;
        if (value >= 0xd800 and value <= 0xdbff) {
            if (index + 6 >= data.len or data[index + 1] != '\\' or data[index + 2] != 'u') return false;
            const low = hexEscape(data[index + 3 ..]) orelse return false;
            if (low < 0xdc00 or low > 0xdfff) return false;
            index += 6;
        }
    }
    return true;
}

fn isIntegerToken(number: []const u8) bool {
    _ = std.fmt.parseInt(i64, number, 10) catch return false;
    return true;
}

fn strictTypeMatches(value: json.Value, kind: ValueKind) bool {
    return switch (kind) {
        .string => value == .string,
        .boolean => value == .boolean,
        .integer => value == .number and isIntegerToken(value.number),
    };
}

fn strictCheckStruct(value: json.Value, members: []const Member) error{WrongType}!void {
    if (value != .object) return error.WrongType;
    for (value.object) |field| {
        const member = memberSpec(members, field.key, false) orelse return error.WrongType;
        if (!strictTypeMatches(field.value, member.kind)) return error.WrongType;
    }
}

fn strictCheckPacket(root: json.Value) error{WrongType}!void {
    if (root != .object) return error.WrongType;
    for (root.object) |field| {
        const canonical = topKeyMatch(field.key, false) orelse return error.WrongType;
        if (isStringTopField(canonical)) {
            if (field.value != .string) return error.WrongType;
        } else if (std.mem.eql(u8, canonical, "freshness")) {
            try strictCheckStruct(field.value, &freshness_members);
        } else if (std.mem.eql(u8, canonical, "authorization")) {
            try strictCheckStruct(field.value, &authorization_members);
        } else if (std.mem.eql(u8, canonical, "budget")) {
            try strictCheckStruct(field.value, &budget_members);
        } else if (std.mem.eql(u8, canonical, "provenance")) {
            try strictCheckStruct(field.value, &provenance_members);
        } else if (std.mem.eql(u8, canonical, "evidence")) {
            if (field.value != .array) return error.WrongType;
            for (field.value.array) |item| try strictCheckStruct(item, &evidence_members);
        } else if (std.mem.eql(u8, canonical, "degradations")) {
            if (field.value != .array) return error.WrongType;
            for (field.value.array) |item| {
                if (item != .string) return error.WrongType;
            }
        } else {
            return error.WrongType;
        }
    }
}

fn loadNodePacket(allocator: std.mem.Allocator, path: []const u8) LoadError!contract.Packet {
    const data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20)) catch |err| switch (err) {
        error.FileNotFound => return error.NotFound,
        else => return error.ReadFailed,
    };
    if (!std.unicode.utf8ValidateSlice(data)) return error.WrongType;
    if (!validUnicodeEscapes(data)) return error.WrongType;
    const trimmed = std.mem.trim(u8, data, " \t\r\n");
    // parse (not parseAllowDuplicateKeys) rejects duplicate keys, invalid
    // escapes and trailing data before the fixed contract graph is checked.
    const root = json.Parser.parse(allocator, trimmed) catch return error.WrongType;
    if (root != .object) return error.WrongType;
    strictCheckPacket(root) catch return error.WrongType;
    return contract.decodePacket(allocator, root) catch return error.WrongType;
}

fn unknownPacketFieldFromFile(allocator: std.mem.Allocator, path: []const u8) ?[]const u8 {
    const data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20)) catch return null;
    const trimmed = std.mem.trim(u8, data, " \t\r\n");
    const normalized = normalizeJsonUtf8(allocator, trimmed) catch return null;
    const root = json.Parser.parseAllowDuplicateKeys(allocator, normalized) catch return null;
    return unknownPacketField(root);
}


const is_print16 = [_]u16{
    0x0020, 0x007e, 0x00a1, 0x0377, 0x037a, 0x037f, 0x0384, 0x0556, 0x0559, 0x058a, 0x058d, 0x05c7, 0x05d0, 0x05ea,
    0x05ef, 0x05f4, 0x0606, 0x070d, 0x0710, 0x074a, 0x074d, 0x07b1, 0x07c0, 0x07fa, 0x07fd, 0x082d, 0x0830, 0x085b,
    0x085e, 0x086a, 0x0870, 0x088f, 0x0897, 0x098c, 0x098f, 0x0990, 0x0993, 0x09b2, 0x09b6, 0x09b9, 0x09bc, 0x09c4,
    0x09c7, 0x09c8, 0x09cb, 0x09ce, 0x09d7, 0x09d7, 0x09dc, 0x09e3, 0x09e6, 0x09fe, 0x0a01, 0x0a0a, 0x0a0f, 0x0a10,
    0x0a13, 0x0a39, 0x0a3c, 0x0a42, 0x0a47, 0x0a48, 0x0a4b, 0x0a4d, 0x0a51, 0x0a51, 0x0a59, 0x0a5e, 0x0a66, 0x0a76,
    0x0a81, 0x0ab9, 0x0abc, 0x0acd, 0x0ad0, 0x0ad0, 0x0ae0, 0x0ae3, 0x0ae6, 0x0af1, 0x0af9, 0x0b0c, 0x0b0f, 0x0b10,
    0x0b13, 0x0b39, 0x0b3c, 0x0b44, 0x0b47, 0x0b48, 0x0b4b, 0x0b4d, 0x0b55, 0x0b57, 0x0b5c, 0x0b63, 0x0b66, 0x0b77,
    0x0b82, 0x0b8a, 0x0b8e, 0x0b95, 0x0b99, 0x0b9f, 0x0ba3, 0x0ba4, 0x0ba8, 0x0baa, 0x0bae, 0x0bb9, 0x0bbe, 0x0bc2,
    0x0bc6, 0x0bcd, 0x0bd0, 0x0bd0, 0x0bd7, 0x0bd7, 0x0be6, 0x0bfa, 0x0c00, 0x0c39, 0x0c3c, 0x0c4d, 0x0c55, 0x0c5d,
    0x0c60, 0x0c63, 0x0c66, 0x0c6f, 0x0c77, 0x0cb9, 0x0cbc, 0x0ccd, 0x0cd5, 0x0cd6, 0x0cdc, 0x0ce3, 0x0ce6, 0x0cf3,
    0x0d00, 0x0d4f, 0x0d54, 0x0d63, 0x0d66, 0x0d96, 0x0d9a, 0x0dbd, 0x0dc0, 0x0dc6, 0x0dca, 0x0dca, 0x0dcf, 0x0ddf,
    0x0de6, 0x0def, 0x0df2, 0x0df4, 0x0e01, 0x0e3a, 0x0e3f, 0x0e5b, 0x0e81, 0x0ebd, 0x0ec0, 0x0ed9, 0x0edc, 0x0edf,
    0x0f00, 0x0f6c, 0x0f71, 0x0fda, 0x1000, 0x10c7, 0x10cd, 0x10cd, 0x10d0, 0x124d, 0x1250, 0x125d, 0x1260, 0x128d,
    0x1290, 0x12b5, 0x12b8, 0x12c5, 0x12c8, 0x1315, 0x1318, 0x135a, 0x135d, 0x137c, 0x1380, 0x1399, 0x13a0, 0x13f5,
    0x13f8, 0x13fd, 0x1400, 0x169c, 0x16a0, 0x16f8, 0x1700, 0x1715, 0x171f, 0x1736, 0x1740, 0x1753, 0x1760, 0x1773,
    0x1780, 0x17dd, 0x17e0, 0x17e9, 0x17f0, 0x17f9, 0x1800, 0x1819, 0x1820, 0x1878, 0x1880, 0x18aa, 0x18b0, 0x18f5,
    0x1900, 0x192b, 0x1930, 0x193b, 0x1940, 0x1940, 0x1944, 0x196d, 0x1970, 0x1974, 0x1980, 0x19ab, 0x19b0, 0x19c9,
    0x19d0, 0x19da, 0x19de, 0x1a1b, 0x1a1e, 0x1a7c, 0x1a7f, 0x1a89, 0x1a90, 0x1a99, 0x1aa0, 0x1aad, 0x1ab0, 0x1add,
    0x1ae0, 0x1aeb, 0x1b00, 0x1bf3, 0x1bfc, 0x1c37, 0x1c3b, 0x1c49, 0x1c4d, 0x1c8a, 0x1c90, 0x1cba, 0x1cbd, 0x1cc7,
    0x1cd0, 0x1cfa, 0x1d00, 0x1f15, 0x1f18, 0x1f1d, 0x1f20, 0x1f45, 0x1f48, 0x1f4d, 0x1f50, 0x1f7d, 0x1f80, 0x1fd3,
    0x1fd6, 0x1fef, 0x1ff2, 0x1ffe, 0x2010, 0x2027, 0x2030, 0x205e, 0x2070, 0x2071, 0x2074, 0x209c, 0x20a0, 0x20c1,
    0x20d0, 0x20f0, 0x2100, 0x218b, 0x2190, 0x2429, 0x2440, 0x244a, 0x2460, 0x2b73, 0x2b76, 0x2cf3, 0x2cf9, 0x2d27,
    0x2d2d, 0x2d2d, 0x2d30, 0x2d67, 0x2d6f, 0x2d70, 0x2d7f, 0x2d96, 0x2da0, 0x2e5d, 0x2e80, 0x2ef3, 0x2f00, 0x2fd5,
    0x2ff0, 0x3096, 0x3099, 0x30ff, 0x3105, 0x31e5, 0x31ef, 0xa48c, 0xa490, 0xa4c6, 0xa4d0, 0xa62b, 0xa640, 0xa6f7,
    0xa700, 0xa7dc, 0xa7f1, 0xa82c, 0xa830, 0xa839, 0xa840, 0xa877, 0xa880, 0xa8c5, 0xa8ce, 0xa8d9, 0xa8e0, 0xa953,
    0xa95f, 0xa97c, 0xa980, 0xa9d9, 0xa9de, 0xaa36, 0xaa40, 0xaa4d, 0xaa50, 0xaa59, 0xaa5c, 0xaac2, 0xaadb, 0xaaf6,
    0xab01, 0xab06, 0xab09, 0xab0e, 0xab11, 0xab16, 0xab20, 0xab6b, 0xab70, 0xabed, 0xabf0, 0xabf9, 0xac00, 0xd7a3,
    0xd7b0, 0xd7c6, 0xd7cb, 0xd7fb, 0xf900, 0xfa6d, 0xfa70, 0xfad9, 0xfb00, 0xfb06, 0xfb13, 0xfb17, 0xfb1d, 0xfdcf,
    0xfdf0, 0xfe19, 0xfe20, 0xfe6b, 0xfe70, 0xfefc, 0xff01, 0xffbe, 0xffc2, 0xffc7, 0xffca, 0xffcf, 0xffd2, 0xffd7,
    0xffda, 0xffdc, 0xffe0, 0xffee, 0xfffc, 0xfffd,
};
const is_not_print16 = [_]u16{
    0x00ad, 0x038b, 0x038d, 0x03a2, 0x0530, 0x0590, 0x061c, 0x06dd, 0x083f, 0x085f, 0x08e2, 0x0984, 0x09a9, 0x09b1,
    0x09de, 0x0a04, 0x0a29, 0x0a31, 0x0a34, 0x0a37, 0x0a3d, 0x0a5d, 0x0a84, 0x0a8e, 0x0a92, 0x0aa9, 0x0ab1, 0x0ab4,
    0x0ac6, 0x0aca, 0x0b00, 0x0b04, 0x0b29, 0x0b31, 0x0b34, 0x0b5e, 0x0b84, 0x0b91, 0x0b9b, 0x0b9d, 0x0bc9, 0x0c0d,
    0x0c11, 0x0c29, 0x0c45, 0x0c49, 0x0c57, 0x0c5b, 0x0c8d, 0x0c91, 0x0ca9, 0x0cb4, 0x0cc5, 0x0cc9, 0x0cdf, 0x0cf0,
    0x0d0d, 0x0d11, 0x0d45, 0x0d49, 0x0d80, 0x0d84, 0x0db2, 0x0dbc, 0x0dd5, 0x0dd7, 0x0e83, 0x0e85, 0x0e8b, 0x0ea4,
    0x0ea6, 0x0ec5, 0x0ec7, 0x0ecf, 0x0f48, 0x0f98, 0x0fbd, 0x0fcd, 0x10c6, 0x1249, 0x1257, 0x1259, 0x1289, 0x12b1,
    0x12bf, 0x12c1, 0x12d7, 0x1311, 0x1680, 0x176d, 0x1771, 0x180e, 0x191f, 0x1a5f, 0x1b4d, 0x1f58, 0x1f5a, 0x1f5c,
    0x1f5e, 0x1fb5, 0x1fc5, 0x1fdc, 0x1ff5, 0x208f, 0x2d26, 0x2da7, 0x2daf, 0x2db7, 0x2dbf, 0x2dc7, 0x2dcf, 0x2dd7,
    0x2ddf, 0x2e9a, 0x3000, 0x3040, 0x3130, 0x318f, 0x321f, 0xa9ce, 0xa9ff, 0xab27, 0xab2f, 0xfb37, 0xfb3d, 0xfb3f,
    0xfb42, 0xfb45, 0xfe53, 0xfe67, 0xfe75, 0xffe7,
};
const is_print32 = [_]u32{
    0x10000, 0x1004d, 0x10050, 0x1005d, 0x10080, 0x100fa, 0x10100, 0x10102, 0x10107, 0x10133, 0x10137, 0x1019c,
    0x101a0, 0x101a0, 0x101d0, 0x101fd, 0x10280, 0x1029c, 0x102a0, 0x102d0, 0x102e0, 0x102fb, 0x10300, 0x10323,
    0x1032d, 0x1034a, 0x10350, 0x1037a, 0x10380, 0x103c3, 0x103c8, 0x103d5, 0x10400, 0x1049d, 0x104a0, 0x104a9,
    0x104b0, 0x104d3, 0x104d8, 0x104fb, 0x10500, 0x10527, 0x10530, 0x10563, 0x1056f, 0x105bc, 0x105c0, 0x105f3,
    0x10600, 0x10736, 0x10740, 0x10755, 0x10760, 0x10767, 0x10780, 0x107ba, 0x10800, 0x10805, 0x10808, 0x10838,
    0x1083c, 0x1083c, 0x1083f, 0x1089e, 0x108a7, 0x108af, 0x108e0, 0x108f5, 0x108fb, 0x1091b, 0x1091f, 0x10939,
    0x1093f, 0x10959, 0x10980, 0x109b7, 0x109bc, 0x109cf, 0x109d2, 0x10a06, 0x10a0c, 0x10a35, 0x10a38, 0x10a3a,
    0x10a3f, 0x10a48, 0x10a50, 0x10a58, 0x10a60, 0x10a9f, 0x10ac0, 0x10ae6, 0x10aeb, 0x10af6, 0x10b00, 0x10b35,
    0x10b39, 0x10b55, 0x10b58, 0x10b72, 0x10b78, 0x10b91, 0x10b99, 0x10b9c, 0x10ba9, 0x10baf, 0x10c00, 0x10c48,
    0x10c80, 0x10cb2, 0x10cc0, 0x10cf2, 0x10cfa, 0x10d27, 0x10d30, 0x10d39, 0x10d40, 0x10d65, 0x10d69, 0x10d85,
    0x10d8e, 0x10d8f, 0x10e60, 0x10ead, 0x10eb0, 0x10eb1, 0x10ec2, 0x10ec7, 0x10ed0, 0x10ed8, 0x10efa, 0x10f27,
    0x10f30, 0x10f59, 0x10f70, 0x10f89, 0x10fb0, 0x10fcb, 0x10fe0, 0x10ff6, 0x11000, 0x1104d, 0x11052, 0x11075,
    0x1107f, 0x110c2, 0x110d0, 0x110e8, 0x110f0, 0x110f9, 0x11100, 0x11147, 0x11150, 0x11176, 0x11180, 0x111f4,
    0x11200, 0x11241, 0x11280, 0x112a9, 0x112b0, 0x112ea, 0x112f0, 0x112f9, 0x11300, 0x1130c, 0x1130f, 0x11310,
    0x11313, 0x11344, 0x11347, 0x11348, 0x1134b, 0x1134d, 0x11350, 0x11350, 0x11357, 0x11357, 0x1135d, 0x11363,
    0x11366, 0x1136c, 0x11370, 0x11374, 0x11380, 0x1138b, 0x1138e, 0x113c2, 0x113c5, 0x113d8, 0x113e1, 0x113e2,
    0x11400, 0x11461, 0x11480, 0x114c7, 0x114d0, 0x114d9, 0x11580, 0x115b5, 0x115b8, 0x115dd, 0x11600, 0x11644,
    0x11650, 0x11659, 0x11660, 0x1166c, 0x11680, 0x116b9, 0x116c0, 0x116c9, 0x116d0, 0x116e3, 0x11700, 0x1171a,
    0x1171d, 0x1172b, 0x11730, 0x11746, 0x11800, 0x1183b, 0x118a0, 0x118f2, 0x118ff, 0x11906, 0x11909, 0x11909,
    0x1190c, 0x11938, 0x1193b, 0x11946, 0x11950, 0x11959, 0x119a0, 0x119a7, 0x119aa, 0x119d7, 0x119da, 0x119e4,
    0x11a00, 0x11a47, 0x11a50, 0x11aa2, 0x11ab0, 0x11af8, 0x11b00, 0x11b09, 0x11b60, 0x11b67, 0x11bc0, 0x11be1,
    0x11bf0, 0x11bf9, 0x11c00, 0x11c45, 0x11c50, 0x11c6c, 0x11c70, 0x11c8f, 0x11c92, 0x11cb6, 0x11d00, 0x11d36,
    0x11d3a, 0x11d47, 0x11d50, 0x11d59, 0x11d60, 0x11d98, 0x11da0, 0x11da9, 0x11db0, 0x11ddb, 0x11de0, 0x11de9,
    0x11ee0, 0x11ef8, 0x11f00, 0x11f3a, 0x11f3e, 0x11f5a, 0x11fb0, 0x11fb0, 0x11fc0, 0x11ff1, 0x11fff, 0x12399,
    0x12400, 0x12474, 0x12480, 0x12543, 0x12f90, 0x12ff2, 0x13000, 0x1342f, 0x13440, 0x13455, 0x13460, 0x143fa,
    0x14400, 0x14646, 0x16100, 0x16139, 0x16800, 0x16a38, 0x16a40, 0x16a69, 0x16a6e, 0x16ac9, 0x16ad0, 0x16aed,
    0x16af0, 0x16af5, 0x16b00, 0x16b45, 0x16b50, 0x16b77, 0x16b7d, 0x16b8f, 0x16d40, 0x16d79, 0x16e40, 0x16e9a,
    0x16ea0, 0x16eb8, 0x16ebb, 0x16ed3, 0x16f00, 0x16f4a, 0x16f4f, 0x16f87, 0x16f8f, 0x16f9f, 0x16fe0, 0x16fe4,
    0x16ff0, 0x16ff6, 0x17000, 0x18cd5, 0x18cff, 0x18d1e, 0x18d80, 0x18df2, 0x1aff0, 0x1b122, 0x1b132, 0x1b132,
    0x1b150, 0x1b152, 0x1b155, 0x1b155, 0x1b164, 0x1b167, 0x1b170, 0x1b2fb, 0x1bc00, 0x1bc6a, 0x1bc70, 0x1bc7c,
    0x1bc80, 0x1bc88, 0x1bc90, 0x1bc99, 0x1bc9c, 0x1bc9f, 0x1cc00, 0x1ccfc, 0x1cd00, 0x1ceb3, 0x1ceba, 0x1ced0,
    0x1cee0, 0x1cef0, 0x1cf00, 0x1cf2d, 0x1cf30, 0x1cf46, 0x1cf50, 0x1cfc3, 0x1d000, 0x1d0f5, 0x1d100, 0x1d126,
    0x1d129, 0x1d172, 0x1d17b, 0x1d1ea, 0x1d200, 0x1d245, 0x1d2c0, 0x1d2d3, 0x1d2e0, 0x1d2f3, 0x1d300, 0x1d356,
    0x1d360, 0x1d378, 0x1d400, 0x1d49f, 0x1d4a2, 0x1d4a2, 0x1d4a5, 0x1d4a6, 0x1d4a9, 0x1d50a, 0x1d50d, 0x1d546,
    0x1d54a, 0x1d6a5, 0x1d6a8, 0x1d7cb, 0x1d7ce, 0x1da8b, 0x1da9b, 0x1daaf, 0x1df00, 0x1df1e, 0x1df25, 0x1df2a,
    0x1e000, 0x1e018, 0x1e01b, 0x1e02a, 0x1e030, 0x1e06d, 0x1e08f, 0x1e08f, 0x1e100, 0x1e12c, 0x1e130, 0x1e13d,
    0x1e140, 0x1e149, 0x1e14e, 0x1e14f, 0x1e290, 0x1e2ae, 0x1e2c0, 0x1e2f9, 0x1e2ff, 0x1e2ff, 0x1e4d0, 0x1e4f9,
    0x1e5d0, 0x1e5fa, 0x1e5ff, 0x1e5ff, 0x1e6c0, 0x1e6f5, 0x1e6fe, 0x1e6ff, 0x1e7e0, 0x1e8c4, 0x1e8c7, 0x1e8d6,
    0x1e900, 0x1e94b, 0x1e950, 0x1e959, 0x1e95e, 0x1e95f, 0x1ec71, 0x1ecb4, 0x1ed01, 0x1ed3d, 0x1ee00, 0x1ee24,
    0x1ee27, 0x1ee3b, 0x1ee42, 0x1ee42, 0x1ee47, 0x1ee54, 0x1ee57, 0x1ee64, 0x1ee67, 0x1ee9b, 0x1eea1, 0x1eebb,
    0x1eef0, 0x1eef1, 0x1f000, 0x1f02b, 0x1f030, 0x1f093, 0x1f0a0, 0x1f0ae, 0x1f0b1, 0x1f0f5, 0x1f100, 0x1f1ad,
    0x1f1e6, 0x1f202, 0x1f210, 0x1f23b, 0x1f240, 0x1f248, 0x1f250, 0x1f251, 0x1f260, 0x1f265, 0x1f300, 0x1f6d8,
    0x1f6dc, 0x1f6ec, 0x1f6f0, 0x1f6fc, 0x1f700, 0x1f7d9, 0x1f7e0, 0x1f7eb, 0x1f7f0, 0x1f7f0, 0x1f800, 0x1f80b,
    0x1f810, 0x1f847, 0x1f850, 0x1f859, 0x1f860, 0x1f887, 0x1f890, 0x1f8ad, 0x1f8b0, 0x1f8bb, 0x1f8c0, 0x1f8c1,
    0x1f8d0, 0x1f8d8, 0x1f900, 0x1fa57, 0x1fa60, 0x1fa6d, 0x1fa70, 0x1fa7c, 0x1fa80, 0x1fa8a, 0x1fa8e, 0x1fac8,
    0x1facd, 0x1fadc, 0x1fadf, 0x1faea, 0x1faef, 0x1faf8, 0x1fb00, 0x1fbfa, 0x20000, 0x2a6df, 0x2a700, 0x2b81d,
    0x2b820, 0x2cead, 0x2ceb0, 0x2ebe0, 0x2ebf0, 0x2ee5d, 0x2f800, 0x2fa1d, 0x30000, 0x3134a, 0x31350, 0x33479,
    0xe0100, 0xe01ef,
};
const is_not_print32 = [_]u16{
    0x000c, 0x0027, 0x003b, 0x003e, 0x018f, 0x039e, 0x057b, 0x058b, 0x0593, 0x0596, 0x05a2, 0x05b2, 0x05ba, 0x0786,
    0x07b1, 0x0809, 0x0836, 0x0856, 0x08f3, 0x0a04, 0x0a14, 0x0a18, 0x0e7f, 0x0eaa, 0x10bd, 0x1135, 0x11e0, 0x1212,
    0x1287, 0x1289, 0x128e, 0x129e, 0x1304, 0x1329, 0x1331, 0x1334, 0x133a, 0x138a, 0x138f, 0x13b6, 0x13c1, 0x13c6,
    0x13cb, 0x13d6, 0x145c, 0x1914, 0x1917, 0x1936, 0x1c09, 0x1c37, 0x1ca8, 0x1d07, 0x1d0a, 0x1d3b, 0x1d3e, 0x1d66,
    0x1d69, 0x1d8f, 0x1d92, 0x1f11, 0x246f, 0x6a5f, 0x6abf, 0x6b5a, 0x6b62, 0xaff4, 0xaffc, 0xafff, 0xd455, 0xd49d,
    0xd4ad, 0xd4ba, 0xd4bc, 0xd4c4, 0xd506, 0xd515, 0xd51d, 0xd53a, 0xd53f, 0xd545, 0xd551, 0xdaa0, 0xe007, 0xe022,
    0xe025, 0xe6df, 0xe7e7, 0xe7ec, 0xe7ef, 0xe7ff, 0xee04, 0xee20, 0xee23, 0xee28, 0xee33, 0xee38, 0xee3a, 0xee48,
    0xee4a, 0xee4c, 0xee50, 0xee53, 0xee58, 0xee5a, 0xee5c, 0xee5e, 0xee60, 0xee63, 0xee6b, 0xee73, 0xee78, 0xee7d,
    0xee7f, 0xee8a, 0xeea4, 0xeeaa, 0xf0c0, 0xf0d0, 0xfac7, 0xfb93,
};

// ---------------------------------------------------------------------------
// Go-compatible decode diagnostics.
//
// The reference decodes with encoding/json and DisallowUnknownFields, which
// walks each object's keys in document order and recurses. The FIRST problem -
// an unknown field or a wrongly-typed value - is therefore whichever appears
// first in the input. firstIssueKind mirrors that walk so loadPacket reports
// the same error the reference would.
// ---------------------------------------------------------------------------

const ValueKind = enum { string, boolean, integer };
const Member = struct { name: []const u8, kind: ValueKind };

const packet_string_fields = [_][]const u8{
    "packet_id", "schema_version", "repo_root", "head_commit", "request_id", "issued_at", "outcome", "packet_hash",
};
const freshness_members = [_]Member{
    .{ .name = "head_commit", .kind = .string },  .{ .name = "head_anchor", .kind = .string },
    .{ .name = "status", .kind = .string },       .{ .name = "current", .kind = .boolean },
    .{ .name = "is_current", .kind = .boolean },  .{ .name = "checked_at", .kind = .string },
};
const authorization_members = [_]Member{
    .{ .name = "level", .kind = .string }, .{ .name = "reason", .kind = .string },
};
const budget_members = [_]Member{
    .{ .name = "max_evidence", .kind = .integer }, .{ .name = "used_evidence", .kind = .integer },
    .{ .name = "max_bytes", .kind = .integer },    .{ .name = "used_bytes", .kind = .integer },
};
const provenance_members = [_]Member{
    .{ .name = "collector", .kind = .string }, .{ .name = "tool", .kind = .string },
    .{ .name = "version", .kind = .string },   .{ .name = "tool_version", .kind = .string },
};
const evidence_members = [_]Member{
    .{ .name = "evidence_id", .kind = .string },  .{ .name = "kind", .kind = .string },
    .{ .name = "path", .kind = .string },         .{ .name = "commit", .kind = .string },
    .{ .name = "line_start", .kind = .integer },  .{ .name = "line_end", .kind = .integer },
    .{ .name = "source", .kind = .string },       .{ .name = "content_hash", .kind = .string },
    .{ .name = "collected_at", .kind = .string }, .{ .name = "verifier_status", .kind = .string },
};

const DecodeIssue = enum { none, unknown, wrong_type };

fn nameMatches(a: []const u8, b: []const u8, fold: bool) bool {
    return if (fold) gofold.foldedEqual(a, b) else std.mem.eql(u8, a, b);
}

fn memberSpec(members: []const Member, name: []const u8, fold: bool) ?Member {
    for (members) |member| if (nameMatches(name, member.name, fold)) return member;
    return null;
}

// ---------------------------------------------------------------------------
// Case-insensitive field names (contract/evidence path only).
//
// encoding/json resolves a key by an exact tag match first and a case-folded
// match second, so the contract and evidence commands accept e.g.
// "SCHEMA_VERSION" for schema_version. internal/nodepacket, used by node
// verify, is exact only, so `fold` is false there.
//
// Only matching is folded: the reference still names the field with the input
// spelling in a type error ("Packet.PACKET_ID"), so keys are never rewritten.
// ---------------------------------------------------------------------------

const packet_struct_fields = [_][]const u8{
    "freshness", "authorization", "budget", "evidence", "degradations", "provenance",
};

fn isStringTopField(name: []const u8) bool {
    for (packet_string_fields) |field| if (std.mem.eql(u8, field, name)) return true;
    return false;
}

/// Resolve a top-level key to the canonical tag it matches, or null when it is
/// unknown. `fold` selects case-insensitive matching.
fn topKeyMatch(key: []const u8, fold: bool) ?[]const u8 {
    for (packet_string_fields) |name| if (nameMatches(key, name, fold)) return name;
    for (packet_struct_fields) |name| if (nameMatches(key, name, fold)) return name;
    return null;
}

fn scalarTypeIssue(value: json.Value, kind: ValueKind) bool {
    return switch (kind) {
        .string => value != .string and value != .null,
        .boolean => value != .boolean and value != .null,
        // encoding/json rejects a number that is not an integral int64, not
        // just a non-number, for an integer field.
        .integer => value != .null and (value != .number or !isIntegerToken(value.number)),
    };
}

fn firstIssueInStruct(value: json.Value, members: []const Member, fold: bool) DecodeIssue {
    if (value == .null) return .none;
    if (value != .object) return .wrong_type;
    for (value.object) |field| {
        const member = memberSpec(members, field.key, fold) orelse return .unknown;
        if (scalarTypeIssue(field.value, member.kind)) return .wrong_type;
    }
    return .none;
}

fn firstIssueKind(root: json.Value, fold: bool) DecodeIssue {
    if (root != .object) return .none;
    for (root.object) |field| {
        const canonical = topKeyMatch(field.key, fold) orelse return .unknown;
        if (isStringTopField(canonical)) {
            if (field.value != .string and field.value != .null) return .wrong_type;
        } else if (std.mem.eql(u8, canonical, "freshness")) {
            const issue = firstIssueInStruct(field.value, &freshness_members, fold);
            if (issue != .none) return issue;
        } else if (std.mem.eql(u8, canonical, "authorization")) {
            const issue = firstIssueInStruct(field.value, &authorization_members, fold);
            if (issue != .none) return issue;
        } else if (std.mem.eql(u8, canonical, "budget")) {
            const issue = firstIssueInStruct(field.value, &budget_members, fold);
            if (issue != .none) return issue;
        } else if (std.mem.eql(u8, canonical, "provenance")) {
            const issue = firstIssueInStruct(field.value, &provenance_members, fold);
            if (issue != .none) return issue;
        } else if (std.mem.eql(u8, canonical, "evidence")) {
            if (field.value == .null) continue;
            if (field.value != .array) return .wrong_type;
            for (field.value.array) |item| {
                if (item == .null) continue;
                if (item != .object) return .wrong_type;
                for (item.object) |nested| {
                    const member = memberSpec(&evidence_members, nested.key, fold) orelse return .unknown;
                    if (scalarTypeIssue(nested.value, member.kind)) return .wrong_type;
                }
            }
        } else if (std.mem.eql(u8, canonical, "degradations")) {
            if (field.value == .null) continue;
            if (field.value != .array) return .wrong_type;
            for (field.value.array) |item| {
                if (item == .null) continue;
                if (item != .string) return .wrong_type;
            }
        } else {
            return .unknown;
        }
    }
    return .none;
}

// ---------------------------------------------------------------------------
// Field-name quoting.
//
// encoding/json reports an unknown field with fmt's %q, i.e. strconv.Quote:
// '"' and '\' are backslashed, the C escapes cover the common controls, other
// bytes below 0x20 and DEL become \xNN, printable runes (strconv.IsPrint) stay
// literal, other BMP runes become \uNNNN and other astral runes \UNNNNNNNN.
// Hex digits are lowercase. The parser hands us a decoded UTF-8 key, so the key
// is walked rune by rune rather than byte by byte.
// ---------------------------------------------------------------------------

fn appendHexByte(out: *std.ArrayList(u8), allocator: std.mem.Allocator, byte: u8) !void {
    const digits = "0123456789abcdef";
    try out.appendSlice(allocator, "\\x");
    try out.append(allocator, digits[byte >> 4]);
    try out.append(allocator, digits[byte & 0x0f]);
}

fn appendRuneHex(out: *std.ArrayList(u8), allocator: std.mem.Allocator, prefix: []const u8, value: u21, comptime width: usize) !void {
    const digits = "0123456789abcdef";
    const wide: u32 = value;
    try out.appendSlice(allocator, prefix);
    var shift: usize = width * 4;
    while (shift > 0) {
        shift -= 4;
        try out.append(allocator, digits[(wide >> @intCast(shift)) & 0x0f]);
    }
}

fn appendQuotedRune(out: *std.ArrayList(u8), allocator: std.mem.Allocator, r: u21) !void {
    if (r == '"' or r == '\\') {
        try out.append(allocator, '\\');
        try out.append(allocator, @intCast(r));
        return;
    }
    if (strconvIsPrint(r)) {
        var encoded: [4]u8 = undefined;
        const length: usize = std.unicode.utf8Encode(r, &encoded) catch {
            try appendHexByte(out, allocator, @intCast(r));
            return;
        };
        try out.appendSlice(allocator, encoded[0..length]);
        return;
    }
    switch (r) {
        0x07 => try out.appendSlice(allocator, "\\a"),
        0x08 => try out.appendSlice(allocator, "\\b"),
        0x0c => try out.appendSlice(allocator, "\\f"),
        0x0a => try out.appendSlice(allocator, "\\n"),
        0x0d => try out.appendSlice(allocator, "\\r"),
        0x09 => try out.appendSlice(allocator, "\\t"),
        0x0b => try out.appendSlice(allocator, "\\v"),
        else => {
            if (r < 0x20 or r == 0x7f) {
                try appendHexByte(out, allocator, @intCast(r));
            } else if (r < 0x10000) {
                try appendRuneHex(out, allocator, "\\u", r, 4);
            } else {
                try appendRuneHex(out, allocator, "\\U", r, 8);
            }
        },
    }
}

fn quoteGoString(allocator: std.mem.Allocator, value: []const u8) ![]u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.append(allocator, '"');
    var index: usize = 0;
    while (index < value.len) {
        const byte = value[index];
        if (byte < 0x80) {
            try appendQuotedRune(&out, allocator, byte);
            index += 1;
            continue;
        }
        const length: usize = std.unicode.utf8ByteSequenceLength(byte) catch {
            try appendHexByte(&out, allocator, byte);
            index += 1;
            continue;
        };
        if (index + length > value.len or !std.unicode.utf8ValidateSlice(value[index .. index + length])) {
            try appendHexByte(&out, allocator, byte);
            index += 1;
            continue;
        }
        const decoded = std.unicode.utf8Decode(value[index .. index + length]) catch {
            try appendHexByte(&out, allocator, byte);
            index += 1;
            continue;
        };
        try appendQuotedRune(&out, allocator, decoded);
        index += length;
    }
    try out.append(allocator, '"');
    return try out.toOwnedSlice(allocator);
}

fn lowerBoundU16(values: []const u16, target: u16) usize {
    var low: usize = 0;
    var high: usize = values.len;
    while (low < high) {
        const mid = low + (high - low) / 2;
        if (values[mid] < target) low = mid + 1 else high = mid;
    }
    return low;
}

fn containsU16(values: []const u16, target: u16) bool {
    const index = lowerBoundU16(values, target);
    return index < values.len and values[index] == target;
}

fn lowerBoundU32(values: []const u32, target: u32) usize {
    var low: usize = 0;
    var high: usize = values.len;
    while (low < high) {
        const mid = low + (high - low) / 2;
        if (values[mid] < target) low = mid + 1 else high = mid;
    }
    return low;
}

/// Port of Go's strconv.IsPrint (strconv/isprint.go): the letters, marks,
/// numbers, punctuation and symbols of Unicode plus ASCII space. The Latin-1
/// range is special-cased and the rest comes from the same generated range
/// tables the reference uses.
fn strconvIsPrint(r: u21) bool {
    if (r <= 0xFF) {
        if (r >= 0x20 and r <= 0x7E) return true;
        if (r >= 0xA1 and r <= 0xFF) return r != 0xAD;
        return false;
    }
    if (r < 1 << 16) {
        const target: u16 = @intCast(r);
        const index = lowerBoundU16(&is_print16, target);
        if (index >= is_print16.len or target < is_print16[index & ~@as(usize, 1)] or is_print16[index | 1] < target) return false;
        return !containsU16(&is_not_print16, target);
    }
    const target: u32 = r;
    const index = lowerBoundU32(&is_print32, target);
    if (index >= is_print32.len or target < is_print32[index & ~@as(usize, 1)] or is_print32[index | 1] < target) return false;
    if (r >= 0x20000) return true;
    return !containsU16(&is_not_print32, @intCast(r - 0x10000));
}

fn jsonTypeName(value: json.Value) []const u8 {
    return switch (value) {
        .null => "null",
        .boolean => "bool",
        .number => "number",
        .string => "string",
        .array => "array",
        .object => "object",
    };
}

fn decodeFailureMessage(allocator: std.mem.Allocator, path: []const u8) ![]u8 {
    const data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20)) catch
        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{path});
    const trimmed = std.mem.trim(u8, data, " \t\r\n");
    const normalized = normalizeJsonUtf8(allocator, trimmed) catch
        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{path});
    const root = json.Parser.parseAllowDuplicateKeys(allocator, normalized) catch
        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{path});
    if (root == .object) {
        for (root.object) |field| {
            // Match case-insensitively, but print the key as the input spelled
            // it: encoding/json names the field it failed on that way.
            const canonical = topKeyMatch(field.key, true) orelse continue;
            if (isStringTopField(canonical) and field.value != .string) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type string", .{ path, jsonTypeName(field.value), field.key });
            }
            if (std.mem.eql(u8, canonical, "evidence") and field.value != .array) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type []contract.Evidence", .{ path, jsonTypeName(field.value), field.key });
            }
            if (std.mem.eql(u8, canonical, "degradations") and field.value != .array) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type []string", .{ path, jsonTypeName(field.value), field.key });
            }
            if ((std.mem.eql(u8, canonical, "freshness") or std.mem.eql(u8, canonical, "authorization") or
                std.mem.eql(u8, canonical, "budget") or std.mem.eql(u8, canonical, "provenance")) and
                field.value != .object and field.value != .null)
            {
                const type_name = if (std.mem.eql(u8, canonical, "freshness")) "contract.Freshness" else if (std.mem.eql(u8, canonical, "authorization")) "contract.Authorization" else if (std.mem.eql(u8, canonical, "budget")) "contract.Budget" else "contract.Provenance";
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type {s}", .{ path, jsonTypeName(field.value), field.key, type_name });
            }
            if (field.value == .object) {
                const nested_members: []const Member = if (std.mem.eql(u8, canonical, "freshness"))
                    &freshness_members
                else if (std.mem.eql(u8, canonical, "authorization"))
                    &authorization_members
                else if (std.mem.eql(u8, canonical, "budget"))
                    &budget_members
                else if (std.mem.eql(u8, canonical, "provenance"))
                    &provenance_members
                else
                    &[_]Member{};
                for (field.value.object) |nested| {
                    if (nested.value == .null) continue;
                    const member = memberSpec(nested_members, nested.key, true) orelse continue;
                    const is_bool = member.kind == .boolean;
                    const is_integer = member.kind == .integer;
                    if (is_bool and nested.value != .boolean) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type bool", .{ path, jsonTypeName(nested.value), field.key, nested.key });
                    }
                    if (is_integer and (nested.value != .number or !isIntegerToken(nested.value.number))) {
                        const type_name = if (std.mem.eql(u8, member.name, "max_bytes") or std.mem.eql(u8, member.name, "used_bytes")) "int64" else "int";
                        // A rejected number names its literal, as encoding/json
                        // does in UnmarshalTypeError.Value.
                        if (nested.value == .number) {
                            return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal number {s} into Go struct field Packet.{s}.{s} of type {s}", .{ path, nested.value.number, field.key, nested.key, type_name });
                        }
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type {s}", .{ path, jsonTypeName(nested.value), field.key, nested.key, type_name });
                    }
                    if (!is_bool and !is_integer and nested.value != .string) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type string", .{ path, jsonTypeName(nested.value), field.key, nested.key });
                    }
                }
            }
            if (std.mem.eql(u8, canonical, "evidence") and field.value == .array) {
                for (field.value.array, 0..) |item, i| {
                    if (item == .null) continue;
                    if (item != .object) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Packet.{s}.{d} of type contract.Evidence", .{ path, jsonTypeName(item), field.key, i });
                    }
                    for (item.object) |nested| {
                        if (nested.value == .null) continue;
                        const member = memberSpec(&evidence_members, nested.key, true) orelse continue;
                        const numeric = member.kind == .integer;
                        if (numeric and (nested.value != .number or !isIntegerToken(nested.value.number))) {
                            if (nested.value == .number) {
                                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal number {s} into Go struct field Packet.{s}.{d}.{s} of type int", .{ path, nested.value.number, field.key, i, nested.key });
                            }
                            return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{d}.{s} of type int", .{ path, jsonTypeName(nested.value), field.key, i, nested.key });
                        }
                        if (!numeric and nested.value != .string) {
                            return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{d}.{s} of type string", .{ path, jsonTypeName(nested.value), field.key, i, nested.key });
                        }
                    }
                }
            }
            if (std.mem.eql(u8, canonical, "degradations") and field.value == .array) {
                for (field.value.array, 0..) |item, i| {
                    if (item == .null) continue;
                    if (item != .string) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Packet.{s}.{d} of type string", .{ path, jsonTypeName(item), field.key, i });
                    }
                }
            }
        }
    }
    return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{path});
}

fn parseValueFlag(args: []const []const u8, flag: []const u8) ?[]const u8 {
    for (args, 0..) |arg, i| {
        if (std.mem.eql(u8, arg, flag) and i + 1 < args.len and !std.mem.startsWith(u8, args[i + 1], "-")) return args[i + 1];
    }
    return null;
}

fn invalidPacketDetails(allocator: std.mem.Allocator, violations: []const contract.Violation) ![]const []const u8 {
    var details: std.ArrayList([]const u8) = .empty;
    for (violations) |v| try details.append(allocator, try std.fmt.allocPrint(allocator, "{s}: {s}: {s}", .{ v.rule, v.field, v.message }));
    return try details.toOwnedSlice(allocator);
}

fn contractValidate(allocator: std.mem.Allocator, args: []const []const u8, json_output: bool) !RunResult {
    for (args, 0..) |arg, i| {
        if (std.mem.eql(u8, arg, "--packet") and (i + 1 >= args.len or std.mem.startsWith(u8, args[i + 1], "-"))) return fail(allocator, "--packet requires a value", "ownscout contract validate --help", json_output);
    }
    const path = parseValueFlag(args, "--packet") orelse return fail(allocator, "missing required --packet <file>", "ownscout contract validate --help", json_output);
    for (args) |arg| {
        if (!std.mem.eql(u8, arg, "--packet") and !std.mem.eql(u8, arg, "--json") and !std.mem.eql(u8, arg, path)) return fail(allocator, try std.fmt.allocPrint(allocator, "unknown flag or argument '{s}'", .{arg}), "ownscout contract validate --help", json_output);
    }
    const packet = loadPacket(allocator, path) catch |err| {
        const detail = switch (err) {
            error.NotFound => try std.fmt.allocPrint(allocator, "packet file \"{s}\" does not exist", .{path}),
            error.InvalidJson => try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{path}),
            error.ObjectRequired => try std.fmt.allocPrint(allocator, "packet \"{s}\" must contain a JSON object", .{path}),
            error.UnknownField => blk: {
                const field = unknownPacketFieldFromFile(allocator, path) orelse "unknown";
                break :blk try std.fmt.allocPrint(allocator, "packet \"{s}\" contains an unknown JSON field: json: unknown field {s}", .{ path, try quoteGoString(allocator, field) });
            },
            error.WrongType => try decodeFailureMessage(allocator, path),
            else => try std.fmt.allocPrint(allocator, "read packet \"{s}\" failed", .{path}),
        };
        const details = [_][]const u8{detail};
        return .{ .output = try result.render(allocator, .{ .command = "contract validate", .ok = false, .summary = "packet could not be loaded", .details = &details, .next_action = "Provide a readable JSON packet with --packet <file>." }, json_output), .code = 2 };
    };
    const violations = try contract.validate(allocator, packet);
    if (violations.len != 0) {
        const details = try invalidPacketDetails(allocator, violations);
        const summary = try std.fmt.allocPrint(allocator, "packet is invalid ({d} violation(s))", .{violations.len});
        return .{ .output = try result.render(allocator, .{ .command = "contract validate", .ok = false, .summary = summary, .details = details, .next_action = "Fix the listed packet fields, then run contract validation again." }, json_output), .code = 1 };
    }
    const action_name = contract.action(packet.outcome) orelse "blocked";
    const details = [_][]const u8{
        try std.fmt.allocPrint(allocator, "outcome: {s}", .{packet.outcome}),
        try std.fmt.allocPrint(allocator, "default action: {s}", .{action_name}),
    };
    return .{ .output = try result.render(allocator, .{ .command = "contract validate", .ok = true, .summary = "packet is valid", .details = &details, .next_action = "Run evidence verification before consuming this packet." }, json_output), .code = 0 };
}

fn evidenceVerify(allocator: std.mem.Allocator, args: []const []const u8, json_output: bool) !RunResult {
    // Mirror the reference parseFlags: --repo and --packet take a value,
    // --relocate is a switch only this subcommand accepts, and anything else is
    // an unknown flag. A valued flag whose value is absent or looks like a flag
    // is rejected before the required-value check, exactly as Go does.
    var repo: ?[]const u8 = null;
    var packet_path: ?[]const u8 = null;
    var relocate = false;
    var index: usize = 0;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--json")) continue;
        if (std.mem.eql(u8, arg, "--relocate")) {
            relocate = true;
            continue;
        }
        if (std.mem.eql(u8, arg, "--repo") or std.mem.eql(u8, arg, "--packet")) {
            if (index + 1 >= args.len or std.mem.startsWith(u8, args[index + 1], "-")) return fail(allocator, try std.fmt.allocPrint(allocator, "{s} requires a value", .{arg}), "ownscout evidence verify --help", json_output);
            if (std.mem.eql(u8, arg, "--repo")) repo = args[index + 1] else packet_path = args[index + 1];
            index += 1;
            continue;
        }
        return fail(allocator, try std.fmt.allocPrint(allocator, "unknown flag or argument '{s}'", .{arg}), "ownscout evidence verify --help", json_output);
    }
    if (repo == null or repo.?.len == 0 or packet_path == null or packet_path.?.len == 0) return fail(allocator, "both --repo <dir> and --packet <file> are required", "ownscout evidence verify --help", json_output);
    const packet = loadPacket(allocator, packet_path.?) catch |err| {
        const detail = switch (err) {
            error.NotFound => try std.fmt.allocPrint(allocator, "packet file \"{s}\" does not exist", .{packet_path.?}),
            error.InvalidJson => try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{packet_path.?}),
            error.ObjectRequired => try std.fmt.allocPrint(allocator, "packet \"{s}\" must contain a JSON object", .{packet_path.?}),
            error.UnknownField => blk: {
                const field = unknownPacketFieldFromFile(allocator, packet_path.?) orelse "unknown";
                break :blk try std.fmt.allocPrint(allocator, "packet \"{s}\" contains an unknown JSON field: json: unknown field {s}", .{ packet_path.?, try quoteGoString(allocator, field) });
            },
            error.WrongType => try decodeFailureMessage(allocator, packet_path.?),
            else => try std.fmt.allocPrint(allocator, "read packet \"{s}\" failed", .{packet_path.?}),
        };
        const details = [_][]const u8{detail};
        return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "packet could not be loaded", .details = &details, .next_action = "Provide a readable JSON packet with --packet <file>." }, json_output), .code = 2 };
    };
    const violations = try contract.validate(allocator, packet);
    if (violations.len != 0) {
        const details = try invalidPacketDetails(allocator, violations);
        return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "packet is invalid", .details = details, .next_action = "Fix the packet contract, then verify evidence again." }, json_output), .code = 1 };
    }
    const root = std.fs.path.resolve(allocator, &.{repo.?}) catch return repositoryError(allocator, repo.?, json_output);
    const dir = std.Io.Dir.cwd().openDir(std.Options.debug_io, root, .{}) catch return repositoryError(allocator, repo.?, json_output);
    var verified: usize = 0;
    var issues: std.ArrayList([]const u8) = .empty;
    for (packet.evidence) |item| {
        const outcome = try verifyEvidence(allocator, dir, item, relocate);
        if (outcome.outcome == .verified) {
            verified += 1;
            continue;
        }
        const msg = switch (outcome.outcome) {
            .verified => unreachable,
            .invalid_path => if (std.mem.indexOf(u8, item.path, "..") != null) try std.fmt.allocPrint(allocator, "evidence path \"{s}\" contains a parent component", .{item.path}) else try std.fmt.allocPrint(allocator, "evidence path \"{s}\" must be repository-relative", .{item.path}),
            .not_found => blk: {
                var cwd_buf: [4096]u8 = undefined;
                const cwd_len = std.Io.Dir.cwd().realPathFile(std.Options.debug_io, ".", &cwd_buf) catch 0;
                break :blk try std.fmt.allocPrint(allocator, "cannot access evidence path \"{s}\": lstat {s}/{s}/{s}: no such file or directory", .{ item.path, cwd_buf[0..cwd_len], root, item.path });
            },
            .bad_hash => try std.fmt.allocPrint(allocator, "invalid SHA-256 content hash \"{s}\"", .{item.content_hash}),
            .bad_range => |line_count| try std.fmt.allocPrint(allocator, "invalid line range {d}-{d} for {d} line(s)", .{ item.line_start, item.line_end, line_count }),
            .hash_mismatch => |actual| blk: {
                if (actual) |hash| {
                    break :blk try std.fmt.allocPrint(allocator, "content hash mismatch: expected {s}, got {s}", .{ item.content_hash, hash });
                }
                break :blk try std.fmt.allocPrint(allocator, "content hash mismatch: expected {s}", .{item.content_hash});
            },
        };
        const detail = if (outcome.clause.len != 0)
            try std.fmt.allocPrint(allocator, "evidence \"{s}\" (\"{s}\"): {s}{s}", .{ item.evidence_id, item.path, msg, outcome.clause })
        else
            try std.fmt.allocPrint(allocator, "evidence \"{s}\" (\"{s}\"): {s}", .{ item.evidence_id, item.path, msg });
        try issues.append(allocator, detail);
    }
    if (issues.items.len != 0) {
        const summary = try std.fmt.allocPrint(allocator, "evidence verification failed ({d} issue(s))", .{issues.items.len});
        return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = summary, .details = issues.items, .next_action = "Refresh or correct the listed evidence, then verify again." }, json_output), .code = 1 };
    }
    const details = [_][]const u8{try std.fmt.allocPrint(allocator, "verified {d} evidence span(s)", .{verified})};
    return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = true, .summary = "evidence verified", .details = &details, .next_action = "The packet is ready for its declared default action." }, json_output), .code = 0 };
}

fn repositoryError(allocator: std.mem.Allocator, repo: []const u8, json_output: bool) !RunResult {
    if (std.Io.Dir.cwd().statFile(std.Options.debug_io, repo, .{})) |stat| {
        if (stat.kind != .directory) {
            const details = [_][]const u8{try std.fmt.allocPrint(allocator, "repository \"{s}\" is not a directory", .{repo})};
            return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "repository could not be checked", .details = &details, .next_action = "Provide a readable repository directory with --repo <dir>." }, json_output), .code = 2 };
        }
    } else |_| {}
    var cwd_buf: [4096]u8 = undefined;
    const cwd_len = std.Io.Dir.cwd().realPathFile(std.Options.debug_io, ".", &cwd_buf) catch return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "repository could not be checked", .details = &.{ "could not determine current directory" }, .next_action = "Provide a readable repository directory with --repo <dir>." }, json_output), .code = 2 };
    const cwd = try allocator.dupe(u8, cwd_buf[0..cwd_len]);
    const detail = try std.fmt.allocPrint(allocator, "resolve repository root \"{s}\": lstat {s}/{s}: no such file or directory", .{ repo, cwd, repo });
    const details = [_][]const u8{detail};
    return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "repository could not be checked", .details = &details, .next_action = "Provide a readable repository directory with --repo <dir>." }, json_output), .code = 2 };
}

// Evidence verification mirrors the reference: files are read raw with no
// size limit, a "\r\n" pair is one terminator, a final terminator adds no
// trailing line, hashing joins the selected lines with "\n" and appends a
// final "\n" for non-empty content, and one read feeds both the range check
// and the digest (no re-read on mismatch).
const VerifyOutcome = union(enum) {
    verified,
    invalid_path,
    not_found,
    bad_hash,
    /// Carries the real line count for the error message.
    bad_range: usize,
    /// Carries the computed digest when it could be computed.
    hash_mismatch: ?[]const u8,
};

/// The outcome of one span plus the relocation clause, if any, that the
/// diagnostic message must append. The clause is empty for every path Go does
/// not annotate (path errors, unusable hashes, relocation disabled).
const VerifyResult = struct {
    outcome: VerifyOutcome,
    clause: []const u8 = "",
};

const VerifyError = error{OutOfMemory};

fn verifyEvidence(allocator: std.mem.Allocator, dir: std.Io.Dir, item: contract.Evidence, relocate: bool) VerifyError!VerifyResult {
    if (item.path.len == 0 or std.fs.path.isAbsolute(item.path) or std.mem.indexOf(u8, item.path, "..") != null) return .{ .outcome = .invalid_path };
    const data = dir.readFileAlloc(std.Options.debug_io, item.path, allocator, .unlimited) catch return .{ .outcome = .not_found };
    const line_count = countNormalizedLines(data);
    if (item.line_start < 1 or item.line_end < item.line_start or item.line_end > @as(i64, @intCast(line_count))) {
        return .{
            .outcome = .{ .bad_range = line_count },
            .clause = try locationClause(allocator, data, line_count, item.line_start, item.line_end, item.content_hash, relocate),
        };
    }
    var expected = item.content_hash;
    if (std.mem.startsWith(u8, expected, "sha256:")) expected = expected[7..];
    if (expected.len != 64) return .{ .outcome = .bad_hash };
    for (expected) |c| {
        if (!std.ascii.isHex(c)) return .{ .outcome = .bad_hash };
    }
    var selected: std.ArrayList(u8) = .empty;
    try appendSelectedLines(data, item.line_start, item.line_end, &selected, allocator);
    var digest: [32]u8 = undefined;
    Sha256.hash(selected.items, &digest, .{});
    const actual = std.fmt.bytesToHex(digest, .lower);
    if (!std.ascii.eqlIgnoreCase(expected, &actual)) {
        return .{
            .outcome = .{ .hash_mismatch = try allocator.dupe(u8, &actual) },
            .clause = try locationClause(allocator, data, line_count, item.line_start, item.line_end, item.content_hash, relocate),
        };
    }
    return .{ .outcome = .verified };
}

// Anchor re-resolution (relocation). This mirrors the reference algorithm
// byte for byte: it is diagnostic only and never changes a status, a counter,
// the exit code, or any ledger byte.
const relocate_byte_budget: usize = 8 << 20;

const Relocation = struct {
    found: bool = false,
    line_start: i64 = 0,
    line_end: i64 = 0,
    shift: i64 = 0,
    /// True when every valid start was probed, so "not present in this file"
    /// is a statement the search actually earned.
    exhaustive: bool = false,
};

/// Byte offset at which each 1-based line begins. Empty input has no lines.
/// Same line model as countNormalizedLines: "\r\n" is one terminator and a
/// final terminator adds no trailing empty line.
fn lineStarts(allocator: std.mem.Allocator, data: []const u8) VerifyError![]usize {
    var starts: std.ArrayList(usize) = .empty;
    if (data.len == 0) return try starts.toOwnedSlice(allocator);
    var offset: usize = 0;
    while (offset < data.len) {
        try starts.append(allocator, offset);
        const index = std.mem.indexOfScalarPos(u8, data, offset, '\n') orelse break;
        offset = index + 1;
    }
    return try starts.toOwnedSlice(allocator);
}

/// Offset just past the last content byte of 1-based line, excluding its
/// terminator. The "\r" of a "\r\n" pair belongs to the terminator, but a lone
/// "\r" on an unterminated final line is content and is kept - exactly as the
/// verification path keeps it.
fn lineContentEnd(data: []const u8, starts: []const usize, line: usize) usize {
    const begin = starts[line - 1];
    var end: usize = data.len;
    if (line < starts.len) end = starts[line];
    // Strip "\r\n" or "\n" only when the line is actually terminated. An
    // unterminated final line keeps a trailing "\r" as content; stripping it
    // here would let a relocation match a window that verification hashes
    // differently.
    if (end > begin and data[end - 1] == '\n') {
        end -= 1;
        if (end > begin and data[end - 1] == '\r') end -= 1;
    }
    return end;
}

/// Hash lines [line_start, line_end] exactly as hashSelectedLines does: the
/// selected lines joined with "\n" plus a final "\n" for multi-line content or
/// a non-empty single line. Lowercase hex, like the reference.
fn windowHash(data: []const u8, starts: []const usize, line_start: usize, line_end: usize) [64]u8 {
    var hasher = Sha256.init(.{});
    var line = line_start;
    while (line <= line_end) : (line += 1) {
        if (line > line_start) hasher.update("\n");
        hasher.update(data[starts[line - 1]..lineContentEnd(data, starts, line)]);
    }
    if (line_end > line_start or lineContentEnd(data, starts, line_start) > starts[line_start - 1]) hasher.update("\n");
    var digest: [32]u8 = undefined;
    hasher.final(&digest);
    return std.fmt.bytesToHex(digest, .lower);
}

/// Strip an optional "sha256:" prefix and require exactly 64 hex characters.
/// Returns the raw (possibly uppercase) hex; callers compare case-insensitively,
/// which is equivalent to the reference lowercasing before comparison.
fn normalizedExpectedHash(value: []const u8) ?[]const u8 {
    var hex_value = value;
    if (std.mem.startsWith(u8, hex_value, "sha256:")) hex_value = hex_value[7..];
    if (hex_value.len != 64) return null;
    for (hex_value) |c| {
        if (!std.ascii.isHex(c)) return null;
    }
    return hex_value;
}

/// Search for the recorded fingerprint, probing candidate start lines outward
/// from the cited start (lower line number first on a tie). Returns the first
/// match, or the exhaustive/budget verdict when nothing matched.
fn resolveAnchor(data: []const u8, starts: []const usize, total_lines: usize, line_start: i64, line_end: i64, expected: []const u8) Relocation {
    const extent_signed = line_end -% line_start +% 1;
    if (extent_signed < 1) return .{ .exhaustive = true };
    const extent: usize = @intCast(extent_signed);
    if (total_lines < extent) return .{ .exhaustive = true };
    const valid_starts = total_lines - extent + 1;
    const valid_starts_signed: i64 = @intCast(valid_starts);

    // Clamp the origin into the windows that fit. When the cited range lies
    // past the end of a file that shrank, the nearest fitting windows are the
    // last ones.
    var origin: i64 = line_start;
    if (origin < 1) origin = 1;
    if (origin > valid_starts_signed) origin = valid_starts_signed;

    var probes: usize = 0;
    var used: usize = 0;
    var distance: i64 = 0;
    while (true) : (distance += 1) {
        const low = origin -% distance;
        const high = origin +% distance;
        if (low < 1 and high > valid_starts_signed) break;
        const count: usize = if (distance == 0) 1 else 2;
        var index: usize = 0;
        while (index < count) : (index += 1) {
            const candidate: i64 = if (index == 0) low else high;
            if (candidate < 1 or candidate > valid_starts_signed) continue;
            const start: usize = @intCast(candidate);
            const cost = lineContentEnd(data, starts, start + extent - 1) - starts[start - 1] + extent;
            if (used + cost > relocate_byte_budget) return .{ .exhaustive = probes == valid_starts };
            used += cost;
            probes += 1;
            const hash = windowHash(data, starts, start, start + extent - 1);
            if (std.ascii.eqlIgnoreCase(&hash, expected)) {
                return .{
                    .found = true,
                    .line_start = candidate,
                    .line_end = candidate + @as(i64, @intCast(extent)) - 1,
                    .shift = candidate -% line_start,
                };
            }
        }
    }
    return .{ .exhaustive = probes == valid_starts };
}

/// Render the diagnostic clause appended to a failure message, or "" when no
/// statement can honestly be made (which keeps the message byte-identical to
/// the pre-relocation behaviour).
fn relocationClause(allocator: std.mem.Allocator, data: []const u8, total_lines: usize, line_start: i64, line_end: i64, expected: []const u8) VerifyError![]const u8 {
    if (line_end -% line_start +% 1 < 1) return "";
    const starts = try lineStarts(allocator, data);
    const anchor = resolveAnchor(data, starts, total_lines, line_start, line_end, expected);
    if (anchor.found) {
        const sign: []const u8 = if (anchor.shift < 0) "" else "+";
        return try std.fmt.allocPrint(allocator, "; content relocates to lines {d}-{d} (shift {s}{d}; nearest matching window)", .{ anchor.line_start, anchor.line_end, sign, anchor.shift });
    }
    if (anchor.exhaustive) return "; content not found elsewhere in this file";
    return "; relocation search stopped after its byte budget";
}

/// Relocation diagnostic for a failed span, or "" when relocation is disabled
/// or the recorded fingerprint is unusable.
fn locationClause(allocator: std.mem.Allocator, data: []const u8, total_lines: usize, line_start: i64, line_end: i64, content_hash: []const u8, relocate: bool) VerifyError![]const u8 {
    if (!relocate) return "";
    const expected = normalizedExpectedHash(content_hash) orelse return "";
    return try relocationClause(allocator, data, total_lines, line_start, line_end, expected);
}

fn countNormalizedLines(data: []const u8) usize {
    if (data.len == 0) return 0;
    var count: usize = 0;
    var offset: usize = 0;
    while (std.mem.indexOfScalarPos(u8, data, offset, '\n')) |index| {
        count += 1;
        offset = index + 1;
    }
    if (data[data.len - 1] != '\n') count += 1;
    return count;
}

fn appendSelectedLines(data: []const u8, line_start: i64, line_end: i64, out: *std.ArrayList(u8), allocator: std.mem.Allocator) error{OutOfMemory}!void {
    var first_line_non_empty = false;
    var line: i64 = 1;
    var start: usize = 0;
    while (line <= line_end and start < data.len) {
        const nl: ?usize = std.mem.indexOfScalarPos(u8, data, start, '\n');
        var content_end: usize = data.len;
        if (nl) |index| {
            content_end = index;
            if (content_end > start and data[content_end - 1] == '\r') content_end -= 1;
        }
        if (line >= line_start) {
            if (line > line_start) try out.append(allocator, '\n');
            try out.appendSlice(allocator, data[start..content_end]);
            if (line == line_start) first_line_non_empty = content_end > start;
        }
        if (nl) |index| {
            start = index + 1;
            line += 1;
        } else break;
    }
    if (line_end > line_start or first_line_non_empty) try out.append(allocator, '\n');
}

fn nodeVerify(allocator: std.mem.Allocator, args: []const []const u8, json_output: bool) !RunResult {
    // Mirror the reference parseFlags: only the four valued flags and --json
    // are accepted, so --relocate is rejected here exactly as before it existed.
    var repo_value: ?[]const u8 = null;
    var packet_value: ?[]const u8 = null;
    var envelope_value: ?[]const u8 = null;
    var ledger_value: ?[]const u8 = null;
    var index: usize = 0;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--json")) continue;
        if (std.mem.eql(u8, arg, "--repo") or std.mem.eql(u8, arg, "--packet") or std.mem.eql(u8, arg, "--envelope") or std.mem.eql(u8, arg, "--ledger")) {
            if (index + 1 >= args.len or std.mem.startsWith(u8, args[index + 1], "-")) return fail(allocator, try std.fmt.allocPrint(allocator, "{s} requires a value", .{arg}), "ownscout node verify --help", json_output);
            if (std.mem.eql(u8, arg, "--repo")) {
                repo_value = args[index + 1];
            } else if (std.mem.eql(u8, arg, "--packet")) {
                packet_value = args[index + 1];
            } else if (std.mem.eql(u8, arg, "--envelope")) {
                envelope_value = args[index + 1];
            } else {
                ledger_value = args[index + 1];
            }
            index += 1;
            continue;
        }
        return fail(allocator, try std.fmt.allocPrint(allocator, "unknown flag or argument '{s}'", .{arg}), "ownscout node verify --help", json_output);
    }
    if (repo_value == null or repo_value.?.len == 0) return fail(allocator, "missing required --repo value", "ownscout node verify --help", json_output);
    if (packet_value == null or packet_value.?.len == 0) return fail(allocator, "missing required --packet value", "ownscout node verify --help", json_output);
    if (envelope_value == null or envelope_value.?.len == 0) return fail(allocator, "missing required --envelope value", "ownscout node verify --help", json_output);
    if (ledger_value == null or ledger_value.?.len == 0) return fail(allocator, "missing required --ledger value", "ownscout node verify --help", json_output);
    const repo = repo_value.?;
    const packet_path = packet_value.?;
    const envelope_path = envelope_value.?;
    const ledger_path = ledger_value.?;
    const packet = loadNodePacket(allocator, packet_path) catch |err| {
        // strict packet decoding rejects every wire-format problem the same
        // way, including an unknown (or merely differently cased) field name.
        if (err == error.WrongType or err == error.InvalidJson or err == error.UnknownField or err == error.ObjectRequired) {
            const details = [_][]const u8{"strict packet decoding failed"};
            return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "packet could not be decoded", .details = &details, .next_action = "Provide one valid packet-v1 JSON object with --packet <file>." }, json_output), .code = 2 };
        }
        const details = [_][]const u8{"packet input could not be read"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "packet could not be loaded", .details = &details, .next_action = "Provide a readable packet file with --packet <file>." }, json_output), .code = 2 };
    };
    const violations = try contract.validate(allocator, packet);
    if (violations.len != 0) {
        const details = try invalidPacketDetails(allocator, violations);
        const summary = try std.fmt.allocPrint(allocator, "packet contract failed ({d} violation(s))", .{violations.len});
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = summary, .details = details, .next_action = "Fix the packet contract, then run node verification again." }, json_output), .code = 2 };
    }
    const envelope_data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, envelope_path, allocator, .limited(1 << 20)) catch {
        const details = [_][]const u8{"envelope input could not be read"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "envelope could not be loaded", .details = &details, .next_action = "Provide a readable envelope file with --envelope <file>." }, json_output), .code = 2 };
    };
    const envelope = parseEnvelope(allocator, envelope_data) catch {
        const details = [_][]const u8{"strict envelope parsing failed"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "envelope could not be parsed", .details = &details, .next_action = "Provide one valid node-envelope-v1 JSON object with --envelope <file>." }, json_output), .code = 2 };
    };
    const binding = canonicalBinding(allocator, packet) catch {
        const details = [_][]const u8{"canonical packet binding failed"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "packet binding could not be computed", .details = &details, .next_action = "Provide a valid packet-v1 document and try again." }, json_output), .code = 2 };
    };
    validateEnvelope(envelope, packet, binding) catch {
        const details = [_][]const u8{"node-envelope-v1 validation failed"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "envelope validation failed", .details = &details, .next_action = "Fix the envelope binding or graph, then run node verification again." }, json_output), .code = 2 };
    };
    if (std.mem.eql(u8, ledger_path, repo) or (std.mem.startsWith(u8, ledger_path, repo) and ledger_path.len > repo.len and ledger_path[repo.len] == '/')) {
        const details = [_][]const u8{"ledger open failed"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "ledger could not be opened", .details = &details, .next_action = "Provide a writable ledger path outside the repository and try again." }, json_output), .code = 2 };
    }
    const root = std.fs.path.resolve(allocator, &.{repo}) catch return repositoryError(allocator, repo, json_output);
    const dir = std.Io.Dir.cwd().openDir(std.Options.debug_io, root, .{}) catch return repositoryError(allocator, repo, json_output);
    var details = std.ArrayList([]const u8).empty;
    var failed = try allocator.alloc(bool, envelope.nodes.len);
    @memset(failed, false);
    for (envelope.nodes, 0..) |node, node_index| {
        var blocked_by: ?[]const u8 = null;
        for (node.depends_on) |dependency| {
            for (envelope.nodes, 0..) |candidate, candidate_index| {
                if (std.mem.eql(u8, candidate.node_id, dependency) and failed[candidate_index]) blocked_by = dependency;
            }
        }
        if (blocked_by) |dependency| {
            failed[node_index] = true;
            try details.append(allocator, try std.fmt.allocPrint(allocator, "node \"{s}\": blocked (dependency \"{s}\" is not evidence_current)", .{ node.node_id, dependency }));
            continue;
        }
        var node_failed = false;
        for (node.evidence_ids) |id| {
            const item = findEvidence(packet.evidence, id) orelse continue;
            const outcome = try verifyEvidence(allocator, dir, item, false);
            if (outcome.outcome != .verified) {
                const detail = try std.fmt.allocPrint(allocator, "node \"{s}\": failed (evidence \"{s}\" is missing or not verified)", .{ node.node_id, id });
                try details.append(allocator, detail);
                node_failed = true;
                break;
            }
        }
        if (!node_failed) {
            try details.append(allocator, try std.fmt.allocPrint(allocator, "node \"{s}\": evidence_current", .{node.node_id}));
        }
        failed[node_index] = node_failed;
    }
    var envelope_digest: [32]u8 = undefined;
    Sha256.hash(envelope_data, &envelope_digest, .{});
    const envelope_sum = envelope_digest;
    const envelope_hash = std.fmt.bytesToHex(envelope_sum, .lower);
    appendLedger(allocator, ledger_path, envelope_hash, binding, details.items) catch {
        const ledger_details = [_][]const u8{"ledger open failed"};
        return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "ledger could not be opened", .details = &ledger_details, .next_action = "Provide a writable ledger path outside the repository and try again." }, json_output), .code = 2 };
    };
    if (std.mem.indexOfScalar(bool, failed, true) != null) return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = false, .summary = "node evaluation failed", .details = details.items, .next_action = "Refresh or correct the failed evidence, then run node verification again." }, json_output), .code = 1 };
    return .{ .output = try result.render(allocator, .{ .command = "node verify", .ok = true, .summary = "all nodes are evidence_current", .details = details.items, .next_action = "The node envelope is recorded and ready for its declared workflow." }, json_output), .code = 0 };
}

const Node = struct { node_id: []const u8, depends_on: []const []const u8, verifier: []const u8, evidence_ids: []const []const u8 };
const Envelope = struct { schema_version: []const u8, envelope_id: []const u8, packet_id: []const u8, packet_binding_sha256: []const u8, nodes: []Node };

fn strictFields(value: json.Value, allowed: []const []const u8) !void {
    if (value != .object) return error.InvalidEnvelope;
    for (value.object) |field_value| {
        var known = false;
        for (allowed) |name| {
            if (std.mem.eql(u8, name, field_value.key)) {
                known = true;
                break;
            }
        }
        if (!known) return error.InvalidEnvelope;
    }
}

fn stringField(value: json.Value, name: []const u8) ![]const u8 {
    const field_value = value.objectField(name) orelse return error.InvalidEnvelope;
    if (field_value != .string) return error.InvalidEnvelope;
    return field_value.string;
}

fn stringArray(allocator: std.mem.Allocator, value: json.Value, name: []const u8) ![]const []const u8 {
    const field_value = value.objectField(name) orelse return error.InvalidEnvelope;
    if (field_value != .array) return error.InvalidEnvelope;
    var values = try allocator.alloc([]const u8, field_value.array.len);
    for (field_value.array, 0..) |item, i| {
        if (item != .string) return error.InvalidEnvelope;
        values[i] = item.string;
    }
    return values;
}

fn parseEnvelope(allocator: std.mem.Allocator, data: []const u8) !Envelope {
    const root = try json.Parser.parse(allocator, data);
    try strictFields(root, &.{ "schema_version", "envelope_id", "packet_id", "packet_binding_sha256", "nodes" });
    const nodes_value = root.objectField("nodes") orelse return error.InvalidEnvelope;
    if (nodes_value != .array) return error.InvalidEnvelope;
    var nodes = try allocator.alloc(Node, nodes_value.array.len);
    if (nodes.len == 0) return error.InvalidEnvelope;
    for (nodes_value.array, 0..) |value, i| {
        try strictFields(value, &.{ "node_id", "depends_on", "verifier", "evidence_ids" });
        nodes[i] = .{
            .node_id = try stringField(value, "node_id"),
            .depends_on = try stringArray(allocator, value, "depends_on"),
            .verifier = try stringField(value, "verifier"),
            .evidence_ids = try stringArray(allocator, value, "evidence_ids"),
        };
    }
    return .{
        .schema_version = try stringField(root, "schema_version"),
        .envelope_id = try stringField(root, "envelope_id"),
        .packet_id = try stringField(root, "packet_id"),
        .packet_binding_sha256 = try stringField(root, "packet_binding_sha256"),
        .nodes = nodes,
    };
}

fn appendJsonString(list: *std.ArrayList(u8), allocator: std.mem.Allocator, value: []const u8) !void {
    try list.append(allocator, '"');
    for (value) |c| switch (c) {
        '"' => try list.appendSlice(allocator, "\\\""),
        '\\' => try list.appendSlice(allocator, "\\\\"),
        '\n' => try list.appendSlice(allocator, "\\n"),
        '\r' => try list.appendSlice(allocator, "\\r"),
        '\t' => try list.appendSlice(allocator, "\\t"),
        else => try list.append(allocator, c),
    };
    try list.append(allocator, '"');
}

fn canonicalBinding(allocator: std.mem.Allocator, p: contract.Packet) ![64]u8 {
    var out = std.ArrayList(u8).empty;
    try out.appendSlice(allocator, "{\"packet_id\":");
    try appendJsonString(&out, allocator, p.packet_id);
    try out.appendSlice(allocator, ",\"schema_version\":");
    try appendJsonString(&out, allocator, p.schema_version);
    try out.appendSlice(allocator, ",\"repo_root\":");
    try appendJsonString(&out, allocator, p.repo_root);
    try out.appendSlice(allocator, ",\"head_commit\":");
    try appendJsonString(&out, allocator, p.head_commit);
    try out.appendSlice(allocator, ",\"request_id\":");
    try appendJsonString(&out, allocator, p.request_id);
    try out.appendSlice(allocator, ",\"issued_at\":");
    try appendJsonString(&out, allocator, p.issued_at);
    try out.appendSlice(allocator, ",\"outcome\":");
    try appendJsonString(&out, allocator, p.outcome);
    try out.appendSlice(allocator, ",\"freshness\":{\"head_commit\":");
    try appendJsonString(&out, allocator, p.freshness.head_commit);
    if (p.freshness.head_anchor.len != 0) { try out.appendSlice(allocator, ",\"head_anchor\":"); try appendJsonString(&out, allocator, p.freshness.head_anchor); }
    if (p.freshness.status.len != 0) { try out.appendSlice(allocator, ",\"status\":"); try appendJsonString(&out, allocator, p.freshness.status); }
    if (p.freshness.current) try out.appendSlice(allocator, ",\"current\":true");
    if (p.freshness.is_current) try out.appendSlice(allocator, ",\"is_current\":true");
    if (p.freshness.checked_at.len != 0) { try out.appendSlice(allocator, ",\"checked_at\":"); try appendJsonString(&out, allocator, p.freshness.checked_at); }
    try out.appendSlice(allocator, "},\"authorization\":{\"level\":");
    try appendJsonString(&out, allocator, p.authorization.level);
    if (p.authorization.reason.len != 0) { try out.appendSlice(allocator, ",\"reason\":"); try appendJsonString(&out, allocator, p.authorization.reason); }
    try out.appendSlice(allocator, "},\"budget\":{\"max_evidence\":");
    const budget_text = try std.fmt.allocPrint(allocator, "{d},\"used_evidence\":{d},\"max_bytes\":{d},\"used_bytes\":{d}}},\"evidence\":[", .{ p.budget.max_evidence, p.budget.used_evidence, p.budget.max_bytes, p.budget.used_bytes });
    try out.appendSlice(allocator, budget_text);
    for (p.evidence, 0..) |e, i| {
        if (i != 0) try out.append(allocator, ',');
        try out.appendSlice(allocator, "{\"evidence_id\":"); try appendJsonString(&out, allocator, e.evidence_id);
        try out.appendSlice(allocator, ",\"kind\":"); try appendJsonString(&out, allocator, e.kind);
        try out.appendSlice(allocator, ",\"path\":"); try appendJsonString(&out, allocator, e.path);
        try out.appendSlice(allocator, ",\"commit\":"); try appendJsonString(&out, allocator, e.commit);
        const line_text = try std.fmt.allocPrint(allocator, ",\"line_start\":{d},\"line_end\":{d},\"source\":", .{e.line_start, e.line_end});
        try out.appendSlice(allocator, line_text); try appendJsonString(&out, allocator, e.source);
        try out.appendSlice(allocator, ",\"content_hash\":"); try appendJsonString(&out, allocator, e.content_hash);
        try out.appendSlice(allocator, ",\"collected_at\":"); try appendJsonString(&out, allocator, e.collected_at);
        try out.appendSlice(allocator, ",\"verifier_status\":"); try appendJsonString(&out, allocator, e.verifier_status); try out.append(allocator, '}');
    }
    try out.appendSlice(allocator, "],\"degradations\":[");
    for (p.degradations, 0..) |d, i| { if (i != 0) try out.append(allocator, ','); try appendJsonString(&out, allocator, d); }
    try out.appendSlice(allocator, "],\"provenance\":{");
    var wrote = false;
    if (p.provenance.collector.len != 0) { try out.appendSlice(allocator, "\"collector\":"); try appendJsonString(&out, allocator, p.provenance.collector); wrote = true; }
    if (p.provenance.tool.len != 0) { if (wrote) try out.append(allocator, ','); try out.appendSlice(allocator, "\"tool\":"); try appendJsonString(&out, allocator, p.provenance.tool); wrote = true; }
    if (p.provenance.version.len != 0) { if (wrote) try out.append(allocator, ','); try out.appendSlice(allocator, "\"version\":"); try appendJsonString(&out, allocator, p.provenance.version); wrote = true; }
    if (p.provenance.tool_version.len != 0) { if (wrote) try out.append(allocator, ','); try out.appendSlice(allocator, "\"tool_version\":"); try appendJsonString(&out, allocator, p.provenance.tool_version); }
    try out.appendSlice(allocator, "},\"packet_hash\":\"\"}");
    var digest: [32]u8 = undefined;
    Sha256.hash(out.items, &digest, .{});
    return std.fmt.bytesToHex(digest, .lower);
}

fn findEvidence(items: []contract.Evidence, id: []const u8) ?contract.Evidence {
    for (items) |item| if (std.mem.eql(u8, item.evidence_id, id)) return item;
    return null;
}

// Mirrors node.validateIdentifier: 1-128 bytes, alphanumeric, and after the
// first byte also '.', '_', '-' or ':'.
fn validIdentifier(value: []const u8) bool {
    if (value.len < 1 or value.len > 128) return false;
    for (value, 0..) |c, i| {
        const alphanumeric = (c >= 'a' and c <= 'z') or (c >= 'A' and c <= 'Z') or (c >= '0' and c <= '9');
        if (!alphanumeric and (i == 0 or (c != '.' and c != '_' and c != '-' and c != ':'))) return false;
    }
    return true;
}

fn validateEnvelope(env: Envelope, p: contract.Packet, binding: [64]u8) !void {
    if (!std.mem.eql(u8, env.schema_version, "node-envelope-v1") or env.nodes.len == 0) return error.InvalidEnvelope;
    if (!validIdentifier(env.envelope_id) or !validIdentifier(env.packet_id)) return error.InvalidEnvelope;
    if (!std.mem.eql(u8, env.packet_id, p.packet_id) or !std.mem.eql(u8, env.packet_binding_sha256, &binding)) return error.InvalidEnvelope;
    for (p.evidence) |evidence| if (!validIdentifier(evidence.evidence_id)) return error.InvalidEnvelope;
    for (env.nodes, 0..) |node, i| {
        if (!validIdentifier(node.node_id) or !std.mem.eql(u8, node.verifier, "evidence.current") or node.evidence_ids.len == 0) return error.InvalidEnvelope;
        for (env.nodes[0..i]) |previous| if (std.mem.eql(u8, previous.node_id, node.node_id)) return error.InvalidEnvelope;
        for (node.depends_on) |dependency| {
            if (!validIdentifier(dependency)) return error.InvalidEnvelope;
            var found = false;
            for (env.nodes) |candidate| {
                if (std.mem.eql(u8, candidate.node_id, dependency)) {
                    found = true;
                    break;
                }
            }
            if (!found or std.mem.eql(u8, dependency, node.node_id)) return error.InvalidEnvelope;
        }
        for (node.evidence_ids) |evidence_id| {
            if (!validIdentifier(evidence_id)) return error.InvalidEnvelope;
            var found = false;
            for (p.evidence) |evidence| {
                if (std.mem.eql(u8, evidence.evidence_id, evidence_id)) {
                    found = true;
                    break;
                }
            }
            if (!found) return error.InvalidEnvelope;
        }
    }
    const marks = try std.heap.page_allocator.alloc(u8, env.nodes.len);
    defer std.heap.page_allocator.free(marks);
    @memset(marks, 0);
    for (env.nodes, 0..) |_, i| try visitNode(env.nodes, i, marks);
}

fn visitNode(nodes: []const Node, index: usize, marks: []u8) !void {
    if (marks[index] == 1) return error.InvalidEnvelope;
    if (marks[index] == 2) return;
    marks[index] = 1;
    for (nodes[index].depends_on) |dependency| {
        for (nodes, 0..) |candidate, candidate_index| {
            if (std.mem.eql(u8, candidate.node_id, dependency)) try visitNode(nodes, candidate_index, marks);
        }
    }
    marks[index] = 2;
}

// ---------------------------------------------------------------------------
// Ledger validation and append (internal/ledger/ledger.go).
//
// An existing ledger is untrusted input: every line must decode strictly with
// canonical field names, chain onto the previous record, and hash to its
// recorded digest before a new record may be appended.
// ---------------------------------------------------------------------------

const ledger_zero_hash = "0000000000000000000000000000000000000000000000000000000000000000";
const ledger_max_record_size = 64 << 10;
const ledger_max_node_results = 4096;

const LedgerNodeResult = struct { node_id: []const u8, status: []const u8, reason: []const u8 = "" };
const LedgerRecord = struct {
    schema_version: []const u8 = "",
    seq: u64 = 0,
    prev_record_hash: []const u8 = "",
    record_hash: []const u8 = "",
    envelope_sha256: []const u8 = "",
    packet_binding_sha256: []const u8 = "",
    ownscout_version: []const u8 = "",
    node_results: []const LedgerNodeResult = &.{},
};

const ledger_record_fields = [_][]const u8{
    "schema_version",  "seq",             "prev_record_hash",     "record_hash",
    "envelope_sha256", "packet_binding_sha256", "ownscout_version", "node_results",
};
const ledger_node_result_fields = [_][]const u8{ "node_id", "status", "reason" };

/// Go's bytes.TrimSpace, which trims unicode.IsSpace runes.
fn goSpaceRune(r: u21) bool {
    return switch (r) {
        0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000 => true,
        0x2000...0x200A => true,
        else => false,
    };
}

fn trimGoSpace(value: []const u8) []const u8 {
    var start: usize = 0;
    while (start < value.len) {
        const byte = value[start];
        if (byte < 0x80) {
            if (!goSpaceRune(byte)) break;
            start += 1;
            continue;
        }
        const length: usize = std.unicode.utf8ByteSequenceLength(byte) catch break;
        if (start + length > value.len) break;
        const decoded = std.unicode.utf8Decode(value[start .. start + length]) catch break;
        if (!goSpaceRune(decoded)) break;
        start += length;
    }
    var end: usize = value.len;
    while (end > start) {
        var rune_start = end - 1;
        while (rune_start > start and (value[rune_start] & 0xC0) == 0x80) rune_start -= 1;
        const byte = value[rune_start];
        if (byte < 0x80) {
            if (!goSpaceRune(byte)) break;
        } else {
            const decoded = std.unicode.utf8Decode(value[rune_start..end]) catch break;
            if (!goSpaceRune(decoded)) break;
        }
        end = rune_start;
    }
    return value[start..end];
}

fn exactLedgerField(key: []const u8, fields: []const []const u8) ?[]const u8 {
    for (fields) |name| if (std.mem.eql(u8, key, name)) return name;
    return null;
}

fn hasNul(value: []const u8) bool {
    return std.mem.indexOfScalar(u8, value, 0) != null;
}

fn isLedgerSha256(value: []const u8) bool {
    if (value.len != 64) return false;
    for (value) |c| {
        if (!((c >= '0' and c <= '9') or (c >= 'a' and c <= 'f'))) return false;
    }
    return true;
}

fn isLedgerStatus(status: []const u8) bool {
    return std.mem.eql(u8, status, "evidence_current") or std.mem.eql(u8, status, "failed") or std.mem.eql(u8, status, "blocked");
}

/// Go's encoding/json string escaping with HTML escaping on (json.Marshal).
fn appendGoJsonString(list: *std.ArrayList(u8), allocator: std.mem.Allocator, value: []const u8) !void {
    const digits = "0123456789abcdef";
    try list.append(allocator, '"');
    var index: usize = 0;
    while (index < value.len) {
        const byte = value[index];
        if (byte < 0x80) {
            index += 1;
            switch (byte) {
                '"' => try list.appendSlice(allocator, "\\\""),
                '\\' => try list.appendSlice(allocator, "\\\\"),
                '\x08' => try list.appendSlice(allocator, "\\b"),
                '\x0c' => try list.appendSlice(allocator, "\\f"),
                '\n' => try list.appendSlice(allocator, "\\n"),
                '\r' => try list.appendSlice(allocator, "\\r"),
                '\t' => try list.appendSlice(allocator, "\\t"),
                '<' => try list.appendSlice(allocator, "\\u003c"),
                '>' => try list.appendSlice(allocator, "\\u003e"),
                '&' => try list.appendSlice(allocator, "\\u0026"),
                else => {
                    if (byte < 0x20 or byte == 0x7f) {
                        try list.appendSlice(allocator, "\\u00");
                        try list.append(allocator, digits[byte >> 4]);
                        try list.append(allocator, digits[byte & 0x0f]);
                    } else {
                        try list.append(allocator, byte);
                    }
                },
            }
            continue;
        }
        const length: usize = std.unicode.utf8ByteSequenceLength(byte) catch {
            try list.appendSlice(allocator, "\xef\xbf\xbd");
            index += 1;
            continue;
        };
        if (index + length > value.len or !std.unicode.utf8ValidateSlice(value[index .. index + length])) {
            try list.appendSlice(allocator, "\xef\xbf\xbd");
            index += 1;
            continue;
        }
        const decoded = std.unicode.utf8Decode(value[index .. index + length]) catch {
            try list.appendSlice(allocator, "\xef\xbf\xbd");
            index += 1;
            continue;
        };
        if (decoded == 0x2028) {
            try list.appendSlice(allocator, "\\u2028");
        } else if (decoded == 0x2029) {
            try list.appendSlice(allocator, "\\u2029");
        } else {
            try list.appendSlice(allocator, value[index .. index + length]);
        }
        index += length;
    }
    try list.append(allocator, '"');
}

fn parseLedgerRecord(allocator: std.mem.Allocator, line: []const u8) !LedgerRecord {
    // encoding/json coerces invalid UTF-8 to U+FFFD before decoding.
    const normalized = try normalizeJsonUtf8(allocator, line);
    const root = json.Parser.parse(allocator, normalized) catch return error.InvalidLedger;
    if (root != .object) return error.InvalidLedger;
    var record = LedgerRecord{};
    for (root.object) |field| {
        const canonical = exactLedgerField(field.key, &ledger_record_fields) orelse return error.InvalidLedger;
        if (std.mem.eql(u8, canonical, "schema_version")) {
            if (field.value != .string) return error.InvalidLedger;
            record.schema_version = field.value.string;
        } else if (std.mem.eql(u8, canonical, "seq")) {
            if (field.value != .number) return error.InvalidLedger;
            record.seq = std.fmt.parseUnsigned(u64, field.value.number, 10) catch return error.InvalidLedger;
        } else if (std.mem.eql(u8, canonical, "prev_record_hash")) {
            if (field.value != .string) return error.InvalidLedger;
            record.prev_record_hash = field.value.string;
        } else if (std.mem.eql(u8, canonical, "record_hash")) {
            if (field.value != .string) return error.InvalidLedger;
            record.record_hash = field.value.string;
        } else if (std.mem.eql(u8, canonical, "envelope_sha256")) {
            if (field.value != .string) return error.InvalidLedger;
            record.envelope_sha256 = field.value.string;
        } else if (std.mem.eql(u8, canonical, "packet_binding_sha256")) {
            if (field.value != .string) return error.InvalidLedger;
            record.packet_binding_sha256 = field.value.string;
        } else if (std.mem.eql(u8, canonical, "ownscout_version")) {
            if (field.value != .string) return error.InvalidLedger;
            record.ownscout_version = field.value.string;
        } else if (std.mem.eql(u8, canonical, "node_results")) {
            if (field.value != .array) return error.InvalidLedger;
            const results = try allocator.alloc(LedgerNodeResult, field.value.array.len);
            for (field.value.array, 0..) |item, i| {
                if (item != .object) return error.InvalidLedger;
                var node_result = LedgerNodeResult{ .node_id = "", .status = "" };
                for (item.object) |member| {
                    const name = exactLedgerField(member.key, &ledger_node_result_fields) orelse return error.InvalidLedger;
                    if (member.value != .string) return error.InvalidLedger;
                    if (std.mem.eql(u8, name, "node_id")) {
                        node_result.node_id = member.value.string;
                    } else if (std.mem.eql(u8, name, "status")) {
                        node_result.status = member.value.string;
                    } else {
                        node_result.reason = member.value.string;
                    }
                }
                results[i] = node_result;
            }
            record.node_results = results;
        }
    }
    return record;
}

fn validateLedgerRecord(record: LedgerRecord, expected_seq: u64, expected_prev: []const u8) !void {
    if (!std.mem.eql(u8, record.schema_version, "ownscout-ledger-v1")) return error.InvalidLedger;
    if (record.seq != expected_seq) return error.InvalidLedger;
    if (!std.mem.eql(u8, record.prev_record_hash, expected_prev)) return error.InvalidLedger;
    if (!isLedgerSha256(record.prev_record_hash)) return error.InvalidLedger;
    if (!isLedgerSha256(record.record_hash)) return error.InvalidLedger;
    if (!isLedgerSha256(record.envelope_sha256)) return error.InvalidLedger;
    if (!isLedgerSha256(record.packet_binding_sha256)) return error.InvalidLedger;
    if (record.ownscout_version.len == 0) return error.InvalidLedger;
    if (record.node_results.len == 0 or record.node_results.len > ledger_max_node_results) return error.InvalidLedger;
    if (hasNul(record.schema_version) or hasNul(record.prev_record_hash) or hasNul(record.record_hash) or
        hasNul(record.envelope_sha256) or hasNul(record.packet_binding_sha256) or hasNul(record.ownscout_version)) return error.InvalidLedger;
    for (record.node_results, 0..) |node_result, i| {
        if (!validIdentifier(node_result.node_id)) return error.InvalidLedger;
        if (!isLedgerStatus(node_result.status)) return error.InvalidLedger;
        if (hasNul(node_result.node_id) or hasNul(node_result.status) or hasNul(node_result.reason)) return error.InvalidLedger;
        for (record.node_results[0..i]) |previous| {
            if (std.mem.eql(u8, previous.node_id, node_result.node_id)) return error.InvalidLedger;
        }
    }
}

/// The canonical record encoding hashRecord hashes: the struct field order,
/// record_hash empty, and reason omitted when empty.
fn ledgerRecordJson(allocator: std.mem.Allocator, record: LedgerRecord, include_hash: bool) ![]u8 {
    var out = std.ArrayList(u8).empty;
    try out.appendSlice(allocator, "{\"schema_version\":");
    try appendGoJsonString(&out, allocator, record.schema_version);
    try out.appendSlice(allocator, ",\"seq\":");
    try out.appendSlice(allocator, try std.fmt.allocPrint(allocator, "{d}", .{record.seq}));
    try out.appendSlice(allocator, ",\"prev_record_hash\":");
    try appendGoJsonString(&out, allocator, record.prev_record_hash);
    try out.appendSlice(allocator, ",\"record_hash\":");
    if (include_hash) {
        try appendGoJsonString(&out, allocator, record.record_hash);
    } else {
        try out.appendSlice(allocator, "\"\"");
    }
    try out.appendSlice(allocator, ",\"envelope_sha256\":");
    try appendGoJsonString(&out, allocator, record.envelope_sha256);
    try out.appendSlice(allocator, ",\"packet_binding_sha256\":");
    try appendGoJsonString(&out, allocator, record.packet_binding_sha256);
    try out.appendSlice(allocator, ",\"ownscout_version\":");
    try appendGoJsonString(&out, allocator, record.ownscout_version);
    try out.appendSlice(allocator, ",\"node_results\":[");
    for (record.node_results, 0..) |node_result, i| {
        if (i != 0) try out.append(allocator, ',');
        try out.appendSlice(allocator, "{\"node_id\":");
        try appendGoJsonString(&out, allocator, node_result.node_id);
        try out.appendSlice(allocator, ",\"status\":");
        try appendGoJsonString(&out, allocator, node_result.status);
        if (node_result.reason.len != 0) {
            try out.appendSlice(allocator, ",\"reason\":");
            try appendGoJsonString(&out, allocator, node_result.reason);
        }
        try out.append(allocator, '}');
    }
    try out.appendSlice(allocator, "]}");
    return try out.toOwnedSlice(allocator);
}

fn ledgerRecordHash(allocator: std.mem.Allocator, record: LedgerRecord) ![64]u8 {
    const encoded = try ledgerRecordJson(allocator, record, false);
    var digest: [32]u8 = undefined;
    Sha256.hash(encoded, &digest, .{});
    return std.fmt.bytesToHex(digest, .lower);
}

fn ledgerResults(allocator: std.mem.Allocator, details: []const []const u8) ![]LedgerNodeResult {
    var out: std.ArrayList(LedgerNodeResult) = .empty;
    for (details) |detail| {
        const node_start = std.mem.indexOf(u8, detail, "node \"") orelse continue;
        const id_start = node_start + 6;
        const id_end = std.mem.indexOfScalarPos(u8, detail, id_start, '"') orelse continue;
        const status_start = (std.mem.indexOf(u8, detail[id_end..], "\": ") orelse continue) + id_end + 3;
        const status_end = std.mem.indexOfScalarPos(u8, detail, status_start, ' ') orelse detail.len;
        var reason: []const u8 = "";
        if (std.mem.indexOf(u8, detail, "failed (")) |reason_start| {
            const start = reason_start + "failed (".len;
            const end = std.mem.lastIndexOfScalar(u8, detail, ')') orelse detail.len;
            reason = detail[start..end];
        } else if (std.mem.indexOf(u8, detail, "blocked (")) |reason_start| {
            const start = reason_start + "blocked (".len;
            const end = std.mem.lastIndexOfScalar(u8, detail, ')') orelse detail.len;
            reason = detail[start..end];
        }
        try out.append(allocator, .{ .node_id = detail[id_start..id_end], .status = detail[status_start..status_end], .reason = reason });
    }
    return try out.toOwnedSlice(allocator);
}

fn appendLedger(allocator: std.mem.Allocator, path: []const u8, envelope_hash: [64]u8, binding: [64]u8, details: []const []const u8) !void {
    const io = std.Options.debug_io;
    const old: []const u8 = std.Io.Dir.cwd().readFileAlloc(io, path, allocator, .limited(1 << 20)) catch |err| switch (err) {
        error.FileNotFound => "",
        else => return error.InvalidLedger,
    };
    var count: u64 = 0;
    var last_hash: []const u8 = ledger_zero_hash;
    if (old.len != 0) {
        if (old[old.len - 1] != '\n') return error.InvalidLedger;
        var start: usize = 0;
        while (start < old.len) {
            const newline = std.mem.indexOfScalarPos(u8, old, start, '\n') orelse return error.InvalidLedger;
            const trimmed = trimGoSpace(old[start..newline]);
            start = newline + 1;
            if (trimmed.len == 0) return error.InvalidLedger;
            if (trimmed.len > ledger_max_record_size + 1) return error.InvalidLedger;
            const record = try parseLedgerRecord(allocator, trimmed);
            const expected_seq = count + 1;
            const expected_prev = if (count == 0) ledger_zero_hash else last_hash;
            try validateLedgerRecord(record, expected_seq, expected_prev);
            const expected_hash = try ledgerRecordHash(allocator, record);
            if (!std.mem.eql(u8, record.record_hash, &expected_hash)) return error.InvalidLedger;
            last_hash = record.record_hash;
            count += 1;
        }
    }
    const results = try ledgerResults(allocator, details);
    const seq = count + 1;
    const previous = if (count == 0) ledger_zero_hash else last_hash;
    const pending = LedgerRecord{
        .schema_version = "ownscout-ledger-v1",
        .seq = seq,
        .prev_record_hash = previous,
        .record_hash = ledger_zero_hash,
        .envelope_sha256 = &envelope_hash,
        .packet_binding_sha256 = &binding,
        .ownscout_version = "0.1.0",
        .node_results = results,
    };
    try validateLedgerRecord(pending, seq, previous);
    const record_hash = try ledgerRecordHash(allocator, pending);
    const final = LedgerRecord{
        .schema_version = pending.schema_version,
        .seq = pending.seq,
        .prev_record_hash = pending.prev_record_hash,
        .record_hash = &record_hash,
        .envelope_sha256 = pending.envelope_sha256,
        .packet_binding_sha256 = pending.packet_binding_sha256,
        .ownscout_version = pending.ownscout_version,
        .node_results = pending.node_results,
    };
    const encoded = try ledgerRecordJson(allocator, final, true);
    var output = std.ArrayList(u8).empty;
    if (old.len != 0) try output.appendSlice(allocator, old);
    try output.appendSlice(allocator, encoded);
    try output.append(allocator, '\n');
    try std.Io.Dir.cwd().writeFile(io, .{ .sub_path = path, .data = output.items, .flags = .{ .truncate = true, .permissions = .default_file } });
}
test "evidence verification hashes selected lines and normalizes CRLF" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "source.txt", .data = "one\r\ntwo\r\nthree\n" });

    var digest: [32]u8 = undefined;
    Sha256.hash("two\n", &digest, .{});
    const hash = std.fmt.bytesToHex(digest, .lower);
    const item = contract.Evidence{
        .evidence_id = "e1", .kind = "source", .path = "source.txt", .commit = "h",
        .line_start = 2, .line_end = 2, .source = "test", .content_hash = &hash,
        .collected_at = "now", .verifier_status = "verified",
    };
    const outcome = try verifyEvidence(arena.allocator(), tmp.dir, item, false);
    try std.testing.expect(outcome.outcome == .verified);
    const mismatch = verifyEvidence(arena.allocator(), tmp.dir, .{ .evidence_id = "e1", .kind = "source", .path = "source.txt", .commit = "h", .line_start = 2, .line_end = 2, .source = "test", .content_hash = &([_]u8{'0'} ** 64), .collected_at = "now", .verifier_status = "verified" }, false) catch |err| switch (err) {
        error.OutOfMemory => return err,
    };
    try std.testing.expectEqualStrings(&hash, mismatch.outcome.hash_mismatch orelse "missing");
}

test "evidence verification rejects unsafe paths and invalid ranges" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "source.txt", .data = "one\n" });
    const base = contract.Evidence{ .evidence_id = "e1", .kind = "source", .path = "source.txt", .commit = "h", .line_start = 1, .line_end = 1, .source = "test", .content_hash = "", .collected_at = "now", .verifier_status = "verified" };
    const invalid = try verifyEvidence(arena.allocator(), tmp.dir, .{ .evidence_id = base.evidence_id, .kind = base.kind, .path = "../source.txt", .commit = base.commit, .line_start = base.line_start, .line_end = base.line_end, .source = base.source, .content_hash = base.content_hash, .collected_at = base.collected_at, .verifier_status = base.verifier_status }, false);
    try std.testing.expect(invalid.outcome == .invalid_path);
    try std.testing.expectEqual(@as(usize, 1), (try verifyEvidence(arena.allocator(), tmp.dir, .{ .evidence_id = base.evidence_id, .kind = base.kind, .path = base.path, .commit = base.commit, .line_start = 3, .line_end = 3, .source = base.source, .content_hash = base.content_hash, .collected_at = base.collected_at, .verifier_status = base.verifier_status }, false)).outcome.bad_range);
}

test "relocation window hashing agrees with the reference line hashing" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    // The relocation path locates lines from a precomputed start index while
    // verification walks the file from byte zero; the two must agree on every
    // range of every awkward payload. This is the differential reference.
    const payloads = [_][]const u8{
        "",                              "\n",
        "\n\n",                          "a",
        "a\n",                           "a\nb\n",
        "a\n\nb\n",                      "a\r\nb\r\n",
        "a\r\nb",                        "a\r\n\r\nb\r\n",
        "a\rb\n",                        "a\rb",
        "a\nb\r",                        "b\r",
        "\r",                            "a\r\nb\r",
        "héllo\n🎉\n",                  "x\xff\xfey\n",
        "\xff\n\xfe\n",                  "one\ntwo\nthree\nfour\nfive\n",
        "one\r\ntwo\r\nthree\r\nfour\r\nfive", " \n\t\n\n  \n",
        "a\n\n\n\n\n\n\n\nb\n",
    };
    for (payloads) |data| {
        const starts = try lineStarts(allocator, data);
        const total = countNormalizedLines(data);
        try std.testing.expectEqual(total, starts.len);
        var start: usize = 1;
        while (start <= total) : (start += 1) {
            var end: usize = start;
            while (end <= total) : (end += 1) {
                var selected: std.ArrayList(u8) = .empty;
                try appendSelectedLines(data, @intCast(start), @intCast(end), &selected, allocator);
                var digest: [32]u8 = undefined;
                Sha256.hash(selected.items, &digest, .{});
                const reference = std.fmt.bytesToHex(digest, .lower);
                const got = windowHash(data, starts, start, end);
                try std.testing.expectEqualStrings(&reference, &got);
            }
        }
    }
}

test "relocation reports moved, absent, and exact-fit windows" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const data = "alpha\nbeta\ngamma\ndelta\n";
    const total = countNormalizedLines(data);
    const starts = try lineStarts(allocator, data);
    try std.testing.expectEqual(@as(usize, 4), total);
    const zeros = [_]u8{'0'} ** 64;

    // Moved: the cited 1-2 actually lives at 3-4, reported with a signed shift.
    const recorded = windowHash(data, starts, 3, 4);
    try std.testing.expectEqualStrings(
        "; content relocates to lines 3-4 (shift +2; nearest matching window)",
        try relocationClause(allocator, data, total, 1, 2, &recorded),
    );

    // Absent with the whole file covered: absence is earned, not assumed.
    try std.testing.expectEqualStrings(
        "; content not found elsewhere in this file",
        try relocationClause(allocator, data, total, 1, 2, &zeros),
    );

    // Exact fit: the window is the whole file, so only one start is valid and
    // the cited range past the end clamps onto it.
    const whole = windowHash(data, starts, 1, 4);
    const clamped = resolveAnchor(data, starts, total, 9000, 9003, &whole);
    try std.testing.expect(clamped.found);
    try std.testing.expectEqual(@as(i64, 1), clamped.line_start);
    try std.testing.expectEqual(@as(i64, 4), clamped.line_end);
    try std.testing.expectEqual(@as(i64, -8999), clamped.shift);

    // Exact fit at the file's last valid start.
    const tail = windowHash(data, starts, 4, 4);
    const tail_result = resolveAnchor(data, starts, total, 1, 1, &tail);
    try std.testing.expect(tail_result.found);
    try std.testing.expectEqual(@as(i64, 4), tail_result.line_start);
    try std.testing.expectEqual(@as(i64, 4), tail_result.line_end);
    try std.testing.expectEqual(@as(i64, 3), tail_result.shift);

    // A reversed range cannot describe a window and yields no clause at all.
    try std.testing.expectEqualStrings("", try relocationClause(allocator, data, total, 2, 1, &zeros));
}

test "relocation stops on the byte budget instead of claiming absence" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const unit = "x" ** 63 ++ "\n";
    var big: std.ArrayList(u8) = .empty;
    for (0..40000) |_| try big.appendSlice(allocator, unit);
    const data = big.items;
    const total = countNormalizedLines(data);
    try std.testing.expectEqual(@as(usize, 40000), total);
    const starts = try lineStarts(allocator, data);
    const zeros = [_]u8{'0'} ** 64;

    const anchor = resolveAnchor(data, starts, total, 20000, 20019, &zeros);
    try std.testing.expect(!anchor.found);
    try std.testing.expect(!anchor.exhaustive);
    try std.testing.expectEqualStrings(
        "; relocation search stopped after its byte budget",
        try relocationClause(allocator, data, total, 20000, 20019, &zeros),
    );
}

test "relocation is diagnostic only and gated by the option" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const contents = "alpha\nbeta\ngamma\ndelta\n";
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "notes.txt", .data = contents });
    const starts = try lineStarts(allocator, contents);
    const recorded = windowHash(contents, starts, 3, 4);
    const content_hash = try std.fmt.allocPrint(allocator, "sha256:{s}", .{&recorded});
    const item = contract.Evidence{ .evidence_id = "e1", .kind = "source", .path = "notes.txt", .commit = "h", .line_start = 1, .line_end = 2, .source = "test", .content_hash = content_hash, .collected_at = "now", .verifier_status = "verified" };

    const annotated = try verifyEvidence(allocator, tmp.dir, item, true);
    try std.testing.expect(annotated.outcome == .hash_mismatch);
    try std.testing.expectEqualStrings("; content relocates to lines 3-4 (shift +2; nearest matching window)", annotated.clause);

    // Without the option the message keeps its pre-relocation wording.
    const plain = try verifyEvidence(allocator, tmp.dir, item, false);
    try std.testing.expect(plain.outcome == .hash_mismatch);
    try std.testing.expectEqualStrings("", plain.clause);
}

test "relocate is accepted only by evidence verify" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // evidence verify accepts the switch (and then reports the missing paths).
    const missing = try run(allocator, &.{ "evidence", "verify", "--relocate" });
    try std.testing.expectEqual(@as(u8, 2), missing.code);
    try std.testing.expect(std.mem.indexOf(u8, missing.output, "both --repo <dir> and --packet <file> are required") != null);

    const contract_flag = try run(allocator, &.{ "contract", "validate", "--packet", "absent.json", "--relocate" });
    try std.testing.expectEqual(@as(u8, 2), contract_flag.code);
    try std.testing.expect(std.mem.indexOf(u8, contract_flag.output, "unknown flag or argument '--relocate'") != null);

    const node_flag = try run(allocator, &.{ "node", "verify", "--relocate" });
    try std.testing.expectEqual(@as(u8, 2), node_flag.code);
    try std.testing.expect(std.mem.indexOf(u8, node_flag.output, "unknown flag or argument '--relocate'") != null);
}

test "unknown field names are quoted like Go strconv.Quote" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const cases = [_][2][]const u8{
        // '"' and '\' are always backslashed.
        .{ "a\"b\\c", "\"a\\\"b\\\\c\"" },
        // The named C escapes.
        .{ "a\x07b", "\"a\\ab\"" },
        .{ "a\x08b", "\"a\\bb\"" },
        .{ "a\x0cb", "\"a\\fb\"" },
        .{ "a\nb", "\"a\\nb\"" },
        .{ "a\rb", "\"a\\rb\"" },
        .{ "a\tb", "\"a\\tb\"" },
        .{ "a\x0bb", "\"a\\vb\"" },
        // Other control bytes and DEL use \xNN with lowercase hex.
        .{ "x\x01y\x1fz\rw", "\"x\\x01y\\x1fz\\rw\"" },
        .{ "evidence\x7fid", "\"evidence\\x7fid\"" },
        // Printable runes stay literal, Latin-1 and astral included.
        .{ "héllo", "\"héllo\"" },
        .{ "héllo🎉", "\"héllo🎉\"" },
        .{ "a\"b\\c\td\ne\x7ff", "\"a\\\"b\\\\c\\td\\ne\\x7ff\"" },
        // Non-printable BMP runes use \uNNNN, astral ones \UNNNNNNNN.
        .{ "héllo🎉\u{00a0}\u{2028}", "\"héllo🎉\\u00a0\\u2028\"" },
        .{ "astral\u{10fffe}end", "\"astral\\U0010fffeend\"" },
    };
    for (cases) |case| {
        const got = try quoteGoString(allocator, case[0]);
        try std.testing.expectEqualStrings(case[1], got);
    }
}

test "printability matches Go for representative runes" {
    try std.testing.expect(strconvIsPrint(' '));
    try std.testing.expect(strconvIsPrint('~'));
    try std.testing.expect(!strconvIsPrint(0x1f));
    try std.testing.expect(!strconvIsPrint(0x7f));
    try std.testing.expect(strconvIsPrint(0xe9)); // é, Latin-1
    try std.testing.expect(!strconvIsPrint(0xa0)); // NBSP
    try std.testing.expect(!strconvIsPrint(0xad)); // soft hyphen
    try std.testing.expect(strconvIsPrint(0x1f389)); // 🎉
    try std.testing.expect(!strconvIsPrint(0x2028)); // line separator
    try std.testing.expect(strconvIsPrint(0x20000)); // CJK extension B
    try std.testing.expect(!strconvIsPrint(0x10fffe)); // astral noncharacter
}

test "decode precedence follows document order in both directions" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();

    // An unknown field before the wrongly-typed one wins.
    const unknown_first = try json.Parser.parse(allocator, "{\"budget\":{\"aaa_unknown\":1,\"max_evidence\":\"notanint\"}}");
    try std.testing.expectEqual(DecodeIssue.unknown, firstIssueKind(unknown_first, true));
    try std.testing.expectEqualStrings("aaa_unknown", unknownPacketField(unknown_first).?);

    // A wrongly-typed field before the unknown one wins instead.
    const unknown_last = try json.Parser.parse(allocator, "{\"budget\":{\"max_evidence\":\"notanint\",\"zzz_unknown\":1}}");
    try std.testing.expectEqual(DecodeIssue.wrong_type, firstIssueKind(unknown_last, true));

    // The same two shapes reach the reference messages end to end.
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "unknown_first.json", .data = "{\"budget\":{\"aaa_unknown\":1,\"max_evidence\":\"notanint\"}}" });
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "unknown_last.json", .data = "{\"budget\":{\"max_evidence\":\"notanint\",\"zzz_unknown\":1}}" });
    const first_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/unknown_first.json", .{tmp.sub_path});
    const last_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/unknown_last.json", .{tmp.sub_path});

    const first_result = try run(allocator, &.{ "contract", "validate", "--packet", first_path });
    try std.testing.expectEqual(@as(u8, 2), first_result.code);
    try std.testing.expect(std.mem.indexOf(u8, first_result.output, "json: unknown field \"aaa_unknown\"") != null);

    const last_result = try run(allocator, &.{ "contract", "validate", "--packet", last_path });
    try std.testing.expectEqual(@as(u8, 2), last_result.code);
    try std.testing.expect(std.mem.indexOf(u8, last_result.output, "json: cannot unmarshal string into Go struct field Packet.budget.max_evidence of type int") != null);
}

test "field names fold like encoding/json" {
    try std.testing.expect(gofold.foldedEqual("SCHEMA_VERSION", "schema_version"));
    try std.testing.expect(gofold.foldedEqual("LiNe_EnD", "line_end"));
    try std.testing.expect(!gofold.foldedEqual("schema_version", "schema_versions"));
    // A non-ASCII rune folds to the minimum of its SimpleFold orbit. That can
    // never equal an ASCII tag, except for the two runes whose orbit contains
    // one: long s folds to S and KELVIN folds to K.
    try std.testing.expectEqual(@as(u21, 'S'), gofold.foldRune(0x17F));
    try std.testing.expectEqual(@as(u21, 'K'), gofold.foldRune(0x212A));
    try std.testing.expectEqual(@as(u21, 0xC9), gofold.foldRune(0xE9)); // é -> É
    try std.testing.expect(gofold.foldedEqual("ſchema_version", "schema_version"));
    try std.testing.expect(!gofold.foldedEqual("öutcome", "outcome"));
}

test "contract and evidence fold field names while node stays exact" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "case.json", .data = "{\"SCHEMA_VERSION\":\"v1\",\"OUTCOME\":\"complete\"}" });
    const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/case.json", .{tmp.sub_path});

    // The contract decoder folds, so the case-varied keys are known and the
    // packet reaches contract validation (missing fields) instead of failing
    // to decode.
    const contract_result = try run(allocator, &.{ "contract", "validate", "--packet", path });
    try std.testing.expectEqual(@as(u8, 1), contract_result.code);
    try std.testing.expect(std.mem.indexOf(u8, contract_result.output, "packet is invalid") != null);
    try std.testing.expect(std.mem.indexOf(u8, contract_result.output, "unknown field") == null);

    // node verify decodes through internal/nodepacket, which is exact only.
    const node_result = try run(allocator, &.{ "node", "verify", "--repo", ".", "--packet", path, "--envelope", "e.json", "--ledger", "l.jsonl" });
    try std.testing.expectEqual(@as(u8, 2), node_result.code);
    try std.testing.expect(std.mem.indexOf(u8, node_result.output, "strict packet decoding failed") != null);
}

test "type errors on case-varied keys keep the input spelling" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const cases = [_][2][]const u8{
        .{ "top.json", "{\"PACKET_ID\":5}" },
        .{ "evidence.json", "{\"evidence\":[{\"LINE_START\":\"x\"}]}" },
        .{ "budget.json", "{\"budget\":{\"MAX_BYTES\":\"x\"}}" },
        .{ "freshness.json", "{\"FRESHNESS\":{\"CURRENT\":\"x\"}}" },
    };
    const expected = [_][]const u8{
        "Packet.PACKET_ID of type string",
        "Packet.evidence.0.LINE_START of type int",
        "Packet.budget.MAX_BYTES of type int64",
        "Packet.FRESHNESS.CURRENT of type bool",
    };
    for (cases, expected) |case, want| {
        try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = case[0], .data = case[1] });
        const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/{s}", .{ tmp.sub_path, case[0] });
        const response = try run(allocator, &.{ "contract", "validate", "--packet", path });
        try std.testing.expectEqual(@as(u8, 2), response.code);
        try std.testing.expect(std.mem.indexOf(u8, response.output, want) != null);
    }
}

test "non-integral and out-of-range integers name the literal" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const cases = [_][2][]const u8{
        .{ "{\"budget\":{\"max_evidence\":1.5}}", "json: cannot unmarshal number 1.5 into Go struct field Packet.budget.max_evidence of type int" },
        .{ "{\"budget\":{\"max_bytes\":1e2}}", "json: cannot unmarshal number 1e2 into Go struct field Packet.budget.max_bytes of type int64" },
        .{ "{\"budget\":{\"used_evidence\":99999999999999999999}}", "json: cannot unmarshal number 99999999999999999999 into Go struct field Packet.budget.used_evidence of type int" },
        .{ "{\"evidence\":[{\"line_start\":1.5}]}", "json: cannot unmarshal number 1.5 into Go struct field Packet.evidence.0.line_start of type int" },
        .{ "{\"budget\":{\"max_evidence\":\"x\"}}", "json: cannot unmarshal string into Go struct field Packet.budget.max_evidence of type int" },
    };
    for (cases, 0..) |case, index| {
        const name = try std.fmt.allocPrint(allocator, "num{d}.json", .{index});
        try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = name, .data = case[0] });
        const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/{s}", .{ tmp.sub_path, name });
        const response = try run(allocator, &.{ "contract", "validate", "--packet", path });
        try std.testing.expectEqual(@as(u8, 2), response.code);
        try std.testing.expect(std.mem.indexOf(u8, response.output, case[1]) != null);
    }
}

test "envelope identifiers follow node.validateIdentifier" {
    try std.testing.expect(validIdentifier("a"));
    try std.testing.expect(validIdentifier("packet-1"));
    try std.testing.expect(validIdentifier("a.b_c-d:e"));
    try std.testing.expect(validIdentifier("Z9"));
    try std.testing.expect(!validIdentifier(""));
    try std.testing.expect(!validIdentifier("-env-1")); // '.' '_' '-' ':' not first
    try std.testing.expect(!validIdentifier(".env"));
    try std.testing.expect(!validIdentifier("env%lope-1"));
    try std.testing.expect(!validIdentifier("no de"));
    try std.testing.expect(validIdentifier("e" ** 128));
    try std.testing.expect(!validIdentifier("e" ** 129));
}

test "strict node decoding rejects duplicate keys, nulls, and invalid utf8" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();

    // Duplicate keys: encoding/json keeps the last, nodepacket rejects.
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "dup.json", .data = "{\"packet_id\":\"a\",\"packet_id\":\"b\"}" });
    const dup_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/dup.json", .{tmp.sub_path});
    try std.testing.expectEqualStrings("b", (try loadPacket(allocator, dup_path)).packet_id);
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, dup_path));

    // An explicit null is absent for the contract path and rejected by node.
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "null.json", .data = "{\"issued_at\":null}" });
    const null_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/null.json", .{tmp.sub_path});
    try std.testing.expectEqualStrings("", (try loadPacket(allocator, null_path)).issued_at);
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, null_path));

    // Invalid UTF-8: the contract path substitutes U+FFFD, node rejects.
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "bad.json", .data = "{\"packet_id\":\"a\xff\"}" });
    const bad_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/bad.json", .{tmp.sub_path});
    try std.testing.expectEqualStrings("a\u{fffd}", (try loadPacket(allocator, bad_path)).packet_id);
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, bad_path));

    // A lone surrogate escape is U+FFFD for the contract path and rejected by
    // node, which checks the raw escapes before tokenizing.
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "surrogate.json", .data = "{\"packet_id\":\"\\uD800\"}" });
    const surrogate_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/surrogate.json", .{tmp.sub_path});
    try std.testing.expectEqualStrings("\u{fffd}", (try loadPacket(allocator, surrogate_path)).packet_id);
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, surrogate_path));
}

test "strict node decoding accepts the exact valid shape" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const valid = "{\"packet_id\":\"p\",\"freshness\":{\"current\":true},\"budget\":{\"max_evidence\":1},\"evidence\":[{\"line_start\":1}],\"degradations\":[\"x\"]}";
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "valid.json", .data = valid });
    const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/valid.json", .{tmp.sub_path});
    const packet = try loadNodePacket(allocator, path);
    try std.testing.expectEqualStrings("p", packet.packet_id);
    try std.testing.expect(packet.freshness.current);
    try std.testing.expectEqual(@as(i64, 1), packet.budget.max_evidence);
    try std.testing.expectEqual(@as(i64, 1), packet.evidence[0].line_start);
    // A case-varied or wrongly typed key is rejected by the strict path.
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "case.json", .data = "{\"PACKET_ID\":\"p\"}" });
    const case_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/case.json", .{tmp.sub_path});
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, case_path));
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "frac.json", .data = "{\"budget\":{\"max_evidence\":1.5}}" });
    const frac_path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/frac.json", .{tmp.sub_path});
    try std.testing.expectError(error.WrongType, loadNodePacket(allocator, frac_path));
}

test "envelope validation rejects duplicate nodes and cycles" {
    const evidence = contract.Evidence{ .evidence_id = "e", .kind = "source", .path = "x", .commit = "h", .line_start = 1, .line_end = 1, .source = "t", .content_hash = "h", .collected_at = "n", .verifier_status = "verified" };
    var evidence_items = [_]contract.Evidence{evidence};
    const packet = contract.Packet{
        .packet_id = "p", .schema_version = "v1", .repo_root = ".", .head_commit = "h", .request_id = "r", .issued_at = "n", .outcome = "partial",
        .freshness = .{ .head_commit = "", .head_anchor = "", .status = "", .current = false, .is_current = false, .checked_at = "" },
        .authorization = .{ .level = "", .reason = "" }, .budget = .{ .max_evidence = 1, .used_evidence = 1, .max_bytes = 1, .used_bytes = 1 },
        .evidence = evidence_items[0..], .degradations = &.{}, .provenance = .{ .collector = "", .tool = "", .version = "", .tool_version = "" }, .packet_hash = "x",
        .freshness_present = true, .authorization_present = true, .budget_present = true, .evidence_present = true, .degradations_present = true, .provenance_present = true,
    };
    const binding = [_]u8{'b'} ** 64;
    const deps = [_][]const u8{"a"};
    const ids = [_][]const u8{"e"};
    var duplicate = [_]Node{
        .{ .node_id = "a", .depends_on = &.{}, .verifier = "evidence.current", .evidence_ids = &ids },
        .{ .node_id = "a", .depends_on = &.{}, .verifier = "evidence.current", .evidence_ids = &ids },
    };
    try std.testing.expectError(error.InvalidEnvelope, validateEnvelope(.{ .schema_version = "node-envelope-v1", .envelope_id = "e", .packet_id = "p", .packet_binding_sha256 = &binding, .nodes = &duplicate }, packet, binding));
    const cycle = [_]Node{
        .{ .node_id = "a", .depends_on = &deps, .verifier = "evidence.current", .evidence_ids = &ids },
        .{ .node_id = "b", .depends_on = &.{ "a" }, .verifier = "evidence.current", .evidence_ids = &ids },
    };
    const cycle_deps = [_][]const u8{"b"};
    var cycle_nodes = [_]Node{
        .{ .node_id = "a", .depends_on = &cycle_deps, .verifier = "evidence.current", .evidence_ids = &ids },
        cycle[1],
    };
    try std.testing.expectError(error.InvalidEnvelope, validateEnvelope(.{ .schema_version = "node-envelope-v1", .envelope_id = "e", .packet_id = "p", .packet_binding_sha256 = &binding, .nodes = &cycle_nodes }, packet, binding));
}

test "ledger append chains sequence and previous hash" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const path = try std.fmt.allocPrint(arena.allocator(), ".zig-cache/tmp/{s}/ledger.jsonl", .{tmp.sub_path});
    const envelope_hash = [_]u8{'a'} ** 64;
    const binding = [_]u8{'b'} ** 64;
    const details = [_][]const u8{"node \"n1\": evidence_current"};
    try appendLedger(arena.allocator(), path, envelope_hash, binding, &details);
    try appendLedger(arena.allocator(), path, envelope_hash, binding, &details);
    const data = try std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, arena.allocator(), .limited(1 << 20));
    var lines = std.mem.splitScalar(u8, data, '\n');
    const first = try json.Parser.parse(arena.allocator(), lines.next().?);
    const second = try json.Parser.parse(arena.allocator(), lines.next().?);
    try std.testing.expectEqualStrings("1", first.objectField("seq").?.number);
    try std.testing.expectEqualStrings("2", second.objectField("seq").?.number);
    try std.testing.expectEqualStrings(first.objectField("record_hash").?.string, second.objectField("prev_record_hash").?.string);
}

fn replaceFirst(allocator: std.mem.Allocator, text: []const u8, needle: []const u8, replacement: []const u8) ![]u8 {
    const index = std.mem.indexOf(u8, text, needle) orelse return error.NotFound;
    var out = std.ArrayList(u8).empty;
    try out.appendSlice(allocator, text[0..index]);
    try out.appendSlice(allocator, replacement);
    try out.appendSlice(allocator, text[index + needle.len ..]);
    return try out.toOwnedSlice(allocator);
}

test "ledger canonical hash matches the reference encoding" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const build_result = [_]LedgerNodeResult{.{ .node_id = "build", .status = "evidence_current" }};
    const record = LedgerRecord{
        .schema_version = "ownscout-ledger-v1",
        .seq = 1,
        .prev_record_hash = ledger_zero_hash,
        .record_hash = "",
        .envelope_sha256 = "2c4f1e238788baeeb21f070e98a4bae098218439c1bdbd5f5df4e8f74df710d3",
        .packet_binding_sha256 = "63d0897ae0c01ee58180a5240dfeea403d99a9d7f6d42e079af6330e8e7267ca",
        .ownscout_version = "0.1.0",
        .node_results = &build_result,
    };
    try std.testing.expectEqualStrings("15f1081bbfe69a07da19cc3ae59930638980bb944be90ac1e2db2726cad63ed6", &(try ledgerRecordHash(allocator, record)));

    // reason is included only when non-empty, and its quotes are escaped.
    const failed = [_]LedgerNodeResult{
        .{ .node_id = "a", .status = "failed", .reason = "evidence \"evidence-1\" is missing or not verified" },
        .{ .node_id = "b", .status = "blocked", .reason = "dependency \"a\" is not evidence_current" },
    };
    const stale = LedgerRecord{
        .schema_version = "ownscout-ledger-v1",
        .seq = 1,
        .prev_record_hash = ledger_zero_hash,
        .record_hash = "",
        .envelope_sha256 = "3fd0720a467d8d703a0f21719b904a94bd3de06f0da3602084222ea5f392ca36",
        .packet_binding_sha256 = "6388241add28c6bcf26d1a146a67d47331076fc1acbbc4a5c56701527db2c337",
        .ownscout_version = "0.1.0",
        .node_results = &failed,
    };
    try std.testing.expectEqualStrings("93fb4f66b99266af232b7f5b9f2a4990528f88222a0702549f4d4f439cf86570", &(try ledgerRecordHash(allocator, stale)));

    // json.Marshal escaping: HTML characters, the named C escapes (including
    // \b and \f), and U+2028/U+2029 all have to match for the hash to line up.
    const special = [_]LedgerNodeResult{.{ .node_id = "build", .status = "failed", .reason = "<a>&\"b\\c\x08\x0c\u{2028}\u{2029}z\ttab" }};
    const escaped = LedgerRecord{
        .schema_version = "ownscout-ledger-v1",
        .seq = 1,
        .prev_record_hash = ledger_zero_hash,
        .record_hash = "",
        .envelope_sha256 = "2c4f1e238788baeeb21f070e98a4bae098218439c1bdbd5f5df4e8f74df710d3",
        .packet_binding_sha256 = "63d0897ae0c01ee58180a5240dfeea403d99a9d7f6d42e079af6330e8e7267ca",
        .ownscout_version = "0.1.0",
        .node_results = &special,
    };
    try std.testing.expectEqualStrings("6442268913c9a628cec5a076b5dad248cb6c0b6717db1b9378b9e5728aeff9f7", &(try ledgerRecordHash(allocator, escaped)));
}

test "ledger validation rejects forged records and preserves the chain" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/ledger.jsonl", .{tmp.sub_path});
    const envelope = [_]u8{'a'} ** 64;
    const binding = [_]u8{'b'} ** 64;
    const ones = [_]u8{'1'} ** 64;
    const effs = [_]u8{'f'} ** 64;
    const details = [_][]const u8{"node \"build\": evidence_current"};

    try appendLedger(allocator, path, envelope, binding, &details);
    const good = try std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20));
    const marker = "\"record_hash\":\"";
    const hash_start = std.mem.indexOf(u8, good, marker).? + marker.len;
    const stored_hash = good[hash_start .. hash_start + 64];

    const forgeries = [_][2][]const u8{
        .{ stored_hash, ledger_zero_hash }, // forged record_hash
        .{ "\"seq\":1", "\"seq\":99" }, // broken sequence
        .{ ledger_zero_hash, &effs }, // forged prev_record_hash
        .{ "\"schema_version\":\"ownscout-ledger-v1\"", "\"schema_version\":\"WRONG\"" },
        .{ "\"status\":\"evidence_current\"", "\"status\":\"bogus\"" },
        .{ "\"schema_version\"", "\"SCHEMA_VERSION\"" }, // non-canonical field name
        .{ &envelope, &ones }, // forged envelope_sha256 (hash no longer matches)
    };
    for (forgeries) |forgery| {
        const text = try replaceFirst(allocator, good, forgery[0], forgery[1]);
        try std.Io.Dir.cwd().writeFile(std.Options.debug_io, .{ .sub_path = path, .data = text, .flags = .{ .truncate = true, .permissions = .default_file } });
        try std.testing.expectError(error.InvalidLedger, appendLedger(allocator, path, envelope, binding, &details));
    }

    // An unmodified ledger still appends.
    try std.Io.Dir.cwd().writeFile(std.Options.debug_io, .{ .sub_path = path, .data = good, .flags = .{ .truncate = true, .permissions = .default_file } });
    try appendLedger(allocator, path, envelope, binding, &details);
    const appended = try std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20));
    try std.testing.expect(appended.len > good.len);
    try std.testing.expect(std.mem.startsWith(u8, appended, good));
}

test "ledger validation rejects malformed framing" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    const path = try std.fmt.allocPrint(allocator, ".zig-cache/tmp/{s}/ledger.jsonl", .{tmp.sub_path});
    const envelope = [_]u8{'a'} ** 64;
    const binding = [_]u8{'b'} ** 64;
    const details = [_][]const u8{"node \"build\": evidence_current"};

    const frames = [_][]const u8{
        "{}", // no trailing LF
        "\n", // blank line
        "not a ledger record\n", // not JSON
        "{\"schema_version\":\"ownscout-ledger-v1\"}\n", // missing fields
    };
    for (frames) |frame| {
        try std.Io.Dir.cwd().writeFile(std.Options.debug_io, .{ .sub_path = path, .data = frame, .flags = .{ .truncate = true, .permissions = .default_file } });
        try std.testing.expectError(error.InvalidLedger, appendLedger(allocator, path, envelope, binding, &details));
    }
}

test "CLI exposes stable help and version output" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const help = try run(arena.allocator(), &.{ "--help" });
    try std.testing.expectEqual(@as(u8, 0), help.code);
    try std.testing.expect(std.mem.indexOf(u8, help.output, "Usage:") != null);
    const version = try run(arena.allocator(), &.{ "version" });
    try std.testing.expectEqual(@as(u8, 0), version.code);
    try std.testing.expectEqualStrings("ownscout 0.1.0\n", version.output);
}

test "CLI returns usage errors with the documented exit code" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const missing = try run(arena.allocator(), &.{});
    try std.testing.expectEqual(@as(u8, 2), missing.code);
    try std.testing.expect(std.mem.indexOf(u8, missing.output, "a command is required") != null);
    const unknown = try run(arena.allocator(), &.{ "unknown" });
    try std.testing.expectEqual(@as(u8, 2), unknown.code);
    try std.testing.expect(std.mem.indexOf(u8, unknown.output, "unknown command") != null);
}

test "CLI JSON errors preserve machine-readable output shape" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const response = try run(arena.allocator(), &.{ "contract", "validate", "--json" });
    try std.testing.expectEqual(@as(u8, 2), response.code);
    const value = try json.Parser.parse(arena.allocator(), response.output);
    try std.testing.expectEqualStrings("usage", value.objectField("command").?.string);
    try std.testing.expectEqual(false, value.objectField("ok").?.boolean);
    try std.testing.expect(value.objectField("details").?.array.len == 1);
}

test "CLI doctor rejects extra arguments" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const response = try run(arena.allocator(), &.{ "doctor", "extra" });
    try std.testing.expectEqual(@as(u8, 2), response.code);
    try std.testing.expect(std.mem.indexOf(u8, response.output, "does not accept arguments") != null);
}
