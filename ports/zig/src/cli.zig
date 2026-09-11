const std = @import("std");
const result = @import("result.zig");
const json = @import("json.zig");
const contract = @import("contract.zig");
const Sha256 = std.crypto.hash.sha2.Sha256;

pub const RootUsage =
    "OwnScout — local repository evidence checks\n\n" ++
    "Usage:\n  ownscout doctor\n  ownscout version\n  ownscout contract validate --packet <file> [--json]\n  ownscout evidence verify --repo <dir> --packet <file> [--json]\n  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\n" ++
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
        if (std.mem.eql(u8, args[0], "evidence")) text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--json]\n\nVerifies packet evidence spans against a local repository.";
        if (std.mem.eql(u8, args[0], "node")) text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nVerifies a node-envelope-v1 graph against fresh repository evidence and records the ordered results.";
    }
    if (args.len >= 2 and std.mem.eql(u8, args[0], "contract") and std.mem.eql(u8, args[1], "validate")) text = "Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.";
    if (args.len >= 2 and std.mem.eql(u8, args[0], "evidence") and std.mem.eql(u8, args[1], "verify")) text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--json]\n\nValidates the packet, then checks each evidence span locally.\n\nNext action: provide both paths and rerun.";
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

fn knownField(name: []const u8, fields: []const []const u8) bool {
    for (fields) |field| {
        if (std.mem.eql(u8, name, field)) return true;
    }
    return false;
}

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
    const packet_fields = [_][]const u8{
        "packet_id", "schema_version", "repo_root", "head_commit", "request_id", "issued_at", "outcome",
        "freshness", "authorization", "budget", "evidence", "degradations", "provenance", "packet_hash",
    };
    for (root.object) |field| {
        if (!knownField(field.key, &packet_fields)) return field.key;
        const nested_fields: []const []const u8 = if (std.mem.eql(u8, field.key, "freshness"))
            &[_][]const u8{ "head_commit", "head_anchor", "status", "current", "is_current", "checked_at" }
        else if (std.mem.eql(u8, field.key, "authorization"))
            &[_][]const u8{ "level", "reason" }
        else if (std.mem.eql(u8, field.key, "budget"))
            &[_][]const u8{ "max_evidence", "used_evidence", "max_bytes", "used_bytes" }
        else if (std.mem.eql(u8, field.key, "provenance"))
            &[_][]const u8{ "collector", "tool", "version", "tool_version" }
        else
            &[_][]const u8{};
        if (field.value == .object) {
            for (field.value.object) |nested| {
                if (!knownField(nested.key, nested_fields)) return nested.key;
            }
        }
        if (std.mem.eql(u8, field.key, "evidence") and field.value == .array) {
            const evidence_fields = [_][]const u8{
                "evidence_id", "kind", "path", "commit", "line_start", "line_end", "source",
                "content_hash", "collected_at", "verifier_status",
            };
            for (field.value.array) |item| {
                if (item != .object) continue;
                for (item.object) |nested| {
                    if (!knownField(nested.key, &evidence_fields)) return nested.key;
                }
            }
        }
    }
    return null;
}

fn firstUnknownTopIndex(root: json.Value) ?usize {
    if (root != .object) return null;
    const packet_fields = [_][]const u8{
        "packet_id", "schema_version", "repo_root", "head_commit", "request_id", "issued_at", "outcome",
        "freshness", "authorization", "budget", "evidence", "degradations", "provenance", "packet_hash",
    };
    for (root.object, 0..) |field, i| {
        if (!knownField(field.key, &packet_fields)) return i;
        const nested_fields: []const []const u8 = if (std.mem.eql(u8, field.key, "freshness"))
            &[_][]const u8{ "head_commit", "head_anchor", "status", "current", "is_current", "checked_at" }
        else if (std.mem.eql(u8, field.key, "authorization"))
            &[_][]const u8{ "level", "reason" }
        else if (std.mem.eql(u8, field.key, "budget"))
            &[_][]const u8{ "max_evidence", "used_evidence", "max_bytes", "used_bytes" }
        else if (std.mem.eql(u8, field.key, "provenance"))
            &[_][]const u8{ "collector", "tool", "version", "tool_version" }
        else
            &[_][]const u8{};
        if (field.value == .object) {
            for (field.value.object) |nested| {
                if (!knownField(nested.key, nested_fields)) return i;
            }
        }
        if (std.mem.eql(u8, field.key, "evidence") and field.value == .array) {
            const evidence_fields = [_][]const u8{
                "evidence_id", "kind", "path", "commit", "line_start", "line_end", "source",
                "content_hash", "collected_at", "verifier_status",
            };
            for (field.value.array) |item| {
                if (item == .object) {
                    for (item.object) |nested| {
                        if (!knownField(nested.key, &evidence_fields)) return i;
                    }
                }
            }
        }
    }
    return null;
}

