package gitengine

import (
	"bytes"
	"testing"
)

func encodeLEB128(val int) []byte {
	var out []byte
	for {
		b := byte(val & 0x7f)
		val >>= 7
		if val > 0 {
			b |= 0x80
			out = append(out, b)
		} else {
			out = append(out, b)
			break
		}
	}
	return out
}

func TestApplyDelta(t *testing.T) {
	base := []byte("Hello World! This is the base content for delta testing.")

	// Test 1: Pure insert
	target1 := []byte("Brand new content without any copy.")
	var delta1 []byte
	delta1 = append(delta1, encodeLEB128(len(base))...)
	delta1 = append(delta1, encodeLEB128(len(target1))...)
	delta1 = append(delta1, byte(len(target1))) // insert opcode
	delta1 = append(delta1, target1...)

	res1, err := ApplyDelta(base, delta1)
	if err != nil {
		t.Fatalf("unexpected error for pure insert: %v", err)
	}
	if !bytes.Equal(res1, target1) {
		t.Fatalf("expected %q, got %q", target1, res1)
	}

	// Test 2: Copy and Insert combined
	// Target: "Prefix: Hello World! -- Suffix"
	var delta2 []byte
	prefix := []byte("Prefix: ")
	suffix := []byte(" -- Suffix")
	targetLen := len(prefix) + 12 + len(suffix) // "Hello World!" is 12 bytes at base[0:12]

	delta2 = append(delta2, encodeLEB128(len(base))...)
	delta2 = append(delta2, encodeLEB128(targetLen)...)
	// Insert prefix
	delta2 = append(delta2, byte(len(prefix)))
	delta2 = append(delta2, prefix...)
	// Copy "Hello World!" from base offset 0, size 12
	// cmd: 0x80 | 0x10 (size byte 1) | 0x01 (offset byte 1, though offset is 0, let's include offset byte 1 = 0)
	delta2 = append(delta2, 0x80|0x10|0x01, 0, 12)
	// Insert suffix
	delta2 = append(delta2, byte(len(suffix)))
	delta2 = append(delta2, suffix...)

	res2, err := ApplyDelta(base, delta2)
	if err != nil {
		t.Fatalf("unexpected error for copy+insert: %v", err)
	}
	expected2 := append([]byte("Prefix: "), append([]byte("Hello World!"), []byte(" -- Suffix")...)...)
	if !bytes.Equal(res2, expected2) {
		t.Fatalf("expected %q, got %q", string(expected2), string(res2))
	}

	// Test 3: Pooled buffer decoding
	buf := make([]byte, 0, 256)
	res3, err := ApplyDeltaWithBuffer(buf, base, delta2)
	if err != nil {
		t.Fatalf("ApplyDeltaWithBuffer failed: %v", err)
	}
	if !bytes.Equal(res3, expected2) {
		t.Fatalf("expected %q, got %q", string(expected2), string(res3))
	}
}

func TestApplyDeltaErrors(t *testing.T) {
	base := []byte("Small base")

	// Base size mismatch
	delta := append(encodeLEB128(200), encodeLEB128(10)...)
	_, err := ApplyDelta(base, delta)
	if err == nil {
		t.Fatal("expected error on base size mismatch, got nil")
	}

	// Copy out of bounds
	var deltaOOB []byte
	deltaOOB = append(deltaOOB, encodeLEB128(len(base))...)
	deltaOOB = append(deltaOOB, encodeLEB128(50)...)
	// Copy offset 100, size 50
	deltaOOB = append(deltaOOB, 0x80|0x10|0x01, 100, 50)
	_, err = ApplyDelta(base, deltaOOB)
	if err == nil {
		t.Fatal("expected error on copy out of bounds, got nil")
	}

	// Truncated insert
	var deltaTrunc []byte
	deltaTrunc = append(deltaTrunc, encodeLEB128(len(base))...)
	deltaTrunc = append(deltaTrunc, encodeLEB128(10)...)
	deltaTrunc = append(deltaTrunc, 10) // insert 10 bytes, but no bytes follow
	_, err = ApplyDelta(base, deltaTrunc)
	if err == nil {
		t.Fatal("expected error on truncated insert, got nil")
	}

	// Invalid zero opcode
	var deltaZero []byte
	deltaZero = append(deltaZero, encodeLEB128(len(base))...)
	deltaZero = append(deltaZero, encodeLEB128(5)...)
	deltaZero = append(deltaZero, 0) // zero opcode
	_, err = ApplyDelta(base, deltaZero)
	if err == nil {
		t.Fatal("expected error on zero opcode, got nil")
	}
}
