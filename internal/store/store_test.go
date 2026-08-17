package store

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.blm")
}

func TestCreateAndReopen(t *testing.T) {
	p := tmpPath(t)
	s, err := Create(p, 4096, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add([]byte("world")); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.Test([]byte("hello")) {
		t.Error("hello should be present after reopen")
	}
	if !s2.Test([]byte("world")) {
		t.Error("world should be present after reopen")
	}
	if s2.Test([]byte("missing")) {
		t.Error("missing should be absent")
	}
}

func TestCheckpointAndReplay(t *testing.T) {
	p := tmpPath(t)
	s, err := Create(p, 4096, 7)
	if err != nil {
		t.Fatal(err)
	}
	s.Add([]byte("a"))
	s.Add([]byte("b"))
	if err := s.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	s.Add([]byte("c"))
	s.Close()

	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.Test([]byte("a")) || !s2.Test([]byte("b")) || !s2.Test([]byte("c")) {
		t.Error("all items should survive checkpoint + replay")
	}
}

func TestTruncatedRecordRecovery(t *testing.T) {
	p := tmpPath(t)
	s, err := Create(p, 4096, 7)
	if err != nil {
		t.Fatal(err)
	}
	s.Add([]byte("good"))
	s.Close()

	// Append partial/corrupt bytes to simulate crash
	f, _ := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0o644)
	f.Write([]byte{0x01, 0x00, 0x00, 0x00, 0x05}) // type=Add, len=5, but no payload/CRC
	f.Close()

	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if !s2.Test([]byte("good")) {
		t.Error("good item should survive truncated trailing record")
	}
}

func TestCorruptRecordCRCSkipped(t *testing.T) {
	p := tmpPath(t)
	s, err := Create(p, 4096, 7)
	if err != nil {
		t.Fatal(err)
	}
	s.Add([]byte("before"))
	s.Close()

	// Read file, corrupt the CRC of the last record
	data, _ := os.ReadFile(p)
	// Flip last byte (part of CRC)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(p, data, 0o644)

	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	// The corrupt record is discarded; filter is empty (no valid adds)
	if s2.Test([]byte("before")) {
		t.Error("item from corrupt record should not be present")
	}
}

func TestOpenNonExistent(t *testing.T) {
	_, err := Open("/nonexistent/path/file.blm")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestOpenBadMagic(t *testing.T) {
	p := tmpPath(t)
	os.WriteFile(p, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0}, 0o644)
	_, err := Open(p)
	if err != ErrNotStore {
		t.Errorf("expected ErrNotStore, got %v", err)
	}
}

func TestCheckpointThenAddAfterReopen(t *testing.T) {
	p := tmpPath(t)
	s, _ := Create(p, 8192, 5)
	s.Add([]byte("x"))
	s.Checkpoint()
	s.Close()

	s2, _ := Open(p)
	s2.Add([]byte("y"))
	s2.Close()

	s3, _ := Open(p)
	defer s3.Close()
	if !s3.Test([]byte("x")) || !s3.Test([]byte("y")) {
		t.Error("both items should persist across checkpoint + reopen + add")
	}
}

func TestMultipleCheckpoints(t *testing.T) {
	p := tmpPath(t)
	s, _ := Create(p, 4096, 7)
	s.Add([]byte("one"))
	s.Checkpoint()
	s.Add([]byte("two"))
	s.Checkpoint()
	s.Add([]byte("three"))
	s.Close()

	s2, _ := Open(p)
	defer s2.Close()
	if !s2.Test([]byte("one")) || !s2.Test([]byte("two")) || !s2.Test([]byte("three")) {
		t.Error("all items should survive multiple checkpoints")
	}
}
