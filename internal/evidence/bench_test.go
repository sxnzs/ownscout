package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ownscout/internal/contract"
)

// benchRepo writes a repository with one evidence file of the given size and
// shape, plus a packet citing it. The line contents are realistic enough that
// hashing and scanning dominate the benchmark.
func benchRepo(b *testing.B, sizeBytes int, lineTemplate func(i int) string) (string, contract.Packet) {
	b.Helper()
	root := b.TempDir()
	path := filepath.Join(root, "generated.txt")

	var content strings.Builder
	content.Grow(sizeBytes)
	line := 0
	for content.Len() < sizeBytes {
		line++
		content.WriteString(lineTemplate(line))
		content.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
		b.Fatal(err)
	}

	totalLines := line
	middle := func() (int, int) {
		start := totalLines/4 + 1
		end := totalLines - totalLines/4
		if end < start {
			return 1, totalLines
		}
		return start, end
	}
	ls, le := middle()
	hash := hashRange(b, []byte(content.String()), ls, le)

	packet := contract.Packet{
		PacketID: "bench-packet",
		Evidence: []contract.Evidence{{
			EvidenceID:  "e1",
			Path:        "generated.txt",
			ContentHash: "sha256:" + hash,
			LineStart:   ls,
			LineEnd:     le,
		}},
	}
	return root, packet
}

func benchLines(i int) string {
	return fmt.Sprintf("line %06d: the quick brown fox jumps over the lazy dog 0123456789", i)
}

func hashRange(b *testing.B, data []byte, ls, le int) string {
	b.Helper()
	lines := legacySplitNormalizedLines(data)
	selected := strings.Join(lines[ls-1:le], "\n")
	if selected != "" {
		selected += "\n"
	}
	sum := sha256.Sum256([]byte(selected))
	return hex.EncodeToString(sum[:])
}

func BenchmarkVerifyPacketSingleSpan(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20, 8 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			root, packet := benchRepo(b, size, benchLines)
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				report, err := VerifyPacket(root, packet)
				if err != nil {
					b.Fatal(err)
				}
				if !report.Ok {
					b.Fatal("expected verified evidence")
				}
			}
		})
	}
}

func BenchmarkVerifyPacketWholeFile(b *testing.B) {
	root, packetTemplate := benchRepo(b, 1<<20, benchLines)
	data := mustRead(b, filepath.Join(root, "generated.txt"))
	total := len(legacySplitNormalizedLines(data))
	packet := packetTemplate
	packet.Evidence[0].LineStart = 1
	packet.Evidence[0].LineEnd = total
	packet.Evidence[0].ContentHash = "sha256:" + hashRange(b, data, 1, total)
	b.SetBytes(1 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := VerifyPacket(root, packet)
		if err != nil {
			b.Fatal(err)
		}
		if !report.Ok {
			b.Fatal("expected verified evidence")
		}
	}
}

func BenchmarkVerifyPacketManySpans(b *testing.B) {
	root, packetTemplate := benchRepo(b, 2<<20, benchLines)
	total := countLines(b, filepath.Join(root, "generated.txt"))
	packet := packetTemplate
	packet.Evidence = nil
	for i := 0; i < 64; i++ {
		ls := 1 + i*(total/64)
		le := ls + total/128
		if le > total {
			le = total
		}
		packet.Evidence = append(packet.Evidence, contract.Evidence{
			EvidenceID:  fmt.Sprintf("e%d", i),
			Path:        "generated.txt",
			ContentHash: "sha256:" + hashRange(b, mustRead(b, filepath.Join(root, "generated.txt")), ls, le),
			LineStart:   ls,
			LineEnd:     le,
		})
	}
	b.SetBytes(2 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := VerifyPacket(root, packet)
		if err != nil {
			b.Fatal(err)
		}
		if !report.Ok {
			b.Fatal("expected verified evidence")
		}
	}
}

func countLines(b *testing.B, path string) int {
	b.Helper()
	data := mustRead(b, path)
	return len(legacySplitNormalizedLines(data))
}

func mustRead(b *testing.B, path string) []byte {
	b.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	return data
}
