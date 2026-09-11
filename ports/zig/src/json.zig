const std = @import("std");

pub const Error = error{
    OutOfMemory,
    InvalidJson,
    InvalidUtf8,
    DuplicateKey,
    TrailingData,
    LimitExceeded,
};

pub const Field = struct {
    key: []const u8,
    value: Value,
};

pub const Value = union(enum) {
    null,
    boolean: bool,
    number: []const u8,
    string: []const u8,
    array: []Value,
    object: []Field,

    pub fn objectField(self: Value, key: []const u8) ?Value {
        if (self != .object) return null;
        for (self.object) |field| {
            if (std.mem.eql(u8, field.key, key)) return field.value;
        }
        return null;
    }
};

pub const Parser = struct {
    allocator: std.mem.Allocator,
    data: []const u8,
    index: usize = 0,

    pub fn parse(allocator: std.mem.Allocator, data: []const u8) Error!Value {
        if (data.len > (1 << 20)) return Error.LimitExceeded;
        if (!std.unicode.utf8ValidateSlice(data)) return Error.InvalidUtf8;
        var parser = Parser{ .allocator = allocator, .data = data };
        const parsed = try parser.value();
        parser.space();
        if (parser.index != data.len) return Error.TrailingData;
        return parsed;
    }

    fn value(self: *Parser) Error!Value {
        self.space();
        if (self.index >= self.data.len) return Error.InvalidJson;
        return switch (self.data[self.index]) {
            '{' => self.object(),
            '[' => self.array(),
            '"' => Value{ .string = try self.string() },
            't' => blk: {
                try self.literal("true");
                break :blk Value{ .boolean = true };
            },
            'f' => blk: {
                try self.literal("false");
                break :blk Value{ .boolean = false };
            },
            'n' => blk: {
                try self.literal("null");
                break :blk Value.null;
            },
            '-', '0'...'9' => Value{ .number = try self.number() },
            else => Error.InvalidJson,
        };
    }

    fn object(self: *Parser) Error!Value {
        self.index += 1;
        var fields: std.ArrayList(Field) = .empty;
        self.space();
        if (self.take('}')) return Value{ .object = try fields.toOwnedSlice(self.allocator) };
        while (true) {
            self.space();
            if (self.index >= self.data.len or self.data[self.index] != '"') return Error.InvalidJson;
            const key = try self.string();
            self.space();
            if (!self.take(':')) return Error.InvalidJson;
            const child = try self.value();
            for (fields.items) |field| {
                if (std.mem.eql(u8, field.key, key)) return Error.DuplicateKey;
            }
            try fields.append(self.allocator, .{ .key = key, .value = child });
            self.space();
            if (self.take('}')) break;
            if (!self.take(',')) return Error.InvalidJson;
        }
        return Value{ .object = try fields.toOwnedSlice(self.allocator) };
    }

    fn array(self: *Parser) Error!Value {
        self.index += 1;
        var items: std.ArrayList(Value) = .empty;
        self.space();
        if (self.take(']')) return Value{ .array = try items.toOwnedSlice(self.allocator) };
        while (true) {
            try items.append(self.allocator, try self.value());
            self.space();
            if (self.take(']')) break;
            if (!self.take(',')) return Error.InvalidJson;
        }
        return Value{ .array = try items.toOwnedSlice(self.allocator) };
    }

    fn string(self: *Parser) Error![]const u8 {
        if (!self.take('"')) return Error.InvalidJson;
        var result: std.ArrayList(u8) = .empty;
        while (self.index < self.data.len) {
            const c = self.data[self.index];
            self.index += 1;
            if (c == '"') return try result.toOwnedSlice(self.allocator);
            if (c < 0x20) return Error.InvalidJson;
            if (c != '\\') {
                try result.append(self.allocator, c);
                continue;
            }
            if (self.index >= self.data.len) return Error.InvalidJson;
            const escaped = self.data[self.index];
            self.index += 1;
            switch (escaped) {
                '"', '\\', '/' => try result.append(self.allocator, escaped),
                'b' => try result.append(self.allocator, 8),
                'f' => try result.append(self.allocator, 12),
                'n' => try result.append(self.allocator, '\n'),
                'r' => try result.append(self.allocator, '\r'),
                't' => try result.append(self.allocator, '\t'),
                'u' => {
                    const first = try self.hex4();
                    var codepoint: u21 = first;
                    if (first >= 0xd800 and first <= 0xdbff) {
                        if (self.index + 6 > self.data.len or self.data[self.index] != '\\' or self.data[self.index + 1] != 'u') return Error.InvalidJson;
                        self.index += 2;
                        const low = try self.hex4();
                        if (low < 0xdc00 or low > 0xdfff) return Error.InvalidJson;
                        codepoint = 0x10000 + (@as(u21, first) - 0xd800) * 0x400 + (low - 0xdc00);
                    } else if (first >= 0xdc00 and first <= 0xdfff) return Error.InvalidJson;
                    var encoded: [4]u8 = undefined;
                    const len = std.unicode.utf8Encode(codepoint, &encoded) catch return Error.InvalidJson;
                    try result.appendSlice(self.allocator, encoded[0..len]);
                },
                else => return Error.InvalidJson,
            }
        }
        return Error.InvalidJson;
    }

    fn hex4(self: *Parser) Error!u16 {
        if (self.index + 4 > self.data.len) return Error.InvalidJson;
        var result: u16 = 0;
        for (self.data[self.index .. self.index + 4]) |c| {
            result <<= 4;
            result |= switch (c) {
                '0'...'9' => c - '0',
                'a'...'f' => c - 'a' + 10,
                'A'...'F' => c - 'A' + 10,
                else => return Error.InvalidJson,
            };
        }
        self.index += 4;
        return result;
    }

    fn number(self: *Parser) Error![]const u8 {
        const start = self.index;
        if (self.data[self.index] == '-') self.index += 1;
        if (self.index >= self.data.len) return Error.InvalidJson;
        if (self.data[self.index] == '0') {
            self.index += 1;
        } else {
            if (self.data[self.index] < '1' or self.data[self.index] > '9') return Error.InvalidJson;
            while (self.index < self.data.len and self.data[self.index] >= '0' and self.data[self.index] <= '9') self.index += 1;
        }
        if (self.index < self.data.len and self.data[self.index] == '.') {
            self.index += 1;
            const begin = self.index;
            while (self.index < self.data.len and self.data[self.index] >= '0' and self.data[self.index] <= '9') self.index += 1;
            if (begin == self.index) return Error.InvalidJson;
        }
        if (self.index < self.data.len and (self.data[self.index] == 'e' or self.data[self.index] == 'E')) {
            self.index += 1;
            if (self.index < self.data.len and (self.data[self.index] == '+' or self.data[self.index] == '-')) self.index += 1;
            const begin = self.index;
            while (self.index < self.data.len and self.data[self.index] >= '0' and self.data[self.index] <= '9') self.index += 1;
            if (begin == self.index) return Error.InvalidJson;
        }
        return self.data[start..self.index];
    }

    fn literal(self: *Parser, literal_text: []const u8) Error!void {
        if (self.index + literal_text.len > self.data.len or !std.mem.eql(u8, self.data[self.index .. self.index + literal_text.len], literal_text)) return Error.InvalidJson;
        self.index += literal_text.len;
    }

    fn space(self: *Parser) void {
        while (self.index < self.data.len) {
            switch (self.data[self.index]) {
                ' ', '\n', '\r', '\t' => self.index += 1,
                else => return,
            }
        }
    }

    fn take(self: *Parser, c: u8) bool {
        if (self.index < self.data.len and self.data[self.index] == c) {
            self.index += 1;
            return true;
        }
        return false;
    }
};

test "parser rejects duplicate keys and trailing data" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    try std.testing.expectError(Error.DuplicateKey, Parser.parse(arena.allocator(), "{\"a\":1,\"a\":2}"));
    try std.testing.expectError(Error.TrailingData, Parser.parse(arena.allocator(), "true false"));
}

test "parser decodes unicode escapes and rejects malformed escapes" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    const value = try Parser.parse(arena.allocator(), "{\"message\":\"\\uD83D\\uDE00\"}");
    try std.testing.expectEqualStrings("😀", value.objectField("message").?.string);
    try std.testing.expectError(Error.InvalidJson, Parser.parse(arena.allocator(), "\"\\uD800\""));
}

test "parser enforces JSON number boundaries" {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    try std.testing.expectError(Error.TrailingData, Parser.parse(arena.allocator(), "01"));
    try std.testing.expectError(Error.InvalidJson, Parser.parse(arena.allocator(), "1."));
    try std.testing.expectError(Error.InvalidJson, Parser.parse(arena.allocator(), "1e"));
    const value = try Parser.parse(arena.allocator(), "-9223372036854775808");
    try std.testing.expectEqualStrings("-9223372036854775808", value.number);
}
