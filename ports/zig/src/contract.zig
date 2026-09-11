const std = @import("std");
const json = @import("json.zig");

pub const Packet = struct {
    packet_id: []const u8,
    schema_version: []const u8,
    repo_root: []const u8,
    head_commit: []const u8,
    request_id: []const u8,
    issued_at: []const u8,
    outcome: []const u8,
    freshness: Freshness,
    authorization: Authorization,
    budget: Budget,
    evidence: []Evidence,
    degradations: [][]const u8,
    provenance: Provenance,
    packet_hash: []const u8,
    freshness_present: bool,
    authorization_present: bool,
    budget_present: bool,
    evidence_present: bool,
    degradations_present: bool,
    provenance_present: bool,
};
pub const Freshness = struct { head_commit: []const u8, head_anchor: []const u8, status: []const u8, current: bool, is_current: bool, checked_at: []const u8 };
pub const Authorization = struct { level: []const u8, reason: []const u8 };
pub const Budget = struct { max_evidence: i64, used_evidence: i64, max_bytes: i64, used_bytes: i64 };
pub const Evidence = struct { evidence_id: []const u8, kind: []const u8, path: []const u8, commit: []const u8, line_start: i64, line_end: i64, source: []const u8, content_hash: []const u8, collected_at: []const u8, verifier_status: []const u8 };
pub const Provenance = struct { collector: []const u8, tool: []const u8, version: []const u8, tool_version: []const u8 };
pub const Violation = struct { rule: []const u8, field: []const u8, message: []const u8 };

fn emptyFreshness(f: Freshness) bool {
    return f.head_commit.len == 0 and f.head_anchor.len == 0 and f.status.len == 0 and
        !f.current and !f.is_current and f.checked_at.len == 0;
}
fn emptyBudget(b: Budget) bool {
    return b.max_evidence == 0 and b.used_evidence == 0 and b.max_bytes == 0 and b.used_bytes == 0;
}
fn emptyProvenance(p: Provenance) bool {
    return p.collector.len == 0 and p.tool.len == 0 and p.version.len == 0 and p.tool_version.len == 0;
}
fn addViolation(list: *std.ArrayList(Violation), allocator: std.mem.Allocator, rule: []const u8, field_name: []const u8, message: []const u8) !void {
    try list.append(allocator, .{ .rule = rule, .field = field_name, .message = message });
}

