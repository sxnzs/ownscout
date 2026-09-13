//! Go-compatible Unicode simple case folding.
//!
//! Transcribed from the Go standard library (unicode.SimpleFold, its asciiFold
//! / caseOrbit / CaseRanges tables) because encoding/json's foldName is
//! defined in terms of it: ASCII letters fold to uppercase and every other
//! rune folds to the minimum of its SimpleFold orbit. Two names match when
//! their folded forms are equal (equivalent to Go's bytes.EqualFold).

const std = @import("std");

const CaseRange = struct { lo: u21, hi: u21, upper: i32, lower: i32, title: i32 };
const FoldPair = struct { from: u21, to: u21 };

const max_rune: i32 = 0x10FFFF;

// unicode.MaxASCII + 1 entries: SimpleFold for r < 128.
const ascii_fold = [_]u16{
    0x0000, 0x0001, 0x0002, 0x0003, 0x0004, 0x0005, 0x0006, 0x0007, 0x0008, 0x0009, 0x000a, 0x000b, 0x000c, 0x000d,
    0x000e, 0x000f, 0x0010, 0x0011, 0x0012, 0x0013, 0x0014, 0x0015, 0x0016, 0x0017, 0x0018, 0x0019, 0x001a, 0x001b,
    0x001c, 0x001d, 0x001e, 0x001f, 0x0020, 0x0021, 0x0022, 0x0023, 0x0024, 0x0025, 0x0026, 0x0027, 0x0028, 0x0029,
    0x002a, 0x002b, 0x002c, 0x002d, 0x002e, 0x002f, 0x0030, 0x0031, 0x0032, 0x0033, 0x0034, 0x0035, 0x0036, 0x0037,
    0x0038, 0x0039, 0x003a, 0x003b, 0x003c, 0x003d, 0x003e, 0x003f, 0x0040, 0x0061, 0x0062, 0x0063, 0x0064, 0x0065,
    0x0066, 0x0067, 0x0068, 0x0069, 0x006a, 0x006b, 0x006c, 0x006d, 0x006e, 0x006f, 0x0070, 0x0071, 0x0072, 0x0073,
    0x0074, 0x0075, 0x0076, 0x0077, 0x0078, 0x0079, 0x007a, 0x005b, 0x005c, 0x005d, 0x005e, 0x005f, 0x0060, 0x0041,
    0x0042, 0x0043, 0x0044, 0x0045, 0x0046, 0x0047, 0x0048, 0x0049, 0x004a, 0x212a, 0x004c, 0x004d, 0x004e, 0x004f,
    0x0050, 0x0051, 0x0052, 0x017f, 0x0054, 0x0055, 0x0056, 0x0057, 0x0058, 0x0059, 0x005a, 0x007b, 0x007c, 0x007d,
    0x007e, 0x007f,
};