fn firstTypeErrorTopIndex(root: json.Value) ?usize {
    if (root != .object) return null;
    const string_fields = [_][]const u8{
        "packet_id", "schema_version", "repo_root", "head_commit", "request_id", "issued_at", "outcome", "packet_hash",
    };
    for (root.object, 0..) |field, i| {
        if (knownField(field.key, &string_fields) and field.value != .string and field.value != .null) return i;
        if (std.mem.eql(u8, field.key, "evidence") and field.value != .array and field.value != .null) return i;
        if (std.mem.eql(u8, field.key, "degradations") and field.value != .array and field.value != .null) return i;
        if ((std.mem.eql(u8, field.key, "freshness") or std.mem.eql(u8, field.key, "authorization") or
            std.mem.eql(u8, field.key, "budget") or std.mem.eql(u8, field.key, "provenance")) and
            field.value != .object and field.value != .null) return i;
        if (field.value == .object) {
            const nested_fields: []const []const u8 = if (std.mem.eql(u8, field.key, "freshness"))
                &[_][]const u8{ "head_commit", "head_anchor", "status", "current", "is_current", "checked_at" }
            else if (std.mem.eql(u8, field.key, "authorization"))
                &[_][]const u8{ "level", "reason" }
            else if (std.mem.eql(u8, field.key, "budget"))
                &[_][]const u8{ "max_evidence", "used_evidence", "max_bytes", "used_bytes" }
            else if (std.mem.eql(u8, field.key, "provenance"))
                &[_][]const u8{ "collector", "tool", "version", "tool_version" }
            else
                &[_][]const u8{};
            for (field.value.object) |nested| {
                if (!knownField(nested.key, nested_fields) or nested.value == .null) continue;
                const is_bool = std.mem.eql(u8, field.key, "freshness") and
                    (std.mem.eql(u8, nested.key, "current") or std.mem.eql(u8, nested.key, "is_current"));
                const is_integer = std.mem.eql(u8, field.key, "budget");
                if ((is_bool and nested.value != .boolean) or
                    (is_integer and nested.value != .number) or
                    (!is_bool and !is_integer and nested.value != .string)) return i;
            }
        }
        if (std.mem.eql(u8, field.key, "evidence") and field.value == .array) {
            const evidence_fields = [_][]const u8{
                "evidence_id", "kind", "path", "commit", "line_start", "line_end", "source",
                "content_hash", "collected_at", "verifier_status",
            };
            for (field.value.array) |item| {
                if (item == .null) continue;
                if (item != .object) return i;
                for (item.object) |nested| {
                    if (!knownField(nested.key, &evidence_fields) or nested.value == .null) continue;
                    const numeric = std.mem.eql(u8, nested.key, "line_start") or std.mem.eql(u8, nested.key, "line_end");
                    if ((numeric and nested.value != .number) or (!numeric and nested.value != .string)) return i;
                }
            }
        }
        if (std.mem.eql(u8, field.key, "degradations") and field.value == .array) {
            for (field.value.array) |item| if (item != .string and item != .null) return i;
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
    const unknown_index = firstUnknownTopIndex(root);
    const type_index = firstTypeErrorTopIndex(root);
    if (unknown_index != null and (type_index == null or unknown_index.? < type_index.?)) return error.UnknownField;
    if (type_index != null) return error.WrongType;
    const packet = contract.decodePacket(allocator, root) catch return error.WrongType;
    if (unknown_index != null) return error.UnknownField;
    return packet;
}

fn unknownPacketFieldFromFile(allocator: std.mem.Allocator, path: []const u8) ?[]const u8 {
    const data = std.Io.Dir.cwd().readFileAlloc(std.Options.debug_io, path, allocator, .limited(1 << 20)) catch return null;
    const trimmed = std.mem.trim(u8, data, " \t\r\n");
    const normalized = normalizeJsonUtf8(allocator, trimmed) catch return null;
    const root = json.Parser.parseAllowDuplicateKeys(allocator, normalized) catch return null;
    return unknownPacketField(root);
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
        const string_fields = [_][]const u8{
            "packet_id", "schema_version", "repo_root", "head_commit", "request_id", "issued_at", "outcome", "packet_hash",
        };
        for (root.object) |field| {
            if (knownField(field.key, &string_fields) and field.value != .string) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type string", .{ path, jsonTypeName(field.value), field.key });
            }
            if (std.mem.eql(u8, field.key, "evidence") and field.value != .array) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.evidence of type []contract.Evidence", .{ path, jsonTypeName(field.value) });
            }
            if (std.mem.eql(u8, field.key, "degradations") and field.value != .array) {
                if (field.value == .null) continue;
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.degradations of type []string", .{ path, jsonTypeName(field.value) });
            }
            if ((std.mem.eql(u8, field.key, "freshness") or std.mem.eql(u8, field.key, "authorization") or
                std.mem.eql(u8, field.key, "budget") or std.mem.eql(u8, field.key, "provenance")) and
                field.value != .object and field.value != .null)
            {
                const type_name = if (std.mem.eql(u8, field.key, "freshness")) "contract.Freshness" else if (std.mem.eql(u8, field.key, "authorization")) "contract.Authorization" else if (std.mem.eql(u8, field.key, "budget")) "contract.Budget" else "contract.Provenance";
                return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s} of type {s}", .{ path, jsonTypeName(field.value), field.key, type_name });
            }
            if (field.value == .object) {
                for (field.value.object) |nested| {
                    if (nested.value == .null) continue;
                    const is_bool = (std.mem.eql(u8, field.key, "freshness") and
                        (std.mem.eql(u8, nested.key, "current") or std.mem.eql(u8, nested.key, "is_current")));
                    const is_integer = (std.mem.eql(u8, field.key, "budget"));
                    if (is_bool and nested.value != .boolean) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type bool", .{ path, jsonTypeName(nested.value), field.key, nested.key });
                    }
                    if (is_integer and nested.value != .number) {
                        const type_name = if (std.mem.eql(u8, nested.key, "max_bytes") or std.mem.eql(u8, nested.key, "used_bytes")) "int64" else "int";
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type {s}", .{ path, jsonTypeName(nested.value), field.key, nested.key, type_name });
                    }
                    if (!is_bool and !is_integer and nested.value != .string) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.{s}.{s} of type string", .{ path, jsonTypeName(nested.value), field.key, nested.key });
                    }
                }
            }
            if (std.mem.eql(u8, field.key, "evidence") and field.value == .array) {
                for (field.value.array, 0..) |item, i| {
                    if (item == .null) continue;
                    if (item != .object) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Packet.evidence.{d} of type contract.Evidence", .{ path, jsonTypeName(item), i });
                    }
                    for (item.object) |nested| {
                        if (nested.value == .null) continue;
                        const numeric = std.mem.eql(u8, nested.key, "line_start") or std.mem.eql(u8, nested.key, "line_end");
                        if ((numeric and nested.value != .number) or (!numeric and nested.value != .string)) {
                            const type_name = if (numeric) "int" else "string";
                            return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Go struct field Packet.evidence.{d}.{s} of type {s}", .{ path, jsonTypeName(nested.value), i, nested.key, type_name });
                        }
                    }
                }
            }
            if (std.mem.eql(u8, field.key, "degradations") and field.value == .array) {
                for (field.value.array, 0..) |item, i| {
                    if (item == .null) continue;
                    if (item != .string) {
                        return try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON: json: cannot unmarshal {s} into Packet.degradations.{d} of type string", .{ path, jsonTypeName(item), i });
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
                break :blk try std.fmt.allocPrint(allocator, "packet \"{s}\" contains an unknown JSON field: json: unknown field \"{s}\"", .{ path, field });
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
    const repo = parseValueFlag(args, "--repo");
    const packet_path = parseValueFlag(args, "--packet");
    if (repo == null or packet_path == null) return fail(allocator, "both --repo <dir> and --packet <file> are required", "ownscout evidence verify --help", json_output);
    const packet = loadPacket(allocator, packet_path.?) catch |err| {
        const detail = switch (err) {
            error.NotFound => try std.fmt.allocPrint(allocator, "packet file \"{s}\" does not exist", .{packet_path.?}),
            error.InvalidJson => try std.fmt.allocPrint(allocator, "packet \"{s}\" is not valid JSON", .{packet_path.?}),
            error.ObjectRequired => try std.fmt.allocPrint(allocator, "packet \"{s}\" must contain a JSON object", .{packet_path.?}),
            error.UnknownField => blk: {
                const field = unknownPacketFieldFromFile(allocator, packet_path.?) orelse "unknown";
                break :blk try std.fmt.allocPrint(allocator, "packet \"{s}\" contains an unknown JSON field: json: unknown field \"{s}\"", .{ packet_path.?, field });
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
        const issue = verifyEvidence(allocator, root, dir, item) catch |err| {
            const msg = switch (err) {
                error.InvalidPath => if (std.mem.indexOf(u8, item.path, "..") != null) try std.fmt.allocPrint(allocator, "evidence path \"{s}\" contains a parent component", .{item.path}) else try std.fmt.allocPrint(allocator, "evidence path \"{s}\" must be repository-relative", .{item.path}),
                error.NotFound => blk: {
                    var cwd_buf: [4096]u8 = undefined;
                    const cwd_ptr = std.c.getcwd(&cwd_buf, cwd_buf.len) orelse "";
                    const cwd = std.mem.sliceTo(cwd_ptr, 0);
                    break :blk try std.fmt.allocPrint(allocator, "cannot access evidence path \"{s}\": lstat {s}/{s}/{s}: no such file or directory", .{ item.path, cwd, root, item.path });
                },
                error.BadHash => try std.fmt.allocPrint(allocator, "invalid SHA-256 content hash \"{s}\"", .{item.content_hash}),
                error.BadRange => try std.fmt.allocPrint(allocator, "invalid line range {d}-{d} for 3 line(s)", .{ item.line_start, item.line_end }),
                error.HashMismatch => blk: {
                    const actual = computeEvidenceHash(allocator, dir, item) catch null;
                    if (actual) |hash| {
                        break :blk try std.fmt.allocPrint(allocator, "content hash mismatch: expected {s}, got {s}", .{ item.content_hash, hash });
                    }
                    break :blk try std.fmt.allocPrint(allocator, "content hash mismatch: expected {s}", .{item.content_hash});
                },
                else => try std.fmt.allocPrint(allocator, "cannot verify evidence file \"{s}\"", .{item.path}),
            };
            try issues.append(allocator, try std.fmt.allocPrint(allocator, "evidence \"{s}\" (\"{s}\"): {s}", .{ item.evidence_id, item.path, msg }));
            continue;
        };
        if (issue) verified += 1;
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
    const cwd_ptr = std.c.getcwd(&cwd_buf, cwd_buf.len) orelse return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "repository could not be checked", .details = &.{ "could not determine current directory" }, .next_action = "Provide a readable repository directory with --repo <dir>." }, json_output), .code = 2 };
    const cwd = try allocator.dupe(u8, std.mem.span(@as([*:0]u8, @ptrCast(cwd_ptr))));
    const detail = try std.fmt.allocPrint(allocator, "resolve repository root \"{s}\": lstat {s}/{s}: no such file or directory", .{ repo, cwd, repo });
    const details = [_][]const u8{detail};
    return .{ .output = try result.render(allocator, .{ .command = "evidence verify", .ok = false, .summary = "repository could not be checked", .details = &details, .next_action = "Provide a readable repository directory with --repo <dir>." }, json_output), .code = 2 };
}

const VerifyError = error{InvalidPath, NotFound, BadHash, HashMismatch, BadRange, OutOfMemory};

fn verifyEvidence(allocator: std.mem.Allocator, root: []const u8, dir: std.Io.Dir, item: contract.Evidence) VerifyError!bool {
    if (item.path.len == 0 or std.fs.path.isAbsolute(item.path) or std.mem.indexOf(u8, item.path, "..") != null) return error.InvalidPath;
    const path = std.fs.path.join(allocator, &.{ root, item.path }) catch return error.NotFound;
    const data = dir.readFileAlloc(std.Options.debug_io, item.path, allocator, .limited(1 << 20)) catch return error.NotFound;
    if (item.line_start < 1 or item.line_end < item.line_start) return error.BadRange;
    var line: i64 = 1;
    var start: usize = 0;
    var selected: std.ArrayList(u8) = .empty;
    var i: usize = 0;
    while (i <= data.len) : (i += 1) {
        if (i == data.len or data[i] == '\n') {
            if (line >= item.line_start and line <= item.line_end) {
                var end = i;
                if (end > start and data[end - 1] == '\r') end -= 1;
                try selected.appendSlice(allocator, data[start..end]);
                try selected.append(allocator, '\n');
            }
            line += 1;
            start = i + 1;
        }
    }
    const line_count = line - 1;
    if (item.line_end > line_count) return error.BadRange;
    const actual = computeEvidenceHash(allocator, dir, item) catch return error.BadRange;
    var expected = item.content_hash;
    if (std.mem.startsWith(u8, expected, "sha256:")) expected = expected[7..];
    if (expected.len != 64) return error.BadHash;
    for (expected) |c| {
        if (!((c >= '0' and c <= '9') or (c >= 'a' and c <= 'f') or (c >= 'A' and c <= 'F'))) return error.BadHash;
    }
    if (!std.ascii.eqlIgnoreCase(expected, &actual)) return error.HashMismatch;
    _ = path;
    return true;
}

fn computeEvidenceHash(allocator: std.mem.Allocator, dir: std.Io.Dir, item: contract.Evidence) ![64]u8 {
    const data = try dir.readFileAlloc(std.Options.debug_io, item.path, allocator, .limited(1 << 20));
    var line: i64 = 1;
    var start: usize = 0;
    var selected: std.ArrayList(u8) = .empty;
    var i: usize = 0;
    while (i <= data.len) : (i += 1) {
        if (i == data.len or data[i] == '\n') {
            if (line >= item.line_start and line <= item.line_end) {
                var end = i;
                if (end > start and data[end - 1] == '\r') end -= 1;
                try selected.appendSlice(allocator, data[start..end]);
                try selected.append(allocator, '\n');
            }
            line += 1;
            start = i + 1;
        }
    }
    var digest: [32]u8 = undefined;
    Sha256.hash(selected.items, &digest, .{});
    return std.fmt.bytesToHex(digest, .lower);
}

fn nodeVerify(allocator: std.mem.Allocator, args: []const []const u8, json_output: bool) !RunResult {
    const repo = parseValueFlag(args, "--repo") orelse return fail(allocator, "missing required --repo value", "ownscout node verify --help", json_output);
    const packet_path = parseValueFlag(args, "--packet") orelse return fail(allocator, "missing required --packet value", "ownscout node verify --help", json_output);
    const envelope_path = parseValueFlag(args, "--envelope") orelse return fail(allocator, "missing required --envelope value", "ownscout node verify --help", json_output);
    const ledger_path = parseValueFlag(args, "--ledger") orelse return fail(allocator, "missing required --ledger value", "ownscout node verify --help", json_output);
    const packet = loadPacket(allocator, packet_path) catch |err| {
        if (err == error.WrongType or err == error.InvalidJson) {
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
            _ = verifyEvidence(allocator, root, dir, item) catch {
                const detail = try std.fmt.allocPrint(allocator, "node \"{s}\": failed (evidence \"{s}\" is missing or not verified)", .{ node.node_id, id });
                try details.append(allocator, detail);
                node_failed = true;
                break;
            };
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

fn validateEnvelope(env: Envelope, p: contract.Packet, binding: [64]u8) !void {
    if (!std.mem.eql(u8, env.schema_version, "node-envelope-v1") or env.nodes.len == 0) return error.InvalidEnvelope;
    if (!std.mem.eql(u8, env.packet_id, p.packet_id) or !std.mem.eql(u8, env.packet_binding_sha256, &binding)) return error.InvalidEnvelope;
    for (env.nodes, 0..) |node, i| {
        if (node.node_id.len == 0 or !std.mem.eql(u8, node.verifier, "evidence.current") or node.evidence_ids.len == 0) return error.InvalidEnvelope;
        for (env.nodes[0..i]) |previous| if (std.mem.eql(u8, previous.node_id, node.node_id)) return error.InvalidEnvelope;
        for (node.depends_on) |dependency| {
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

fn appendLedger(allocator: std.mem.Allocator, path: []const u8, envelope_hash: [64]u8, binding: [64]u8, details: []const []const u8) !void {
    const io = std.Options.debug_io;
    const old = std.Io.Dir.cwd().readFileAlloc(io, path, allocator, .limited(1 << 20)) catch "";
    var seq: u64 = 1;
    var previous: []const u8 = "0000000000000000000000000000000000000000000000000000000000000000";
    if (old.len != 0) {
        var lines = std.mem.splitScalar(u8, old, '\n');
        while (lines.next()) |line| {
            if (line.len == 0) continue;
            const value = try json.Parser.parse(allocator, line);
            const seq_value = value.objectField("seq") orelse return error.InvalidLedger;
            const hash_value = value.objectField("record_hash") orelse return error.InvalidLedger;
            if (seq_value != .number or hash_value != .string) return error.InvalidLedger;
            seq = try std.fmt.parseUnsigned(u64, seq_value.number, 10) + 1;
            previous = hash_value.string;
        }
    }
    var record = std.ArrayList(u8).empty;
    try record.appendSlice(allocator, "{\"schema_version\":\"ownscout-ledger-v1\",\"seq\":");
    try record.appendSlice(allocator, try std.fmt.allocPrint(allocator, "{d}", .{seq}));
    try record.appendSlice(allocator, ",\"prev_record_hash\":");
    try appendJsonString(&record, allocator, previous);
    try record.appendSlice(allocator, ",\"record_hash\":\"\",\"envelope_sha256\":");
    try appendJsonString(&record, allocator, &envelope_hash);
    try record.appendSlice(allocator, ",\"packet_binding_sha256\":");
    try appendJsonString(&record, allocator, &binding);
    try record.appendSlice(allocator, ",\"ownscout_version\":\"0.1.0\",\"node_results\":[");
    for (details, 0..) |detail, i| {
        if (i != 0) try record.append(allocator, ',');
        const node_start = std.mem.indexOf(u8, detail, "node \"") orelse continue;
        const id_start = node_start + 6;
        const id_end = std.mem.indexOfScalarPos(u8, detail, id_start, '"') orelse continue;
        const status_start = (std.mem.indexOf(u8, detail[id_end..], "\": ") orelse continue) + id_end + 3;
        const status_end = std.mem.indexOfScalarPos(u8, detail, status_start, ' ') orelse detail.len;
        try record.appendSlice(allocator, "{\"node_id\":");
        try appendJsonString(&record, allocator, detail[id_start..id_end]);
        try record.appendSlice(allocator, ",\"status\":");
        try appendJsonString(&record, allocator, detail[status_start..status_end]);
        if (std.mem.indexOf(u8, detail, "failed (")) |reason_start| {
            const start = reason_start + "failed (".len;
            const end = std.mem.lastIndexOfScalar(u8, detail, ')') orelse detail.len;
            try record.appendSlice(allocator, ",\"reason\":");
            try appendJsonString(&record, allocator, detail[start..end]);
        } else if (std.mem.indexOf(u8, detail, "blocked (")) |reason_start| {
            const start = reason_start + "blocked (".len;
            const end = std.mem.lastIndexOfScalar(u8, detail, ')') orelse detail.len;
            try record.appendSlice(allocator, ",\"reason\":");
            try appendJsonString(&record, allocator, detail[start..end]);
        }
        try record.append(allocator, '}');
    }
    try record.appendSlice(allocator, "]}");
    var hash_input = try allocator.dupe(u8, record.items);
    const hash_pos = std.mem.indexOf(u8, hash_input, "\"record_hash\":\"\"") orelse return error.InvalidLedger;
    _ = hash_pos;
    var digest: [32]u8 = undefined;
    Sha256.hash(hash_input, &digest, .{});
    const record_hash = std.fmt.bytesToHex(digest, .lower);
    const replacement = try std.fmt.allocPrint(allocator, "\"record_hash\":\"{s}\"", .{&record_hash});
    const marker = "\"record_hash\":\"\"";
    const marker_pos = std.mem.indexOf(u8, hash_input, marker).?;
    var final = std.ArrayList(u8).empty;
    try final.appendSlice(allocator, hash_input[0..marker_pos]);
    try final.appendSlice(allocator, replacement);
    try final.appendSlice(allocator, hash_input[marker_pos + marker.len ..]);
    try final.append(allocator, '\n');
    var output = std.ArrayList(u8).empty;
    if (old.len != 0) {
        try output.appendSlice(allocator, old);
        if (old[old.len - 1] != '\n') try output.append(allocator, '\n');
    }
    try output.appendSlice(allocator, final.items);
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
    try std.testing.expect(try verifyEvidence(arena.allocator(), ".", tmp.dir, item));
    try std.testing.expectError(VerifyError.HashMismatch, verifyEvidence(arena.allocator(), ".", tmp.dir, .{ .evidence_id = "e1", .kind = "source", .path = "source.txt", .commit = "h", .line_start = 2, .line_end = 2, .source = "test", .content_hash = &([_]u8{'0'} ** 64), .collected_at = "now", .verifier_status = "verified" }));
}

test "evidence verification rejects unsafe paths and invalid ranges" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(std.Options.debug_io, .{ .sub_path = "source.txt", .data = "one\n" });
    const base = contract.Evidence{ .evidence_id = "e1", .kind = "source", .path = "source.txt", .commit = "h", .line_start = 1, .line_end = 1, .source = "test", .content_hash = "", .collected_at = "now", .verifier_status = "verified" };
    try std.testing.expectError(VerifyError.InvalidPath, verifyEvidence(arena.allocator(), ".", tmp.dir, .{ .evidence_id = base.evidence_id, .kind = base.kind, .path = "../source.txt", .commit = base.commit, .line_start = base.line_start, .line_end = base.line_end, .source = base.source, .content_hash = base.content_hash, .collected_at = base.collected_at, .verifier_status = base.verifier_status }));
    try std.testing.expectError(VerifyError.BadRange, verifyEvidence(arena.allocator(), ".", tmp.dir, .{ .evidence_id = base.evidence_id, .kind = base.kind, .path = base.path, .commit = base.commit, .line_start = 3, .line_end = 3, .source = base.source, .content_hash = base.content_hash, .collected_at = base.collected_at, .verifier_status = base.verifier_status }));
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