pub fn validate(allocator: std.mem.Allocator, p: Packet) ![]Violation {
    var out: std.ArrayList(Violation) = .empty;
    const required = [_][2][]const u8{
        .{ "packet_id", p.packet_id }, .{ "schema_version", p.schema_version }, .{ "repo_root", p.repo_root },
        .{ "head_commit", p.head_commit }, .{ "request_id", p.request_id }, .{ "issued_at", p.issued_at },
        .{ "outcome", p.outcome }, .{ "packet_hash", p.packet_hash },
    };
    for (required) |r| if (r[1].len == 0) try addViolation(&out, allocator, "required_field", r[0], "required field is missing");
    if (!p.freshness_present) try addViolation(&out, allocator, "required_field", "freshness", "required field is missing");
    if (!p.authorization_present) try addViolation(&out, allocator, "required_field", "authorization", "required field is missing");
    if (!p.budget_present) try addViolation(&out, allocator, "required_field", "budget", "required field is missing");
    if (!p.evidence_present) try addViolation(&out, allocator, "required_field", "evidence", "required field is missing");
    if (!p.degradations_present) try addViolation(&out, allocator, "required_field", "degradations", "required field is missing");
    if (!p.provenance_present) try addViolation(&out, allocator, "required_field", "provenance", "required field is missing");
    if (!std.mem.eql(u8, p.schema_version, "v1")) try addViolation(&out, allocator, "schema_version", "schema_version", "must be v1");
    if (action(p.outcome) == null) try addViolation(&out, allocator, "outcome", "outcome", "unknown outcome");
    if (p.freshness.head_commit.len > 0 and p.head_commit.len > 0 and !std.mem.eql(u8, p.freshness.head_commit, p.head_commit))
        try addViolation(&out, allocator, "freshness", "freshness.head_commit", "freshness head anchor does not match packet head_commit");
    if (p.freshness.head_commit.len > 0 and p.freshness.head_anchor.len > 0 and !std.mem.eql(u8, p.freshness.head_commit, p.freshness.head_anchor))
        try addViolation(&out, allocator, "freshness", "freshness.head_anchor", "freshness head anchors disagree");
    const anchor = if (p.freshness.head_commit.len > 0) p.freshness.head_commit else p.freshness.head_anchor;
    if (std.mem.eql(u8, p.outcome, "complete")) {
        if (anchor.len == 0) try addViolation(&out, allocator, "freshness", "freshness.head_commit", "complete packet requires a freshness head anchor");
        if (!p.freshness.current and !p.freshness.is_current and !std.mem.eql(u8, p.freshness.status, "current"))
            try addViolation(&out, allocator, "freshness", "freshness.status", "complete packet requires current freshness");
    }
    if (!p.budget_present or emptyBudget(p.budget)) {
        try addViolation(&out, allocator, "budget", "budget", "budget must contain counters");
    } else {
        if (p.budget.max_evidence < 0) try addViolation(&out, allocator, "budget", "budget.max_evidence", "budget counter cannot be negative");
        if (p.budget.used_evidence < 0) try addViolation(&out, allocator, "budget", "budget.used_evidence", "budget counter cannot be negative");
        if (p.budget.max_bytes < 0) try addViolation(&out, allocator, "budget", "budget.max_bytes", "budget counter cannot be negative");
        if (p.budget.used_bytes < 0) try addViolation(&out, allocator, "budget", "budget.used_bytes", "budget counter cannot be negative");
        if (p.budget.used_evidence > p.budget.max_evidence) try addViolation(&out, allocator, "budget", "budget.used_evidence", "used evidence exceeds max_evidence");
        if (p.budget.used_bytes > p.budget.max_bytes) try addViolation(&out, allocator, "budget", "budget.used_bytes", "used bytes exceeds max_bytes");
    }
    for (p.degradations, 0..) |d, i| {
        if (d.len == 0) {
            const f = try std.fmt.allocPrint(allocator, "degradations[{d}]", .{i});
            try addViolation(&out, allocator, "degradations", f, "degradation must not be empty");
        }
    }
    if (std.mem.eql(u8, p.outcome, "complete") and p.degradations.len != 0)
        try addViolation(&out, allocator, "degradations", "degradations", "complete packet cannot contain degradations");
    for (p.evidence, 0..) |e, i| {
        const prefix = try std.fmt.allocPrint(allocator, "evidence[{d}]", .{i});
        const fields = [_][2][]const u8{
            .{ "evidence_id", e.evidence_id }, .{ "kind", e.kind }, .{ "path", e.path }, .{ "commit", e.commit },
            .{ "source", e.source }, .{ "content_hash", e.content_hash }, .{ "collected_at", e.collected_at }, .{ "verifier_status", e.verifier_status },
        };
        for (fields) |f| if (f[1].len == 0) {
            const name = try std.fmt.allocPrint(allocator, "{s}.{s}", .{ prefix, f[0] });
            try addViolation(&out, allocator, "malformed_evidence", name, "required evidence field is missing");
        };
        if (e.line_start < 1) {
            const name = try std.fmt.allocPrint(allocator, "{s}.line_start", .{prefix});
            try addViolation(&out, allocator, "malformed_evidence", name, "line_start must be at least 1");
        }
        if (e.line_end < 1) {
            const name = try std.fmt.allocPrint(allocator, "{s}.line_end", .{prefix});
            try addViolation(&out, allocator, "malformed_evidence", name, "line_end must be at least 1");
        }
        if (e.line_start >= 1 and e.line_end >= 1 and e.line_end < e.line_start)
            try addViolation(&out, allocator, "malformed_evidence", prefix, "line_end must be greater than or equal to line_start");
        if (!std.mem.eql(u8, e.verifier_status, "verified") and !std.mem.eql(u8, e.verifier_status, "unverified") and
            !std.mem.eql(u8, e.verifier_status, "failed") and !std.mem.eql(u8, e.verifier_status, "unavailable") and !std.mem.eql(u8, e.verifier_status, "pending")) {
            const name = try std.fmt.allocPrint(allocator, "{s}.verifier_status", .{prefix});
            try addViolation(&out, allocator, "malformed_evidence", name, "unknown verifier status");
        }
        if (std.mem.eql(u8, p.outcome, "complete") and !std.mem.eql(u8, e.verifier_status, "verified")) {
            const name = try std.fmt.allocPrint(allocator, "{s}.verifier_status", .{prefix});
            try addViolation(&out, allocator, "evidence", name, "complete packet requires verified evidence");
        }
    }
    if (std.mem.eql(u8, p.outcome, "complete") and p.evidence.len == 0)
        try addViolation(&out, allocator, "evidence", "evidence", "complete packet requires at least one evidence entry");
    if ((std.mem.eql(u8, p.authorization.level, "autonomous") or std.mem.eql(u8, p.authorization.level, "autonomous_proceed") or std.mem.eql(u8, p.authorization.level, "autonomous-proceed")) and
        (std.mem.eql(u8, p.outcome, "failed_verification") or std.mem.eql(u8, p.outcome, "blocked") or std.mem.eql(u8, p.outcome, "unavailable") or std.mem.eql(u8, p.outcome, "budget_exhausted")))
        try addViolation(&out, allocator, "authorization", "authorization.level", "autonomous authorization is forbidden for this outcome");
    return try out.toOwnedSlice(allocator);
}

