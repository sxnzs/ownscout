package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Anchor re-resolution.
//
// A DeltaDB-style reference is anchored to an identity rather than to a line
// number, so that it survives the code moving underneath it. OwnScout's packet
// contract (packet-v1) is frozen and carries no anchor origin and no copy of
// the cited text: the only identity an evidence span has is its SHA-256 content
// fingerprint, and the only shape it has is its recorded line count. This file
// resolves that identity against a working tree.
//
// The window whose fingerprint matches is called a relocation. Relocation never
// changes a span's status, the report counters, the exit code, or any ledger
// byte: a span whose content has moved is NOT current at the cited location, so
// it stays "failed". The clause appended to the failure message is diagnostic
// only. There is deliberately no way for relocation to turn a failure into a
// pass.

// relocateByteBudget bounds the total number of window bytes hashed while
// resolving one anchor. Neighbouring windows are probed nearest-first, so a
// small shift (the common case: lines inserted above the cited range) is found
// after a handful of probes. The budget only matters when the content is gone
// or has moved far, and it is what keeps re-resolution from degrading into an
// unbounded scan of a large file.
const relocateByteBudget = 8 << 20

// relocation is the outcome of trying to resolve one anchor.
type relocation struct {
	found     bool
	lineStart int // 1-based inclusive bounds of the match
	lineEnd   int
	shift     int // lineStart minus the cited line start
	// exhaustive reports that every window that fits in the file was probed,
	// so "not present in this file" is a statement the search actually earned.
	exhaustive bool
}

// lineStarts returns the byte offset at which each line begins. Line numbers
// are 1-based, so starts[i-1] is the first byte of line i. It returns nil for
// empty input, which has no lines. This is the same line model as
// countNormalizedLines: a "\r\n" pair is one terminator, and a final terminator
// does not introduce a trailing empty line.
func lineStarts(data []byte) []int {
	if len(data) == 0 {
		return nil
	}
	starts := make([]int, 0, bytes.Count(data, []byte("\n"))+1)
	for offset := 0; offset < len(data); {
		starts = append(starts, offset)
		index := bytes.IndexByte(data[offset:], '\n')
		if index < 0 {
			break
		}
		offset += index + 1
	}
	return starts
}

// contentEnd returns the offset just past the last content byte of 1-based line
// number, excluding its terminator. The "\r" of a "\r\n" pair belongs to the
// terminator, but a lone "\r" on an unterminated final line is content and is
// kept - exactly as hashSelectedLines keeps it.
func contentEnd(data []byte, starts []int, line int) int {
	begin := starts[line-1]
	end := len(data)
	if line < len(starts) {
		end = starts[line]
	}
	// Strip "\r\n" or "\n" only when the line is actually terminated. An
	// unterminated final line keeps a trailing "\r" as content; stripping it
	// here would let a relocation match a window that verification hashes
	// differently.
	if end > begin && data[end-1] == '\n' {
		end--
		if end > begin && data[end-1] == '\r' {
			end--
		}
	}
	return end
}

// windowHash hashes lines [lineStart, lineEnd] exactly as hashSelectedLines
// does: the selected lines are joined with "\n" and a final "\n" is appended
// for multi-line content or a non-empty single line. It differs from
// hashSelectedLines only in *how* it locates the lines - from a precomputed
// start index rather than by re-walking the file from byte zero - which is what
// makes probing many candidate windows affordable.
func windowHash(data []byte, starts []int, lineStart, lineEnd int) string {
	hasher := sha256.New()
	for line := lineStart; line <= lineEnd; line++ {
		if line > lineStart {
			hasher.Write([]byte("\n"))
		}
		hasher.Write(data[starts[line-1]:contentEnd(data, starts, line)])
	}
	if lineEnd > lineStart || contentEnd(data, starts, lineStart) > starts[lineStart-1] {
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// resolveAnchor searches for the recorded fingerprint.
//
// Candidate windows keep the cited line count, because the packet records the
// extent (line_start..line_end) as well as the identity (content_hash). Windows
// are probed nearest-first: the cited start, then one line below, one line
// above, and so on outward, preferring the lower line number on a tie. A small
// shift is therefore found immediately, and the byte budget is only spent in
// full when the content is absent or has moved beyond it.
//
// A window that does not fit in the file is skipped, so a file that shrank
// below the cited extent is still searched honestly instead of failing outright.
func resolveAnchor(data []byte, starts []int, totalLines, lineStart, lineEnd int, expected string) relocation {
	extent := lineEnd - lineStart + 1
	if extent < 1 || totalLines < extent {
		// No window of the recorded shape fits anywhere in the file.
		return relocation{exhaustive: true}
	}
	// Every valid start is probed at most once, so counting probes against the
	// number of valid starts tells us exactly whether the search covered the
	// whole file, including when it stops early on the byte budget.
	validStarts := totalLines - extent + 1

	// Order probes by distance from the cited start, but clamp the origin into
	// the range of windows that actually fit. When the cited range lies past the
	// end of a file that shrank, the nearest fitting windows are the last ones,
	// and clamping keeps the outward walk bounded by the file's line count
	// rather than by the cited line number, which a packet can set arbitrarily
	// high.
	origin := lineStart
	if origin < 1 {
		origin = 1
	}
	if origin > validStarts {
		origin = validStarts
	}

	probes := 0
	used := 0
	for distance := 0; ; distance++ {
		low, high := origin-distance, origin+distance
		if low < 1 && high > validStarts {
			break
		}
		candidates := [2]int{low, high}
		count := 2
		if distance == 0 {
			// The two directions coincide; probe the origin once.
			count = 1
		}
		for index := 0; index < count; index++ {
			candidate := candidates[index]
			if candidate < 1 || candidate > validStarts {
				continue
			}
			cost := contentEnd(data, starts, candidate+extent-1) - starts[candidate-1] + extent
			if used+cost > relocateByteBudget {
				return relocation{exhaustive: probes == validStarts}
			}
			used += cost
			probes++
			if windowHash(data, starts, candidate, candidate+extent-1) == expected {
				return relocation{
					found:     true,
					lineStart: candidate,
					lineEnd:   candidate + extent - 1,
					shift:     candidate - lineStart,
				}
			}
		}
	}
	return relocation{exhaustive: probes == validStarts}
}

// relocationClause renders the diagnostic appended to a failure message. It
// returns "" when no statement can honestly be made, which keeps the message
// byte-identical to the pre-relocation behaviour in that case.
func relocationClause(data []byte, totalLines, lineStart, lineEnd int, expected string) string {
	if lineEnd-lineStart+1 < 1 {
		return ""
	}
	result := resolveAnchor(data, lineStarts(data), totalLines, lineStart, lineEnd, expected)
	switch {
	case result.found:
		return "; content relocates to lines " + strconv.Itoa(result.lineStart) +
			"-" + strconv.Itoa(result.lineEnd) +
			" (shift " + signedShift(result.shift) + "; nearest matching window)"
	case result.exhaustive:
		return "; content not found elsewhere in this file"
	default:
		return "; relocation search stopped after its byte budget"
	}
}

// signedShift renders a line shift with an explicit sign so that a relocation
// upwards is never mistaken for a downwards one.
func signedShift(shift int) string {
	if shift < 0 {
		return strconv.Itoa(shift)
	}
	return "+" + strconv.Itoa(shift)
}