// Special-case fold pairs, sorted by `from` (unicode caseOrbit).
const case_orbit = [_]FoldPair{
    .{ .from = 0x004b, .to = 0x006b }, .{ .from = 0x0053, .to = 0x0073 }, .{ .from = 0x006b, .to = 0x212a },
    .{ .from = 0x0073, .to = 0x017f }, .{ .from = 0x00b5, .to = 0x039c }, .{ .from = 0x00c5, .to = 0x00e5 },
    .{ .from = 0x00df, .to = 0x1e9e }, .{ .from = 0x00e5, .to = 0x212b }, .{ .from = 0x0130, .to = 0x0130 },
    .{ .from = 0x0131, .to = 0x0131 }, .{ .from = 0x017f, .to = 0x0053 }, .{ .from = 0x01c4, .to = 0x01c5 },
    .{ .from = 0x01c5, .to = 0x01c6 }, .{ .from = 0x01c6, .to = 0x01c4 }, .{ .from = 0x01c7, .to = 0x01c8 },
    .{ .from = 0x01c8, .to = 0x01c9 }, .{ .from = 0x01c9, .to = 0x01c7 }, .{ .from = 0x01ca, .to = 0x01cb },
    .{ .from = 0x01cb, .to = 0x01cc }, .{ .from = 0x01cc, .to = 0x01ca }, .{ .from = 0x01f1, .to = 0x01f2 },
    .{ .from = 0x01f2, .to = 0x01f3 }, .{ .from = 0x01f3, .to = 0x01f1 }, .{ .from = 0x0345, .to = 0x0399 },
    .{ .from = 0x0390, .to = 0x1fd3 }, .{ .from = 0x0392, .to = 0x03b2 }, .{ .from = 0x0395, .to = 0x03b5 },
    .{ .from = 0x0398, .to = 0x03b8 }, .{ .from = 0x0399, .to = 0x03b9 }, .{ .from = 0x039a, .to = 0x03ba },
    .{ .from = 0x039c, .to = 0x03bc }, .{ .from = 0x03a0, .to = 0x03c0 }, .{ .from = 0x03a1, .to = 0x03c1 },
    .{ .from = 0x03a3, .to = 0x03c2 }, .{ .from = 0x03a6, .to = 0x03c6 }, .{ .from = 0x03a9, .to = 0x03c9 },
    .{ .from = 0x03b0, .to = 0x1fe3 }, .{ .from = 0x03b2, .to = 0x03d0 }, .{ .from = 0x03b5, .to = 0x03f5 },
    .{ .from = 0x03b8, .to = 0x03d1 }, .{ .from = 0x03b9, .to = 0x1fbe }, .{ .from = 0x03ba, .to = 0x03f0 },
    .{ .from = 0x03bc, .to = 0x00b5 }, .{ .from = 0x03c0, .to = 0x03d6 }, .{ .from = 0x03c1, .to = 0x03f1 },
    .{ .from = 0x03c2, .to = 0x03c3 }, .{ .from = 0x03c3, .to = 0x03a3 }, .{ .from = 0x03c6, .to = 0x03d5 },
    .{ .from = 0x03c9, .to = 0x2126 }, .{ .from = 0x03d0, .to = 0x0392 }, .{ .from = 0x03d1, .to = 0x03f4 },
    .{ .from = 0x03d5, .to = 0x03a6 }, .{ .from = 0x03d6, .to = 0x03a0 }, .{ .from = 0x03f0, .to = 0x039a },
    .{ .from = 0x03f1, .to = 0x03a1 }, .{ .from = 0x03f4, .to = 0x0398 }, .{ .from = 0x03f5, .to = 0x0395 },
    .{ .from = 0x0412, .to = 0x0432 }, .{ .from = 0x0414, .to = 0x0434 }, .{ .from = 0x041e, .to = 0x043e },
    .{ .from = 0x0421, .to = 0x0441 }, .{ .from = 0x0422, .to = 0x0442 }, .{ .from = 0x042a, .to = 0x044a },
    .{ .from = 0x0432, .to = 0x1c80 }, .{ .from = 0x0434, .to = 0x1c81 }, .{ .from = 0x043e, .to = 0x1c82 },
    .{ .from = 0x0441, .to = 0x1c83 }, .{ .from = 0x0442, .to = 0x1c84 }, .{ .from = 0x044a, .to = 0x1c86 },
    .{ .from = 0x0462, .to = 0x0463 }, .{ .from = 0x0463, .to = 0x1c87 }, .{ .from = 0x1c80, .to = 0x0412 },
    .{ .from = 0x1c81, .to = 0x0414 }, .{ .from = 0x1c82, .to = 0x041e }, .{ .from = 0x1c83, .to = 0x0421 },
    .{ .from = 0x1c84, .to = 0x1c85 }, .{ .from = 0x1c85, .to = 0x0422 }, .{ .from = 0x1c86, .to = 0x042a },
    .{ .from = 0x1c87, .to = 0x0462 }, .{ .from = 0x1c88, .to = 0xa64a }, .{ .from = 0x1e60, .to = 0x1e61 },
    .{ .from = 0x1e61, .to = 0x1e9b }, .{ .from = 0x1e9b, .to = 0x1e60 }, .{ .from = 0x1e9e, .to = 0x00df },
    .{ .from = 0x1fbe, .to = 0x0345 }, .{ .from = 0x1fd3, .to = 0x0390 }, .{ .from = 0x1fe3, .to = 0x03b0 },
    .{ .from = 0x2126, .to = 0x03a9 }, .{ .from = 0x212a, .to = 0x004b }, .{ .from = 0x212b, .to = 0x00c5 },
    .{ .from = 0xa64a, .to = 0xa64b }, .{ .from = 0xa64b, .to = 0x1c88 }, .{ .from = 0xfb05, .to = 0xfb06 },
    .{ .from = 0xfb06, .to = 0xfb05 },
};

