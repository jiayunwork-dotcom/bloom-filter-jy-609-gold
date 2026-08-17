// Package store provides append-only file storage for a BloomFilter with
// checkpoint and replay semantics. The file format supports crash recovery:
// incomplete trailing records are discarded on open.
//
// File layout:
//
//	[FileHeader]
//	[Record]*
//
// FileHeader (fixed 8 bytes):
//
//	[4] magic 0x424C5301 ("BLS\x01")
//	[4] filterM (uint32 big-endian) — bit count of the filter
//
// Record:
//
//	[1] type (0x01=Add, 0x02=Checkpoint)
//	[4] payload length (uint32 big-endian)
//	[N] payload
//	[4] CRC32 (IEEE, over type+length+payload)
//
// Add record payload: raw bytes of the added element.
// Checkpoint record payload: codec.Marshal(filter) — a full v2 snapshot.
//
// On Open, the store replays from the last valid Checkpoint forward.
// Truncated/corrupt trailing records are silently discarded (crash recovery).
package store

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"

	"bloom-filter/internal/codec"
	"bloom-filter/internal/filter"
)

const (
	fileMagic  uint32 = 0x424C5301 // "BLS\x01"
	headerSize        = 8          // magic(4) + filterM(4)

	recAdd        byte = 0x01
	recCheckpoint byte = 0x02

	recHeaderSize = 5 // type(1) + length(4)
	recCRCSize    = 4
)

var (
	// ErrNotStore is returned when the file does not have the expected magic.
	ErrNotStore = errors.New("store: not a bloom-filter store file")
	// ErrCorruptHeader is returned when the file header is too short.
	ErrCorruptHeader = errors.New("store: corrupt file header")
)

// Store manages an append-only bloom filter file.
type Store struct {
	path   string
	f      *os.File
	filter *filter.BloomFilter
	k      uint
}

// Create initializes a new store file at path with a fresh filter of m bits and k hashes.
// If the file already exists it is truncated.
func Create(path string, m, k uint) (*Store, error) {
	bf, err := filter.New(m, k)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	// Write header
	hdr := make([]byte, headerSize)
	binary.BigEndian.PutUint32(hdr[0:4], fileMagic)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(m))
	if _, err := f.Write(hdr); err != nil {
		f.Close()
		return nil, err
	}
	return &Store{path: path, f: f, filter: bf, k: k}, nil
}

// Open replays an existing store file from disk. Silently discards any
// incomplete trailing record (crash recovery). Returns ErrNotStore if the
// file magic does not match.
func Open(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < headerSize {
		return nil, ErrCorruptHeader
	}
	if binary.BigEndian.Uint32(data[0:4]) != fileMagic {
		return nil, ErrNotStore
	}
	m := uint(binary.BigEndian.Uint32(data[4:8]))

	// Replay records
	var bf *filter.BloomFilter
	var k uint
	pos := headerSize
	lastCheckpoint := -1

	// First pass: find last valid checkpoint
	p := headerSize
	for {
		rec, size, ok := parseRecord(data[p:])
		if !ok {
			break
		}
		if rec.typ == recCheckpoint {
			lastCheckpoint = p
		}
		p += size
	}

	// Start replay from last checkpoint or beginning
	if lastCheckpoint >= 0 {
		pos = lastCheckpoint
	}

	for {
		rec, size, ok := parseRecord(data[pos:])
		if !ok {
			break
		}
		switch rec.typ {
		case recCheckpoint:
			restored, err := codec.Unmarshal(rec.payload)
			if err != nil {
				// Skip corrupt checkpoint, keep going
				pos += size
				continue
			}
			bf = restored
			k = bf.K()
		case recAdd:
			if bf == nil {
				// No checkpoint yet; create fresh filter
				bf, _ = filter.New(m, 7)
				k = 7
			}
			bf.Add(rec.payload)
		}
		pos += size
	}

	if bf == nil {
		bf, _ = filter.New(m, 7)
		k = 7
	}

	// Re-open file for appending (seek to valid end)
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Truncate any partial trailing record
	if _, err := f.Seek(int64(pos), io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Truncate(int64(pos)); err != nil {
		f.Close()
		return nil, err
	}

	return &Store{path: path, f: f, filter: bf, k: k}, nil
}

// Add appends an element to the filter and writes an Add record to the file.
func (s *Store) Add(item []byte) error {
	s.filter.Add(item)
	return s.writeRecord(recAdd, item)
}

// Test checks if an element may be in the filter.
func (s *Store) Test(item []byte) bool {
	return s.filter.Test(item)
}

// Checkpoint writes a full filter snapshot to the file. After a checkpoint,
// replay on next Open will start from this point, making earlier Add records
// unnecessary (but harmless).
func (s *Store) Checkpoint() error {
	data := codec.Marshal(s.filter)
	return s.writeRecord(recCheckpoint, data)
}

// Filter returns the underlying BloomFilter (read-only view).
func (s *Store) Filter() *filter.BloomFilter {
	return s.filter
}

// Close flushes and closes the store file.
func (s *Store) Close() error {
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

func (s *Store) writeRecord(typ byte, payload []byte) error {
	rec := encodeRecord(typ, payload)
	_, err := s.f.Write(rec)
	return err
}

func encodeRecord(typ byte, payload []byte) []byte {
	buf := make([]byte, recHeaderSize+len(payload)+recCRCSize)
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	checksum := crc32.ChecksumIEEE(buf[:recHeaderSize+len(payload)])
	binary.BigEndian.PutUint32(buf[recHeaderSize+len(payload):], checksum)
	return buf
}

type record struct {
	typ     byte
	payload []byte
}

// parseRecord attempts to parse one record from data. Returns the record,
// total bytes consumed, and whether parsing succeeded. Returns ok=false
// if data is too short or CRC does not match (truncation/corruption).
func parseRecord(data []byte) (record, int, bool) {
	if len(data) < recHeaderSize {
		return record{}, 0, false
	}
	typ := data[0]
	if typ != recAdd && typ != recCheckpoint {
		return record{}, 0, false
	}
	pLen := int(binary.BigEndian.Uint32(data[1:5]))
	total := recHeaderSize + pLen + recCRCSize
	if len(data) < total {
		return record{}, 0, false
	}
	// Validate CRC
	stored := binary.BigEndian.Uint32(data[recHeaderSize+pLen : total])
	computed := crc32.ChecksumIEEE(data[:recHeaderSize+pLen])
	if stored != computed {
		return record{}, 0, false
	}
	payload := make([]byte, pLen)
	copy(payload, data[recHeaderSize:recHeaderSize+pLen])
	return record{typ: typ, payload: payload}, total, true
}
