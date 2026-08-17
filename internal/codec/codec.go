// Package codec provides binary serialization of BloomFilter with versioning and
// integrity checking. Supports both v1 (legacy) and v2 (current) wire formats.
package codec

import (
	"encoding/binary"
	"errors"
	"hash/crc32"

	"bloom-filter/internal/filter"
)

var (
	// ErrTooShort is returned when the input is too short to be a valid encoding.
	ErrTooShort = errors.New("codec: input too short")
	// ErrBadMagic is returned when the leading magic bytes do not match any known version.
	ErrBadMagic = errors.New("codec: bad magic")
	// ErrUnsupportedVersion is returned when the version byte is not recognized.
	ErrUnsupportedVersion = errors.New("codec: unsupported version")
	// ErrCRCMismatch is returned when the trailing CRC32 does not match the computed value.
	ErrCRCMismatch = errors.New("codec: CRC mismatch (data corrupted)")
	// ErrBitsTruncated is returned when the bits section length does not match m.
	ErrBitsTruncated = errors.New("codec: bits section truncated")
)

const (
	magicV1 uint32 = 0x424C4D01 // "BLM" + version byte 0x01
	magicV2 uint32 = 0x424C4D02 // "BLM" + version byte 0x02

	headerSizeV1 = 12 // magic(4) + m(4) + k(4)
	headerSizeV2 = 13 // magic(4) + version(1) + m(4) + k(4)
	trailerV2    = 4  // CRC32(4)
)

// Marshal serializes a BloomFilter to the v2 wire format:
//
//	[4] magic (0x424C4D02)
//	[1] version (2)
//	[4] m (uint32 big-endian)
//	[4] k (uint32 big-endian)
//	[N] bits
//	[4] CRC32 (IEEE, over bytes [0..headerSizeV2+N-1])
//
// Returns nil when f is nil.
func Marshal(f *filter.BloomFilter) []byte {
	if f == nil {
		return nil
	}
	bits := f.Bits()
	total := headerSizeV2 + len(bits) + trailerV2
	out := make([]byte, total)

	binary.BigEndian.PutUint32(out[0:4], magicV2)
	out[4] = 2 // version
	binary.BigEndian.PutUint32(out[5:9], uint32(f.M()))
	binary.BigEndian.PutUint32(out[9:13], uint32(f.K()))
	copy(out[13:], bits)

	// CRC32 over everything except the trailing 4 bytes
	checksum := crc32.ChecksumIEEE(out[:total-trailerV2])
	binary.BigEndian.PutUint32(out[total-trailerV2:], checksum)
	return out
}

// MarshalV1 serializes a BloomFilter to the legacy v1 wire format (no CRC):
//
//	[4] magic (0x424C4D01)
//	[4] m (uint32 big-endian)
//	[4] k (uint32 big-endian)
//	[N] bits
//
// Used for testing backward compatibility. Returns nil when f is nil.
func MarshalV1(f *filter.BloomFilter) []byte {
	if f == nil {
		return nil
	}
	bits := f.Bits()
	out := make([]byte, headerSizeV1+len(bits))
	binary.BigEndian.PutUint32(out[0:4], magicV1)
	binary.BigEndian.PutUint32(out[4:8], uint32(f.M()))
	binary.BigEndian.PutUint32(out[8:12], uint32(f.K()))
	copy(out[12:], bits)
	return out
}

// Unmarshal reconstructs a BloomFilter from bytes produced by Marshal or MarshalV1.
// Automatically detects version from the magic bytes.
//   - v1: no CRC check (legacy compatibility)
//   - v2: validates CRC32 integrity
//
// Returns appropriate error for short/corrupted/unknown-version data.
func Unmarshal(b []byte) (*filter.BloomFilter, error) {
	if len(b) < 4 {
		return nil, ErrTooShort
	}
	magic := binary.BigEndian.Uint32(b[0:4])
	switch magic {
	case magicV1:
		return unmarshalV1(b)
	case magicV2:
		return unmarshalV2(b)
	default:
		return nil, ErrBadMagic
	}
}

// Version returns the format version of an encoded blob (1 or 2), or an error.
func Version(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, ErrTooShort
	}
	magic := binary.BigEndian.Uint32(b[0:4])
	switch magic {
	case magicV1:
		return 1, nil
	case magicV2:
		return 2, nil
	default:
		return 0, ErrBadMagic
	}
}

func unmarshalV1(b []byte) (*filter.BloomFilter, error) {
	if len(b) < headerSizeV1 {
		return nil, ErrTooShort
	}
	m := uint(binary.BigEndian.Uint32(b[4:8]))
	k := uint(binary.BigEndian.Uint32(b[8:12]))
	bits := b[headerSizeV1:]
	need := (m + 7) / 8
	if uint(len(bits)) != need {
		return nil, ErrBitsTruncated
	}
	return filter.NewFromParts(m, k, bits)
}

func unmarshalV2(b []byte) (*filter.BloomFilter, error) {
	if len(b) < headerSizeV2+trailerV2 {
		return nil, ErrTooShort
	}
	ver := b[4]
	if ver != 2 {
		return nil, ErrUnsupportedVersion
	}
	m := uint(binary.BigEndian.Uint32(b[5:9]))
	k := uint(binary.BigEndian.Uint32(b[9:13]))
	need := (m + 7) / 8
	bitsEnd := headerSizeV2 + int(need)
	if len(b) < bitsEnd+trailerV2 {
		return nil, ErrBitsTruncated
	}

	// Validate CRC32
	payload := b[:bitsEnd]
	stored := binary.BigEndian.Uint32(b[bitsEnd : bitsEnd+trailerV2])
	computed := crc32.ChecksumIEEE(payload)
	if stored != computed {
		return nil, ErrCRCMismatch
	}

	bits := b[headerSizeV2:bitsEnd]
	return filter.NewFromParts(m, k, bits)
}