// Sorted case ranges (unicode CaseRanges). `upper_lower` marks a range that
// alternates upper/lower case.
const case_ranges = [_]CaseRange{
    .{ .lo = 0x0041, .hi = 0x005a, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x0061, .hi = 0x007a, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x00b5, .hi = 0x00b5, .upper = 743, .lower = 0, .title = 743 },
    .{ .lo = 0x00c0, .hi = 0x00d6, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x00d8, .hi = 0x00de, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x00e0, .hi = 0x00f6, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x00f8, .hi = 0x00fe, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x00ff, .hi = 0x00ff, .upper = 121, .lower = 0, .title = 121 },
    .{ .lo = 0x0100, .hi = 0x012f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0130, .hi = 0x0130, .upper = 0, .lower = -199, .title = 0 },
    .{ .lo = 0x0131, .hi = 0x0131, .upper = -232, .lower = 0, .title = -232 },
    .{ .lo = 0x0132, .hi = 0x0137, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0139, .hi = 0x0148, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x014a, .hi = 0x0177, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0178, .hi = 0x0178, .upper = 0, .lower = -121, .title = 0 },
    .{ .lo = 0x0179, .hi = 0x017e, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x017f, .hi = 0x017f, .upper = -300, .lower = 0, .title = -300 },
    .{ .lo = 0x0180, .hi = 0x0180, .upper = 195, .lower = 0, .title = 195 },
    .{ .lo = 0x0181, .hi = 0x0181, .upper = 0, .lower = 210, .title = 0 },
    .{ .lo = 0x0182, .hi = 0x0185, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0186, .hi = 0x0186, .upper = 0, .lower = 206, .title = 0 },
    .{ .lo = 0x0187, .hi = 0x0188, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0189, .hi = 0x018a, .upper = 0, .lower = 205, .title = 0 },
    .{ .lo = 0x018b, .hi = 0x018c, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x018e, .hi = 0x018e, .upper = 0, .lower = 79, .title = 0 },
    .{ .lo = 0x018f, .hi = 0x018f, .upper = 0, .lower = 202, .title = 0 },
    .{ .lo = 0x0190, .hi = 0x0190, .upper = 0, .lower = 203, .title = 0 },
    .{ .lo = 0x0191, .hi = 0x0192, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0193, .hi = 0x0193, .upper = 0, .lower = 205, .title = 0 },
    .{ .lo = 0x0194, .hi = 0x0194, .upper = 0, .lower = 207, .title = 0 },
    .{ .lo = 0x0195, .hi = 0x0195, .upper = 97, .lower = 0, .title = 97 },
    .{ .lo = 0x0196, .hi = 0x0196, .upper = 0, .lower = 211, .title = 0 },
    .{ .lo = 0x0197, .hi = 0x0197, .upper = 0, .lower = 209, .title = 0 },
    .{ .lo = 0x0198, .hi = 0x0199, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x019a, .hi = 0x019a, .upper = 163, .lower = 0, .title = 163 },
    .{ .lo = 0x019b, .hi = 0x019b, .upper = 42561, .lower = 0, .title = 42561 },
    .{ .lo = 0x019c, .hi = 0x019c, .upper = 0, .lower = 211, .title = 0 },
    .{ .lo = 0x019d, .hi = 0x019d, .upper = 0, .lower = 213, .title = 0 },
    .{ .lo = 0x019e, .hi = 0x019e, .upper = 130, .lower = 0, .title = 130 },
    .{ .lo = 0x019f, .hi = 0x019f, .upper = 0, .lower = 214, .title = 0 },
    .{ .lo = 0x01a0, .hi = 0x01a5, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01a6, .hi = 0x01a6, .upper = 0, .lower = 218, .title = 0 },
    .{ .lo = 0x01a7, .hi = 0x01a8, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01a9, .hi = 0x01a9, .upper = 0, .lower = 218, .title = 0 },
    .{ .lo = 0x01ac, .hi = 0x01ad, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01ae, .hi = 0x01ae, .upper = 0, .lower = 218, .title = 0 },
    .{ .lo = 0x01af, .hi = 0x01b0, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01b1, .hi = 0x01b2, .upper = 0, .lower = 217, .title = 0 },
    .{ .lo = 0x01b3, .hi = 0x01b6, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01b7, .hi = 0x01b7, .upper = 0, .lower = 219, .title = 0 },
    .{ .lo = 0x01b8, .hi = 0x01b9, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01bc, .hi = 0x01bd, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01bf, .hi = 0x01bf, .upper = 56, .lower = 0, .title = 56 },
    .{ .lo = 0x01c4, .hi = 0x01c4, .upper = 0, .lower = 2, .title = 1 },
    .{ .lo = 0x01c5, .hi = 0x01c5, .upper = -1, .lower = 1, .title = 0 },
    .{ .lo = 0x01c6, .hi = 0x01c6, .upper = -2, .lower = 0, .title = -1 },
    .{ .lo = 0x01c7, .hi = 0x01c7, .upper = 0, .lower = 2, .title = 1 },
    .{ .lo = 0x01c8, .hi = 0x01c8, .upper = -1, .lower = 1, .title = 0 },
    .{ .lo = 0x01c9, .hi = 0x01c9, .upper = -2, .lower = 0, .title = -1 },
    .{ .lo = 0x01ca, .hi = 0x01ca, .upper = 0, .lower = 2, .title = 1 },
    .{ .lo = 0x01cb, .hi = 0x01cb, .upper = -1, .lower = 1, .title = 0 },
    .{ .lo = 0x01cc, .hi = 0x01cc, .upper = -2, .lower = 0, .title = -1 },
    .{ .lo = 0x01cd, .hi = 0x01dc, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01dd, .hi = 0x01dd, .upper = -79, .lower = 0, .title = -79 },
    .{ .lo = 0x01de, .hi = 0x01ef, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01f1, .hi = 0x01f1, .upper = 0, .lower = 2, .title = 1 },
    .{ .lo = 0x01f2, .hi = 0x01f2, .upper = -1, .lower = 1, .title = 0 },
    .{ .lo = 0x01f3, .hi = 0x01f3, .upper = -2, .lower = 0, .title = -1 },
    .{ .lo = 0x01f4, .hi = 0x01f5, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x01f6, .hi = 0x01f6, .upper = 0, .lower = -97, .title = 0 },
    .{ .lo = 0x01f7, .hi = 0x01f7, .upper = 0, .lower = -56, .title = 0 },
    .{ .lo = 0x01f8, .hi = 0x021f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0220, .hi = 0x0220, .upper = 0, .lower = -130, .title = 0 },
    .{ .lo = 0x0222, .hi = 0x0233, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x023a, .hi = 0x023a, .upper = 0, .lower = 10795, .title = 0 },
    .{ .lo = 0x023b, .hi = 0x023c, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x023d, .hi = 0x023d, .upper = 0, .lower = -163, .title = 0 },
    .{ .lo = 0x023e, .hi = 0x023e, .upper = 0, .lower = 10792, .title = 0 },
    .{ .lo = 0x023f, .hi = 0x0240, .upper = 10815, .lower = 0, .title = 10815 },
    .{ .lo = 0x0241, .hi = 0x0242, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0243, .hi = 0x0243, .upper = 0, .lower = -195, .title = 0 },
    .{ .lo = 0x0244, .hi = 0x0244, .upper = 0, .lower = 69, .title = 0 },
    .{ .lo = 0x0245, .hi = 0x0245, .upper = 0, .lower = 71, .title = 0 },
    .{ .lo = 0x0246, .hi = 0x024f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0250, .hi = 0x0250, .upper = 10783, .lower = 0, .title = 10783 },
    .{ .lo = 0x0251, .hi = 0x0251, .upper = 10780, .lower = 0, .title = 10780 },
    .{ .lo = 0x0252, .hi = 0x0252, .upper = 10782, .lower = 0, .title = 10782 },
    .{ .lo = 0x0253, .hi = 0x0253, .upper = -210, .lower = 0, .title = -210 },
    .{ .lo = 0x0254, .hi = 0x0254, .upper = -206, .lower = 0, .title = -206 },
    .{ .lo = 0x0256, .hi = 0x0257, .upper = -205, .lower = 0, .title = -205 },
    .{ .lo = 0x0259, .hi = 0x0259, .upper = -202, .lower = 0, .title = -202 },
    .{ .lo = 0x025b, .hi = 0x025b, .upper = -203, .lower = 0, .title = -203 },
    .{ .lo = 0x025c, .hi = 0x025c, .upper = 42319, .lower = 0, .title = 42319 },
    .{ .lo = 0x0260, .hi = 0x0260, .upper = -205, .lower = 0, .title = -205 },
    .{ .lo = 0x0261, .hi = 0x0261, .upper = 42315, .lower = 0, .title = 42315 },
    .{ .lo = 0x0263, .hi = 0x0263, .upper = -207, .lower = 0, .title = -207 },
    .{ .lo = 0x0264, .hi = 0x0264, .upper = 42343, .lower = 0, .title = 42343 },
    .{ .lo = 0x0265, .hi = 0x0265, .upper = 42280, .lower = 0, .title = 42280 },
    .{ .lo = 0x0266, .hi = 0x0266, .upper = 42308, .lower = 0, .title = 42308 },
    .{ .lo = 0x0268, .hi = 0x0268, .upper = -209, .lower = 0, .title = -209 },
    .{ .lo = 0x0269, .hi = 0x0269, .upper = -211, .lower = 0, .title = -211 },
    .{ .lo = 0x026a, .hi = 0x026a, .upper = 42308, .lower = 0, .title = 42308 },
    .{ .lo = 0x026b, .hi = 0x026b, .upper = 10743, .lower = 0, .title = 10743 },
    .{ .lo = 0x026c, .hi = 0x026c, .upper = 42305, .lower = 0, .title = 42305 },
    .{ .lo = 0x026f, .hi = 0x026f, .upper = -211, .lower = 0, .title = -211 },
    .{ .lo = 0x0271, .hi = 0x0271, .upper = 10749, .lower = 0, .title = 10749 },
    .{ .lo = 0x0272, .hi = 0x0272, .upper = -213, .lower = 0, .title = -213 },
    .{ .lo = 0x0275, .hi = 0x0275, .upper = -214, .lower = 0, .title = -214 },
    .{ .lo = 0x027d, .hi = 0x027d, .upper = 10727, .lower = 0, .title = 10727 },
    .{ .lo = 0x0280, .hi = 0x0280, .upper = -218, .lower = 0, .title = -218 },
    .{ .lo = 0x0282, .hi = 0x0282, .upper = 42307, .lower = 0, .title = 42307 },
    .{ .lo = 0x0283, .hi = 0x0283, .upper = -218, .lower = 0, .title = -218 },
    .{ .lo = 0x0287, .hi = 0x0287, .upper = 42282, .lower = 0, .title = 42282 },
    .{ .lo = 0x0288, .hi = 0x0288, .upper = -218, .lower = 0, .title = -218 },
    .{ .lo = 0x0289, .hi = 0x0289, .upper = -69, .lower = 0, .title = -69 },
    .{ .lo = 0x028a, .hi = 0x028b, .upper = -217, .lower = 0, .title = -217 },
    .{ .lo = 0x028c, .hi = 0x028c, .upper = -71, .lower = 0, .title = -71 },
    .{ .lo = 0x0292, .hi = 0x0292, .upper = -219, .lower = 0, .title = -219 },
    .{ .lo = 0x029d, .hi = 0x029d, .upper = 42261, .lower = 0, .title = 42261 },
    .{ .lo = 0x029e, .hi = 0x029e, .upper = 42258, .lower = 0, .title = 42258 },
    .{ .lo = 0x0345, .hi = 0x0345, .upper = 84, .lower = 0, .title = 84 },
    .{ .lo = 0x0370, .hi = 0x0373, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0376, .hi = 0x0377, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x037b, .hi = 0x037d, .upper = 130, .lower = 0, .title = 130 },
    .{ .lo = 0x037f, .hi = 0x037f, .upper = 0, .lower = 116, .title = 0 },
    .{ .lo = 0x0386, .hi = 0x0386, .upper = 0, .lower = 38, .title = 0 },
    .{ .lo = 0x0388, .hi = 0x038a, .upper = 0, .lower = 37, .title = 0 },
    .{ .lo = 0x038c, .hi = 0x038c, .upper = 0, .lower = 64, .title = 0 },
    .{ .lo = 0x038e, .hi = 0x038f, .upper = 0, .lower = 63, .title = 0 },
    .{ .lo = 0x0391, .hi = 0x03a1, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x03a3, .hi = 0x03ab, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x03ac, .hi = 0x03ac, .upper = -38, .lower = 0, .title = -38 },
    .{ .lo = 0x03ad, .hi = 0x03af, .upper = -37, .lower = 0, .title = -37 },
    .{ .lo = 0x03b1, .hi = 0x03c1, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x03c2, .hi = 0x03c2, .upper = -31, .lower = 0, .title = -31 },
    .{ .lo = 0x03c3, .hi = 0x03cb, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x03cc, .hi = 0x03cc, .upper = -64, .lower = 0, .title = -64 },
    .{ .lo = 0x03cd, .hi = 0x03ce, .upper = -63, .lower = 0, .title = -63 },
    .{ .lo = 0x03cf, .hi = 0x03cf, .upper = 0, .lower = 8, .title = 0 },
    .{ .lo = 0x03d0, .hi = 0x03d0, .upper = -62, .lower = 0, .title = -62 },
    .{ .lo = 0x03d1, .hi = 0x03d1, .upper = -57, .lower = 0, .title = -57 },
    .{ .lo = 0x03d5, .hi = 0x03d5, .upper = -47, .lower = 0, .title = -47 },
    .{ .lo = 0x03d6, .hi = 0x03d6, .upper = -54, .lower = 0, .title = -54 },
    .{ .lo = 0x03d7, .hi = 0x03d7, .upper = -8, .lower = 0, .title = -8 },
    .{ .lo = 0x03d8, .hi = 0x03ef, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x03f0, .hi = 0x03f0, .upper = -86, .lower = 0, .title = -86 },
    .{ .lo = 0x03f1, .hi = 0x03f1, .upper = -80, .lower = 0, .title = -80 },
    .{ .lo = 0x03f2, .hi = 0x03f2, .upper = 7, .lower = 0, .title = 7 },
    .{ .lo = 0x03f3, .hi = 0x03f3, .upper = -116, .lower = 0, .title = -116 },
    .{ .lo = 0x03f4, .hi = 0x03f4, .upper = 0, .lower = -60, .title = 0 },
    .{ .lo = 0x03f5, .hi = 0x03f5, .upper = -96, .lower = 0, .title = -96 },
    .{ .lo = 0x03f7, .hi = 0x03f8, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x03f9, .hi = 0x03f9, .upper = 0, .lower = -7, .title = 0 },
    .{ .lo = 0x03fa, .hi = 0x03fb, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x03fd, .hi = 0x03ff, .upper = 0, .lower = -130, .title = 0 },
    .{ .lo = 0x0400, .hi = 0x040f, .upper = 0, .lower = 80, .title = 0 },
    .{ .lo = 0x0410, .hi = 0x042f, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x0430, .hi = 0x044f, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x0450, .hi = 0x045f, .upper = -80, .lower = 0, .title = -80 },
    .{ .lo = 0x0460, .hi = 0x0481, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x048a, .hi = 0x04bf, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x04c0, .hi = 0x04c0, .upper = 0, .lower = 15, .title = 0 },
    .{ .lo = 0x04c1, .hi = 0x04ce, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x04cf, .hi = 0x04cf, .upper = -15, .lower = 0, .title = -15 },
    .{ .lo = 0x04d0, .hi = 0x052f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x0531, .hi = 0x0556, .upper = 0, .lower = 48, .title = 0 },
    .{ .lo = 0x0561, .hi = 0x0586, .upper = -48, .lower = 0, .title = -48 },
    .{ .lo = 0x10a0, .hi = 0x10c5, .upper = 0, .lower = 7264, .title = 0 },
    .{ .lo = 0x10c7, .hi = 0x10c7, .upper = 0, .lower = 7264, .title = 0 },
    .{ .lo = 0x10cd, .hi = 0x10cd, .upper = 0, .lower = 7264, .title = 0 },
    .{ .lo = 0x10d0, .hi = 0x10fa, .upper = 3008, .lower = 0, .title = 0 },
    .{ .lo = 0x10fd, .hi = 0x10ff, .upper = 3008, .lower = 0, .title = 0 },
    .{ .lo = 0x13a0, .hi = 0x13ef, .upper = 0, .lower = 38864, .title = 0 },
    .{ .lo = 0x13f0, .hi = 0x13f5, .upper = 0, .lower = 8, .title = 0 },
    .{ .lo = 0x13f8, .hi = 0x13fd, .upper = -8, .lower = 0, .title = -8 },
    .{ .lo = 0x1c80, .hi = 0x1c80, .upper = -6254, .lower = 0, .title = -6254 },
    .{ .lo = 0x1c81, .hi = 0x1c81, .upper = -6253, .lower = 0, .title = -6253 },
    .{ .lo = 0x1c82, .hi = 0x1c82, .upper = -6244, .lower = 0, .title = -6244 },
    .{ .lo = 0x1c83, .hi = 0x1c84, .upper = -6242, .lower = 0, .title = -6242 },
    .{ .lo = 0x1c85, .hi = 0x1c85, .upper = -6243, .lower = 0, .title = -6243 },
    .{ .lo = 0x1c86, .hi = 0x1c86, .upper = -6236, .lower = 0, .title = -6236 },
    .{ .lo = 0x1c87, .hi = 0x1c87, .upper = -6181, .lower = 0, .title = -6181 },
    .{ .lo = 0x1c88, .hi = 0x1c88, .upper = 35266, .lower = 0, .title = 35266 },
    .{ .lo = 0x1c89, .hi = 0x1c8a, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x1c90, .hi = 0x1cba, .upper = 0, .lower = -3008, .title = 0 },
    .{ .lo = 0x1cbd, .hi = 0x1cbf, .upper = 0, .lower = -3008, .title = 0 },
    .{ .lo = 0x1d79, .hi = 0x1d79, .upper = 35332, .lower = 0, .title = 35332 },
    .{ .lo = 0x1d7d, .hi = 0x1d7d, .upper = 3814, .lower = 0, .title = 3814 },
    .{ .lo = 0x1d8e, .hi = 0x1d8e, .upper = 35384, .lower = 0, .title = 35384 },
    .{ .lo = 0x1e00, .hi = 0x1e95, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x1e9b, .hi = 0x1e9b, .upper = -59, .lower = 0, .title = -59 },
    .{ .lo = 0x1e9e, .hi = 0x1e9e, .upper = 0, .lower = -7615, .title = 0 },
    .{ .lo = 0x1ea0, .hi = 0x1eff, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x1f00, .hi = 0x1f07, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f08, .hi = 0x1f0f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f10, .hi = 0x1f15, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f18, .hi = 0x1f1d, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f20, .hi = 0x1f27, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f28, .hi = 0x1f2f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f30, .hi = 0x1f37, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f38, .hi = 0x1f3f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f40, .hi = 0x1f45, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f48, .hi = 0x1f4d, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f51, .hi = 0x1f51, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f53, .hi = 0x1f53, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f55, .hi = 0x1f55, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f57, .hi = 0x1f57, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f59, .hi = 0x1f59, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f5b, .hi = 0x1f5b, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f5d, .hi = 0x1f5d, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f5f, .hi = 0x1f5f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f60, .hi = 0x1f67, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f68, .hi = 0x1f6f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f70, .hi = 0x1f71, .upper = 74, .lower = 0, .title = 74 },
    .{ .lo = 0x1f72, .hi = 0x1f75, .upper = 86, .lower = 0, .title = 86 },
    .{ .lo = 0x1f76, .hi = 0x1f77, .upper = 100, .lower = 0, .title = 100 },
    .{ .lo = 0x1f78, .hi = 0x1f79, .upper = 128, .lower = 0, .title = 128 },
    .{ .lo = 0x1f7a, .hi = 0x1f7b, .upper = 112, .lower = 0, .title = 112 },
    .{ .lo = 0x1f7c, .hi = 0x1f7d, .upper = 126, .lower = 0, .title = 126 },
    .{ .lo = 0x1f80, .hi = 0x1f87, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f88, .hi = 0x1f8f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1f90, .hi = 0x1f97, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1f98, .hi = 0x1f9f, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1fa0, .hi = 0x1fa7, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1fa8, .hi = 0x1faf, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1fb0, .hi = 0x1fb1, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1fb3, .hi = 0x1fb3, .upper = 9, .lower = 0, .title = 9 },
    .{ .lo = 0x1fb8, .hi = 0x1fb9, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1fba, .hi = 0x1fbb, .upper = 0, .lower = -74, .title = 0 },
    .{ .lo = 0x1fbc, .hi = 0x1fbc, .upper = 0, .lower = -9, .title = 0 },
    .{ .lo = 0x1fbe, .hi = 0x1fbe, .upper = -7205, .lower = 0, .title = -7205 },
    .{ .lo = 0x1fc3, .hi = 0x1fc3, .upper = 9, .lower = 0, .title = 9 },
    .{ .lo = 0x1fc8, .hi = 0x1fcb, .upper = 0, .lower = -86, .title = 0 },
    .{ .lo = 0x1fcc, .hi = 0x1fcc, .upper = 0, .lower = -9, .title = 0 },
    .{ .lo = 0x1fd0, .hi = 0x1fd1, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1fd8, .hi = 0x1fd9, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1fda, .hi = 0x1fdb, .upper = 0, .lower = -100, .title = 0 },
    .{ .lo = 0x1fe0, .hi = 0x1fe1, .upper = 8, .lower = 0, .title = 8 },
    .{ .lo = 0x1fe5, .hi = 0x1fe5, .upper = 7, .lower = 0, .title = 7 },
    .{ .lo = 0x1fe8, .hi = 0x1fe9, .upper = 0, .lower = -8, .title = 0 },
    .{ .lo = 0x1fea, .hi = 0x1feb, .upper = 0, .lower = -112, .title = 0 },
    .{ .lo = 0x1fec, .hi = 0x1fec, .upper = 0, .lower = -7, .title = 0 },
    .{ .lo = 0x1ff3, .hi = 0x1ff3, .upper = 9, .lower = 0, .title = 9 },
    .{ .lo = 0x1ff8, .hi = 0x1ff9, .upper = 0, .lower = -128, .title = 0 },
    .{ .lo = 0x1ffa, .hi = 0x1ffb, .upper = 0, .lower = -126, .title = 0 },
    .{ .lo = 0x1ffc, .hi = 0x1ffc, .upper = 0, .lower = -9, .title = 0 },
    .{ .lo = 0x2126, .hi = 0x2126, .upper = 0, .lower = -7517, .title = 0 },
    .{ .lo = 0x212a, .hi = 0x212a, .upper = 0, .lower = -8383, .title = 0 },
    .{ .lo = 0x212b, .hi = 0x212b, .upper = 0, .lower = -8262, .title = 0 },
    .{ .lo = 0x2132, .hi = 0x2132, .upper = 0, .lower = 28, .title = 0 },
    .{ .lo = 0x214e, .hi = 0x214e, .upper = -28, .lower = 0, .title = -28 },
    .{ .lo = 0x2160, .hi = 0x216f, .upper = 0, .lower = 16, .title = 0 },
    .{ .lo = 0x2170, .hi = 0x217f, .upper = -16, .lower = 0, .title = -16 },
    .{ .lo = 0x2183, .hi = 0x2184, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x24b6, .hi = 0x24cf, .upper = 0, .lower = 26, .title = 0 },
    .{ .lo = 0x24d0, .hi = 0x24e9, .upper = -26, .lower = 0, .title = -26 },
    .{ .lo = 0x2c00, .hi = 0x2c2f, .upper = 0, .lower = 48, .title = 0 },
    .{ .lo = 0x2c30, .hi = 0x2c5f, .upper = -48, .lower = 0, .title = -48 },
    .{ .lo = 0x2c60, .hi = 0x2c61, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2c62, .hi = 0x2c62, .upper = 0, .lower = -10743, .title = 0 },
    .{ .lo = 0x2c63, .hi = 0x2c63, .upper = 0, .lower = -3814, .title = 0 },
    .{ .lo = 0x2c64, .hi = 0x2c64, .upper = 0, .lower = -10727, .title = 0 },
    .{ .lo = 0x2c65, .hi = 0x2c65, .upper = -10795, .lower = 0, .title = -10795 },
    .{ .lo = 0x2c66, .hi = 0x2c66, .upper = -10792, .lower = 0, .title = -10792 },
    .{ .lo = 0x2c67, .hi = 0x2c6c, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2c6d, .hi = 0x2c6d, .upper = 0, .lower = -10780, .title = 0 },
    .{ .lo = 0x2c6e, .hi = 0x2c6e, .upper = 0, .lower = -10749, .title = 0 },
    .{ .lo = 0x2c6f, .hi = 0x2c6f, .upper = 0, .lower = -10783, .title = 0 },
    .{ .lo = 0x2c70, .hi = 0x2c70, .upper = 0, .lower = -10782, .title = 0 },
    .{ .lo = 0x2c72, .hi = 0x2c73, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2c75, .hi = 0x2c76, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2c7e, .hi = 0x2c7f, .upper = 0, .lower = -10815, .title = 0 },
    .{ .lo = 0x2c80, .hi = 0x2ce3, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2ceb, .hi = 0x2cee, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2cf2, .hi = 0x2cf3, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0x2d00, .hi = 0x2d25, .upper = -7264, .lower = 0, .title = -7264 },
    .{ .lo = 0x2d27, .hi = 0x2d27, .upper = -7264, .lower = 0, .title = -7264 },
    .{ .lo = 0x2d2d, .hi = 0x2d2d, .upper = -7264, .lower = 0, .title = -7264 },
    .{ .lo = 0xa640, .hi = 0xa66d, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa680, .hi = 0xa69b, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa722, .hi = 0xa72f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa732, .hi = 0xa76f, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa779, .hi = 0xa77c, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa77d, .hi = 0xa77d, .upper = 0, .lower = -35332, .title = 0 },
    .{ .lo = 0xa77e, .hi = 0xa787, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa78b, .hi = 0xa78c, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa78d, .hi = 0xa78d, .upper = 0, .lower = -42280, .title = 0 },
    .{ .lo = 0xa790, .hi = 0xa793, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa794, .hi = 0xa794, .upper = 48, .lower = 0, .title = 48 },
    .{ .lo = 0xa796, .hi = 0xa7a9, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa7aa, .hi = 0xa7aa, .upper = 0, .lower = -42308, .title = 0 },
    .{ .lo = 0xa7ab, .hi = 0xa7ab, .upper = 0, .lower = -42319, .title = 0 },
    .{ .lo = 0xa7ac, .hi = 0xa7ac, .upper = 0, .lower = -42315, .title = 0 },
    .{ .lo = 0xa7ad, .hi = 0xa7ad, .upper = 0, .lower = -42305, .title = 0 },
    .{ .lo = 0xa7ae, .hi = 0xa7ae, .upper = 0, .lower = -42308, .title = 0 },
    .{ .lo = 0xa7b0, .hi = 0xa7b0, .upper = 0, .lower = -42258, .title = 0 },
    .{ .lo = 0xa7b1, .hi = 0xa7b1, .upper = 0, .lower = -42282, .title = 0 },
    .{ .lo = 0xa7b2, .hi = 0xa7b2, .upper = 0, .lower = -42261, .title = 0 },
    .{ .lo = 0xa7b3, .hi = 0xa7b3, .upper = 0, .lower = 928, .title = 0 },
    .{ .lo = 0xa7b4, .hi = 0xa7c3, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa7c4, .hi = 0xa7c4, .upper = 0, .lower = -48, .title = 0 },
    .{ .lo = 0xa7c5, .hi = 0xa7c5, .upper = 0, .lower = -42307, .title = 0 },
    .{ .lo = 0xa7c6, .hi = 0xa7c6, .upper = 0, .lower = -35384, .title = 0 },
    .{ .lo = 0xa7c7, .hi = 0xa7ca, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa7cb, .hi = 0xa7cb, .upper = 0, .lower = -42343, .title = 0 },
    .{ .lo = 0xa7cc, .hi = 0xa7db, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xa7dc, .hi = 0xa7dc, .upper = 0, .lower = -42561, .title = 0 },
    .{ .lo = 0xa7f5, .hi = 0xa7f6, .upper = 1114112, .lower = 1114112, .title = 1114112 },
    .{ .lo = 0xab53, .hi = 0xab53, .upper = -928, .lower = 0, .title = -928 },
    .{ .lo = 0xab70, .hi = 0xabbf, .upper = -38864, .lower = 0, .title = -38864 },
    .{ .lo = 0xff21, .hi = 0xff3a, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0xff41, .hi = 0xff5a, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x10400, .hi = 0x10427, .upper = 0, .lower = 40, .title = 0 },
    .{ .lo = 0x10428, .hi = 0x1044f, .upper = -40, .lower = 0, .title = -40 },
    .{ .lo = 0x104b0, .hi = 0x104d3, .upper = 0, .lower = 40, .title = 0 },
    .{ .lo = 0x104d8, .hi = 0x104fb, .upper = -40, .lower = 0, .title = -40 },
    .{ .lo = 0x10570, .hi = 0x1057a, .upper = 0, .lower = 39, .title = 0 },
    .{ .lo = 0x1057c, .hi = 0x1058a, .upper = 0, .lower = 39, .title = 0 },
    .{ .lo = 0x1058c, .hi = 0x10592, .upper = 0, .lower = 39, .title = 0 },
    .{ .lo = 0x10594, .hi = 0x10595, .upper = 0, .lower = 39, .title = 0 },
    .{ .lo = 0x10597, .hi = 0x105a1, .upper = -39, .lower = 0, .title = -39 },
    .{ .lo = 0x105a3, .hi = 0x105b1, .upper = -39, .lower = 0, .title = -39 },
    .{ .lo = 0x105b3, .hi = 0x105b9, .upper = -39, .lower = 0, .title = -39 },
    .{ .lo = 0x105bb, .hi = 0x105bc, .upper = -39, .lower = 0, .title = -39 },
    .{ .lo = 0x10c80, .hi = 0x10cb2, .upper = 0, .lower = 64, .title = 0 },
    .{ .lo = 0x10cc0, .hi = 0x10cf2, .upper = -64, .lower = 0, .title = -64 },
    .{ .lo = 0x10d50, .hi = 0x10d65, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x10d70, .hi = 0x10d85, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x118a0, .hi = 0x118bf, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x118c0, .hi = 0x118df, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x16e40, .hi = 0x16e5f, .upper = 0, .lower = 32, .title = 0 },
    .{ .lo = 0x16e60, .hi = 0x16e7f, .upper = -32, .lower = 0, .title = -32 },
    .{ .lo = 0x16ea0, .hi = 0x16eb8, .upper = 0, .lower = 27, .title = 0 },
    .{ .lo = 0x16ebb, .hi = 0x16ed3, .upper = -27, .lower = 0, .title = -27 },
    .{ .lo = 0x1e900, .hi = 0x1e921, .upper = 0, .lower = 34, .title = 0 },
    .{ .lo = 0x1e922, .hi = 0x1e943, .upper = -34, .lower = 0, .title = -34 },
};