fn field(obj: json.Value, name: []const u8) ?json.Value { return obj.objectField(name); }
fn text(obj: json.Value, name: []const u8) ![]const u8 {
    const v = field(obj, name) orelse return "";
    if (v == .null) return "";
    if (v != .string) return error.WrongType;
    return v.string;
}
fn optionalText(obj: json.Value, name: []const u8) ![]const u8 {
    const v = field(obj, name) orelse return "";
    if (v == .null) return "";
    if (v != .string) return error.WrongType;
    return v.string;
}
fn boolean(obj: json.Value, name: []const u8) !bool {
    const v = field(obj, name) orelse return false;
    if (v == .null) return false;
    if (v != .boolean) return error.WrongType;
    return v.boolean;
}
fn integer(obj: json.Value, name: []const u8) !i64 {
    const v = field(obj, name) orelse return 0;
    if (v == .null) return 0;
    if (v != .number) return error.WrongType;
    return std.fmt.parseInt(i64, v.number, 10) catch return error.WrongType;
}
fn object(obj: json.Value, name: []const u8) !json.Value {
    const v = field(obj, name) orelse return json.Value.null;
    if (v == .null) return .{ .object = &.{} };
    if (v != .object) return error.WrongType;
    return v;
}
fn hasText(obj: json.Value, name: []const u8) bool {
    const v = field(obj, name) orelse return false;
    return v == .string and v.string.len != 0;
}

pub fn decodePacket(allocator: std.mem.Allocator, root: json.Value) !Packet {
    if (root != .object) return error.WrongType;
    const f = object(root, "freshness") catch |err| return err;
    const a = object(root, "authorization") catch |err| return err;
    const b = object(root, "budget") catch |err| return err;
    const p = object(root, "provenance") catch |err| return err;
    const ev_field = field(root, "evidence");
    const dg_field = field(root, "degradations");
    const ev = if (ev_field == null or ev_field.? == .null) json.Value{ .array = &.{} } else ev_field.?;
    const dg = if (dg_field == null or dg_field.? == .null) json.Value{ .array = &.{} } else dg_field.?;
    if (ev != .array or dg != .array) return error.WrongType;
    var evidence: []Evidence = if (ev_field == null or ev_field.? == .null) &.{} else try allocator.alloc(Evidence, ev.array.len);
    for (ev.array, 0..) |item, i| {
        const item_object = if (item == .null) json.Value{ .object = &.{} } else item;
        if (item_object != .object) return error.WrongType;
        evidence[i] = .{
            .evidence_id = try text(item_object, "evidence_id"), .kind = try text(item_object, "kind"), .path = try text(item_object, "path"),
            .commit = try text(item_object, "commit"), .line_start = try integer(item_object, "line_start"), .line_end = try integer(item_object, "line_end"),
            .source = try text(item_object, "source"), .content_hash = try text(item_object, "content_hash"), .collected_at = try text(item_object, "collected_at"),
            .verifier_status = try text(item_object, "verifier_status"),
        };
    }
    var degradations: [][]const u8 = if (dg_field == null or dg_field.? == .null) &.{} else try allocator.alloc([]const u8, dg.array.len);
    for (dg.array, 0..) |item, i| {
        if (item == .null) {
            degradations[i] = "";
            continue;
        }
        if (item != .string) return error.WrongType;
        degradations[i] = item.string;
    }
    const freshness = Freshness{
        .head_commit = try optionalText(f, "head_commit"), .head_anchor = try optionalText(f, "head_anchor"), .status = try optionalText(f, "status"),
        .current = try boolean(f, "current"), .is_current = try boolean(f, "is_current"), .checked_at = try optionalText(f, "checked_at"),
    };
    const budget = Budget{ .max_evidence = try integer(b, "max_evidence"), .used_evidence = try integer(b, "used_evidence"), .max_bytes = try integer(b, "max_bytes"), .used_bytes = try integer(b, "used_bytes") };
    return .{
        .packet_id = try text(root, "packet_id"), .schema_version = try text(root, "schema_version"), .repo_root = try text(root, "repo_root"),
        .head_commit = try text(root, "head_commit"), .request_id = try text(root, "request_id"), .issued_at = try text(root, "issued_at"),
        .outcome = try text(root, "outcome"), .freshness = freshness,
        .authorization = .{ .level = try text(a, "level"), .reason = try optionalText(a, "reason") },
        .budget = budget,
        .evidence = evidence, .degradations = degradations, .provenance = .{ .collector = try optionalText(p, "collector"), .tool = try optionalText(p, "tool"), .version = try optionalText(p, "version"), .tool_version = try optionalText(p, "tool_version") },
        .packet_hash = try text(root, "packet_hash"),
        // The reference keys required-field presence off emptiness for structs and
        // nil-ness for slices, so an explicit null is absent but [] is present
        // (internal/contract/contract.go:217-231).
        .freshness_present = !emptyFreshness(freshness),
        .authorization_present = hasText(a, "level"),
        .budget_present = !emptyBudget(budget),
        .evidence_present = !(ev_field == null or ev_field.? == .null),
        .degradations_present = !(dg_field == null or dg_field.? == .null),
        .provenance_present = hasText(p, "collector") or hasText(p, "tool") or hasText(p, "version") or hasText(p, "tool_version"),
    };
}

