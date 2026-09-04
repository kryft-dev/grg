package gitengine

import (
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrDeltaCorrupt indicates a malformed or invalid delta instruction stream.
	ErrDeltaCorrupt = errors.New("corrupt git delta payload")
	// ErrDeltaBaseMismatch indicates that the delta base size does not match the actual base.
	ErrDeltaBaseMismatch = errors.New("delta base size mismatch")
)

var deltaBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 64*1024)
		return &b
	},
}

// GetDeltaBuffer returns a pooled byte slice buffer with capacity for delta decoding.
func GetDeltaBuffer() *[]byte {
	return deltaBufferPool.Get().(*[]byte)
}

// PutDeltaBuffer returns a delta buffer to the pool after resetting length to 0.
func PutDeltaBuffer(b *[]byte) {
	if b == nil || cap(*b) > 4*1024*1024 {
		// Do not pool buffers larger than 4MB to prevent memory bloat
		return
	}
	*b = (*b)[:0]
	deltaBufferPool.Put(b)
}

// ReadDeltaHeader parses the base size and target size from the start of a delta payload.
func ReadDeltaHeader(delta []byte) (baseSize int, targetSize int, bytesRead int, err error) {
	pos := 0
	shift := 0

	// Read base object size (variable-length LEB128)
	for {
		if pos >= len(delta) {
			return 0, 0, 0, fmt.Errorf("%w: truncated base size header", ErrDeltaCorrupt)
		}
		b := delta[pos]
		pos++
		baseSize |= int(b&0x7f) << shift
		shift += 7
		if (b & 0x80) == 0 {
			break
		}
	}

	// Read target object size (variable-length LEB128)
	shift = 0
	for {
		if pos >= len(delta) {
			return 0, 0, 0, fmt.Errorf("%w: truncated target size header", ErrDeltaCorrupt)
		}
		b := delta[pos]
		pos++
		targetSize |= int(b&0x7f) << shift
		shift += 7
		if (b & 0x80) == 0 {
			break
		}
	}

	return baseSize, targetSize, pos, nil
}

// ApplyDelta applies git delta instructions from delta onto base, returning the reconstructed target object.
func ApplyDelta(base, delta []byte) ([]byte, error) {
	baseSize, targetSize, pos, err := ReadDeltaHeader(delta)
	if err != nil {
		return nil, err
	}

	if baseSize != len(base) {
		return nil, fmt.Errorf("%w: expected base size %d, got %d", ErrDeltaBaseMismatch, baseSize, len(base))
	}

	target := make([]byte, 0, targetSize)
	return applyDeltaInstructions(target, base, delta[pos:], targetSize)
}

// ApplyDeltaWithBuffer decodes the delta into a caller-supplied or pooled buffer.
func ApplyDeltaWithBuffer(targetBuf []byte, base, delta []byte) ([]byte, error) {
	baseSize, targetSize, pos, err := ReadDeltaHeader(delta)
	if err != nil {
		return nil, err
	}

	if baseSize != len(base) {
		return nil, fmt.Errorf("%w: expected base size %d, got %d", ErrDeltaBaseMismatch, baseSize, len(base))
	}

	if cap(targetBuf) < targetSize {
		targetBuf = make([]byte, 0, targetSize)
	} else {
		targetBuf = targetBuf[:0]
	}

	return applyDeltaInstructions(targetBuf, base, delta[pos:], targetSize)
}

func applyDeltaInstructions(target []byte, base, instructions []byte, targetSize int) ([]byte, error) {
	pos := 0
	instrLen := len(instructions)

	for pos < instrLen {
		cmd := instructions[pos]
		pos++

		if (cmd & 0x80) != 0 {
			// COPY instruction: copy range from base object
			var offset uint32
			var size uint32

			if (cmd & 0x01) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy offset byte 1", ErrDeltaCorrupt)
				}
				offset |= uint32(instructions[pos])
				pos++
			}
			if (cmd & 0x02) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy offset byte 2", ErrDeltaCorrupt)
				}
				offset |= uint32(instructions[pos]) << 8
				pos++
			}
			if (cmd & 0x04) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy offset byte 3", ErrDeltaCorrupt)
				}
				offset |= uint32(instructions[pos]) << 16
				pos++
			}
			if (cmd & 0x08) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy offset byte 4", ErrDeltaCorrupt)
				}
				offset |= uint32(instructions[pos]) << 24
				pos++
			}

			if (cmd & 0x10) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy size byte 1", ErrDeltaCorrupt)
				}
				size |= uint32(instructions[pos])
				pos++
			}
			if (cmd & 0x20) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy size byte 2", ErrDeltaCorrupt)
				}
				size |= uint32(instructions[pos]) << 8
				pos++
			}
			if (cmd & 0x40) != 0 {
				if pos >= instrLen {
					return nil, fmt.Errorf("%w: truncated copy size byte 3", ErrDeltaCorrupt)
				}
				size |= uint32(instructions[pos]) << 16
				pos++
			}

			if size == 0 {
				size = 0x10000 // 64KB per Git pack spec
			}

			end := int(offset + size)
			if int(offset) > len(base) || end > len(base) {
				return nil, fmt.Errorf("%w: copy out of bounds [offset=%d size=%d baseLen=%d]", ErrDeltaCorrupt, offset, size, len(base))
			}

			target = append(target, base[offset:end]...)
		} else if cmd > 0 {
			// INSERT instruction: literal bytes from delta stream
			size := int(cmd & 0x7f)
			if pos+size > instrLen {
				return nil, fmt.Errorf("%w: insert reaches beyond instruction data", ErrDeltaCorrupt)
			}
			target = append(target, instructions[pos:pos+size]...)
			pos += size
		} else {
			// Zero opcode is invalid/reserved in Git delta format
			return nil, fmt.Errorf("%w: invalid zero opcode in delta instructions", ErrDeltaCorrupt)
		}
	}

	if len(target) != targetSize {
		return nil, fmt.Errorf("%w: expected target size %d, decoded %d", ErrDeltaCorrupt, targetSize, len(target))
	}

	return target, nil
}