const upper_lower: i32 = 0x110000; // unicode.UpperLower (MaxRune + 1)

fn lookupCaseRange(r: u21) ?CaseRange {
    var low: usize = 0;
    var high: usize = case_ranges.len;
    while (low < high) {
        const mid = low + (high - low) / 2;
        const range = case_ranges[mid];
        if (r < range.lo) {
            high = mid;
        } else if (r > range.hi) {
            low = mid + 1;
        } else {
            return range;
        }
    }
    return null;
}

/// unicode.convertCase for one case (0 upper, 1 lower, 2 title).
fn convertCase(range: CaseRange, r: u21, case_index: u2) u21 {
    const delta: i32 = switch (case_index) {
        0 => range.upper,
        1 => range.lower,
        else => range.title,
    };
    if (delta > max_rune) {
        // An upper/lower alternating range: the low bit of the offset from the
        // range start selects the case.
        const offset = (r - range.lo) & ~@as(u21, 1);
        return range.lo + (offset | @as(u21, case_index & 1));
    }
    return @intCast(@as(i32, r) + delta);
}

/// Go's unicode.SimpleFold.
fn simpleFold(r: u21) u21 {
    if (r < 128) return ascii_fold[r];
    var low: usize = 0;
    var high: usize = case_orbit.len;
    while (low < high) {
        const mid = low + (high - low) / 2;
        if (case_orbit[mid].from < r) low = mid + 1 else high = mid;
    }
    if (low < case_orbit.len and case_orbit[low].from == r) return case_orbit[low].to;
    const range = lookupCaseRange(r) orelse return r;
    const lower = convertCase(range, r, 1);
    if (lower != r) return lower;
    return convertCase(range, r, 0);
}