pub fn action(outcome: []const u8) ?[]const u8 {
    const pairs = [_][2][]const u8{
        .{ "complete", "autonomous_proceed" }, .{ "partial", "bounded_more_evidence" }, .{ "partial_degraded", "autonomous_proceed" },
        .{ "no_match", "autonomous_proceed" }, .{ "stale", "bounded_refresh" }, .{ "unavailable", "blocked" }, .{ "blocked", "blocked" },
        .{ "needs_more_evidence", "bounded_more_evidence" }, .{ "failed_verification", "quarantine" }, .{ "budget_exhausted", "human_approval" },
    };
    for (pairs) |pair| if (std.mem.eql(u8, outcome, pair[0])) return pair[1];
    return null;
}

test "default actions cover every supported outcome" {
    const cases = [_][2][]const u8{
        .{ "complete", "autonomous_proceed" },
        .{ "partial", "bounded_more_evidence" },
        .{ "stale", "bounded_refresh" },
        .{ "unavailable", "blocked" },
        .{ "failed_verification", "quarantine" },
        .{ "budget_exhausted", "human_approval" },
    };
    for (cases) |item| try std.testing.expectEqualStrings(item[1], action(item[0]).?);
    try std.testing.expect(action("unknown") == null);
}

test "complete packets require current evidence and freshness" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const packet = Packet{
        .packet_id = "p", .schema_version = "v1", .repo_root = ".", .head_commit = "h",
        .request_id = "r", .issued_at = "now", .outcome = "complete",
        .freshness = .{ .head_commit = "", .head_anchor = "", .status = "", .current = false, .is_current = false, .checked_at = "" },
        .authorization = .{ .level = "bounded", .reason = "" },
        .budget = .{ .max_evidence = 1, .used_evidence = 0, .max_bytes = 1, .used_bytes = 0 },
        .evidence = &.{}, .degradations = &.{}, .provenance = .{ .collector = "", .tool = "", .version = "", .tool_version = "" },
        .packet_hash = "hash", .freshness_present = true, .authorization_present = true,
        .budget_present = true, .evidence_present = true, .degradations_present = true,
        .provenance_present = true,
    };
    const violations = try validate(arena.allocator(), packet);
    try std.testing.expect(violations.len >= 2);
}

test "validation rejects negative and over-budget counters" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const packet = Packet{
        .packet_id = "p", .schema_version = "v1", .repo_root = ".", .head_commit = "h",
        .request_id = "r", .issued_at = "now", .outcome = "partial",
        .freshness = .{ .head_commit = "", .head_anchor = "", .status = "", .current = false, .is_current = false, .checked_at = "" },
        .authorization = .{ .level = "bounded", .reason = "" },
        .budget = .{ .max_evidence = 1, .used_evidence = 2, .max_bytes = -1, .used_bytes = 0 },
        .evidence = &.{}, .degradations = &.{}, .provenance = .{ .collector = "", .tool = "", .version = "", .tool_version = "" },
        .packet_hash = "hash", .freshness_present = true, .authorization_present = true,
        .budget_present = true, .evidence_present = true, .degradations_present = true,
        .provenance_present = true,
    };
    const violations = try validate(arena.allocator(), packet);
    try std.testing.expect(violations.len >= 2);
}

test "decoder rejects wrong nested types" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const root = try json.Parser.parse(arena.allocator(),
        "{\"freshness\":[],\"authorization\":{},\"budget\":{},\"provenance\":{},\"evidence\":[],\"degradations\":[]}");
    try std.testing.expectError(error.WrongType, decodePacket(arena.allocator(), root));
}
