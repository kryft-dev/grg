package benchmark

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kryft-dev/grg/internal/gitengine"
)

// runGRG runs the compiled grg binary inside repoDir and returns its stdout.
func runGRG(b *testing.B, bin, repoDir string, args ...string) {
	b.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = repoDir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Exit code 1 means no match found, which is valid for some bench runs
			return
		}
		b.Fatalf("grg failed: %v\nStderr: %s", err, errBuf.String())
	}
}

// BenchmarkSearch_EndToEnd_Small100 benchmarks search across a small 100-commit repository.
func BenchmarkSearch_EndToEnd_Small100(b *testing.B) {
	bin := GetBenchmarkGRGBinary(b)
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    100,
		FileCount:      20,
		DuplicateRatio: 0.5,
		PackRepo:       true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runGRG(b, bin, repoDir, "--color=never", "-c", "BENCHMARK_")
	}
}

// BenchmarkSearch_EndToEnd_Medium500 benchmarks search across a medium 500-commit repository.
func BenchmarkSearch_EndToEnd_Medium500(b *testing.B) {
	bin := GetBenchmarkGRGBinary(b)
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    500,
		FileCount:      50,
		DuplicateRatio: 0.5,
		PackRepo:       true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runGRG(b, bin, repoDir, "--color=never", "-c", "BENCHMARK_")
	}
}

// BenchmarkSearch_EndToEnd_Large1000 benchmarks search across a large 1,000-commit repository.
func BenchmarkSearch_EndToEnd_Large1000(b *testing.B) {
	bin := GetBenchmarkGRGBinary(b)
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    1000,
		FileCount:      100,
		DuplicateRatio: 0.5,
		PackRepo:       true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runGRG(b, bin, repoDir, "--color=never", "-c", "BENCHMARK_")
	}
}

// BenchmarkDeduplication_UniqueBlobs benchmarks search throughput with 0% duplicate blobs (all unique).
func BenchmarkDeduplication_UniqueBlobs(b *testing.B) {
	bin := GetBenchmarkGRGBinary(b)
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    150,
		FileCount:      30,
		DuplicateRatio: 0.0, // 100% unique blobs
		PackRepo:       true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runGRG(b, bin, repoDir, "--color=never", "-c", "BENCHMARK_")
	}
}

// BenchmarkDeduplication_DuplicateBlobs_90Percent benchmarks search throughput with 90% identical blobs.
func BenchmarkDeduplication_DuplicateBlobs_90Percent(b *testing.B) {
	bin := GetBenchmarkGRGBinary(b)
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    150,
		FileCount:      30,
		DuplicateRatio: 0.9, // 90% identical duplicate blobs
		PackRepo:       true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runGRG(b, bin, repoDir, "--color=never", "-c", "BENCHMARK_")
	}
}

// BenchmarkPackfileReading benchmarks direct packfile object decompression and reading throughput.
func BenchmarkPackfileReading(b *testing.B) {
	repoDir := CreateBenchmarkRepo(b, BenchmarkRepoOptions{
		CommitCount:    100,
		FileCount:      20,
		DuplicateRatio: 0.2,
		PackRepo:       true,
	})

	packDir := filepath.Join(repoDir, ".git", "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		b.Fatalf("failed reading pack dir: %v", err)
	}

	var idxPath, packPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".idx") {
			idxPath = filepath.Join(packDir, e.Name())
			packPath = filepath.Join(packDir, strings.TrimSuffix(e.Name(), ".idx")+".pack")
			break
		}
	}

	packReader, err := gitengine.OpenPackfile(packPath, idxPath)
	if err != nil {
		b.Fatalf("failed opening packfile: %v", err)
	}
	defer packReader.Close()

	// Collect list of OIDs from repo HEAD
	cmd := exec.Command("git", "rev-list", "--objects", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		b.Fatalf("git rev-list failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var oids []string
	for _, l := range lines {
		parts := strings.Fields(l)
		if len(parts) > 0 && packReader.HasObject(parts[0]) {
			oids = append(oids, parts[0])
		}
	}
	if len(oids) == 0 {
		b.Fatalf("no objects found in packfile")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		oid := oids[i%len(oids)]
		obj, err := packReader.ReadObject(oid)
		if err != nil {
			b.Fatalf("failed reading object %s: %v", oid, err)
		}
		b.SetBytes(obj.Size)
	}
}

// BenchmarkDeltaDecompression benchmarks pure Go git delta application throughput.
func BenchmarkDeltaDecompression(b *testing.B) {
	// Synthesize a realistic base buffer (64KB)
	base := make([]byte, 64*1024)
	for i := range base {
		base[i] = byte((i * 31) & 0xff)
	}

	// Construct delta instruction payload:
	// Base size: 65536 -> encoded in LEB128: 0x80, 0x80, 0x04
	// Target size: 65536 + 64 -> encoded in LEB128: 0xc0, 0x80, 0x04
	deltaHdr := []byte{
		0x80, 0x80, 0x04, // base size: 65536
		0xc0, 0x80, 0x04, // target size: 65600
	}

	var deltaBody []byte
	// 1. Copy first 32KB from base: opcode 0x80 | 0x01 | 0x02 | 0x10 | 0x20
	// offset: 0, size: 32768 (0x8000)
	deltaBody = append(deltaBody, 0xb0, 0x00, 0x80) // opcode: copy, size 0x8000

	// 2. Insert 64 bytes literal
	insertData := bytes.Repeat([]byte("Delta insertion benchmark string with high throughput operations!\n"), 1)[:64]
	deltaBody = append(deltaBody, byte(len(insertData)))
	deltaBody = append(deltaBody, insertData...)

	// 3. Copy second 32KB from base: offset 32768 (0x8000), size 32768 (0x8000)
	deltaBody = append(deltaBody, 0xb2, 0x80, 0x00, 0x80) // offset 0x8000, size 0x8000

	fullDelta := append(deltaHdr, deltaBody...)

	buf := make([]byte, 0, 70*1024)
	b.SetBytes(int64(len(base)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		target, err := gitengine.ApplyDeltaWithBuffer(buf, base, fullDelta)
		if err != nil {
			b.Fatalf("ApplyDeltaWithBuffer failed: %v", err)
		}
		if len(target) != 65600 {
			b.Fatalf("unexpected target size: %d", len(target))
		}
	}
}
