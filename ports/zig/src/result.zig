const std = @import("std");

pub const Data = struct {
    command: []const u8,
    ok: bool,
    summary: []const u8,
    details: []const []const u8,
    next_action: []const u8,
};

fn appendString(list: *std.ArrayList(u8), allocator: std.mem.Allocator, value: []const u8) !void {
    try list.append(allocator, '"');
    for (value) |c| {
        switch (c) {
            '"' => try list.appendSlice(allocator, "\\\""),
            '\\' => try list.appendSlice(allocator, "\\\\"),
            '\n' => try list.appendSlice(allocator, "\\n"),
            '\r' => try list.appendSlice(allocator, "\\r"),
            '\t' => try list.appendSlice(allocator, "\\t"),
            '<' => try list.appendSlice(allocator, "\\u003c"),
            '>' => try list.appendSlice(allocator, "\\u003e"),
            '&' => try list.appendSlice(allocator, "\\u0026"),
            else => if (c < 0x20) {
                try list.appendSlice(allocator, "\\u00");
                const digits = "0123456789abcdef";
                try list.append(allocator, digits[c >> 4]);
                try list.append(allocator, digits[c & 15]);
            } else try list.append(allocator, c),
        }
    }
    try list.append(allocator, '"');
}

pub fn render(allocator: std.mem.Allocator, data: Data, json_output: bool) ![]u8 {
    var out: std.ArrayList(u8) = .empty;
    if (json_output) {
        try out.append(allocator, '{');
        try out.appendSlice(allocator, "\"command\":");
        try appendString(&out, allocator, data.command);
        try out.appendSlice(allocator, ",\"ok\":");
        try out.appendSlice(allocator, if (data.ok) "true" else "false");
        try out.appendSlice(allocator, ",\"summary\":");
        try appendString(&out, allocator, data.summary);
        try out.appendSlice(allocator, ",\"details\":[");
        for (data.details, 0..) |detail, i| {
            if (i != 0) try out.append(allocator, ',');
            try appendString(&out, allocator, detail);
        }
        try out.appendSlice(allocator, "],\"next_action\":");
        try appendString(&out, allocator, data.next_action);
        try out.appendSlice(allocator, "}\n");
    } else {
        try out.appendSlice(allocator, if (data.ok) "OK: " else "ERROR: ");
        try out.appendSlice(allocator, data.summary);
        try out.append(allocator, '\n');
        for (data.details) |detail| {
            try out.appendSlice(allocator, "  - ");
            try out.appendSlice(allocator, detail);
            try out.append(allocator, '\n');
        }
        try out.appendSlice(allocator, "Next action: ");
        try out.appendSlice(allocator, data.next_action);
        try out.append(allocator, '\n');
    }
    return try out.toOwnedSlice(allocator);
}

pub fn usage(allocator: std.mem.Allocator, message: []const u8, next: []const u8, json_output: bool) ![]u8 {
    if (json_output) {
        var detail: std.ArrayList(u8) = .empty;
        try detail.appendSlice(allocator, "usage: ");
        try detail.appendSlice(allocator, next);
        const detail_slice = try detail.toOwnedSlice(allocator);
        const details = [_][]const u8{detail_slice};
        var action_text: std.ArrayList(u8) = .empty;
        try action_text.appendSlice(allocator, "Run '");
        try action_text.appendSlice(allocator, next);
        try action_text.appendSlice(allocator, "'.");
        return render(allocator, .{ .command = "usage", .ok = false, .summary = message, .details = &details, .next_action = try action_text.toOwnedSlice(allocator) }, true);
    }
    var out: std.ArrayList(u8) = .empty;
    try out.appendSlice(allocator, "error: ");
    try out.appendSlice(allocator, message);
    try out.appendSlice(allocator, "\nNext action: run '");
    try out.appendSlice(allocator, next);
    try out.appendSlice(allocator, "'.\n");
    return try out.toOwnedSlice(allocator);
}
