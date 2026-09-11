const std = @import("std");
const json = @import("json.zig");
const cli = @import("cli.zig");

pub fn main(init: std.process.Init) !void {
    var arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena.deinit();
    _ = json.Parser.parse(arena.allocator(), "{}") catch {};
    var iterator = std.process.Args.Iterator.init(init.minimal.args);
    var args: std.ArrayList([]const u8) = .empty;
    _ = iterator.next();
    while (iterator.next()) |arg| try args.append(arena.allocator(), arg[0..arg.len]);
    const run = try cli.run(arena.allocator(), args.items);
    try std.Io.File.stdout().writeStreamingAll(std.Options.debug_io, run.output);
    if (run.code != 0) std.process.exit(run.code);
}

test {
    _ = @import("json.zig");
    _ = @import("contract.zig");
    _ = @import("cli.zig");
}