/// The smallest rune in r's SimpleFold orbit. Idempotent, and equal to Go's
/// encoding/json foldRune; ASCII letters fold to uppercase.
pub fn foldRune(r: u21) u21 {
    var current = r;
    while (true) {
        const next = simpleFold(current);
        if (next <= current) return next;
        current = next;
    }
}

fn nextRune(value: []const u8, index: *usize) u21 {
    const byte = value[index.*];
    if (byte < 0x80) {
        index.* += 1;
        return byte;
    }
    const length: usize = std.unicode.utf8ByteSequenceLength(byte) catch {
        index.* += 1;
        return 0xFFFD;
    };
    if (index.* + length > value.len or !std.unicode.utf8ValidateSlice(value[index.* .. index.* + length])) {
        index.* += 1;
        return 0xFFFD;
    }
    const decoded = std.unicode.utf8Decode(value[index.* .. index.* + length]) catch {
        index.* += 1;
        return 0xFFFD;
    };
    index.* += length;
    return decoded;
}

/// encoding/json's foldName equality. Used for JSON field-name matching, which
/// is exact first and folded second in the reference decoder.
pub fn foldedEqual(a: []const u8, b: []const u8) bool {
    var index_a: usize = 0;
    var index_b: usize = 0;
    while (index_a < a.len and index_b < b.len) {
        if (foldRune(nextRune(a, &index_a)) != foldRune(nextRune(b, &index_b))) return false;
    }
    return index_a == a.len and index_b == b.len;
}

