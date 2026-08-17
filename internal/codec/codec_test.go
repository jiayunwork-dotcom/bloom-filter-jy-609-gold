package codec

import (
	"encoding/binary"
	"testing"

	"bloom-filter/internal/filter"
)

func TestMarshalRoundTrip(t *testing.T) {
	f, err := filter.New(4096, 7)
	if err != nil {
		t.Fatal(err)
	}
	f.Add([]byte("apple"))
	f.Add([]byte("banana"))

	enc := Marshal(f)
	back, err := Unmarshal(enc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.M() != f.M() || back.K() != f.K() {
		t.Errorf("M/K mismatch: %d/%d vs %d/%d", back.M(), back.K(), f.M(), f.K())
	}
	if !back.Test([]byte("apple")) || !back.Test([]byte("banana")) {
		t.Error("round-trip lost inserted items")
	}
	if back.Test([]byte("grape")) {
		t.Error("unexpected present after round-trip")
	}
}

func TestMarshalV2Format(t *testing.T) {
	f, _ := filter.New(1024, 3)
	f.Add([]byte("x"))
	enc := Marshal(f)
	ver, err := Version(enc)
	if err != nil {
		t.Fatal(err)
	}
	if ver != 2 {
		t.Errorf("Marshal should produce v2, got version %d", ver)
	}
}

func TestV1BackwardCompatibility(t *testing.T) {
	f, _ := filter.New(2048, 5)
	f.Add([]byte("hello"))
	f.Add([]byte("world"))
	v1 := MarshalV1(f)
	ver, err := Version(v1)
	if err != nil || ver != 1 {
		t.Fatalf("Version(v1)=%d, err=%v", ver, err)
	}
	back, err := Unmarshal(v1)
	if err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	if !back.Test([]byte("hello")) || !back.Test([]byte("world")) {
		t.Error("v1 round-trip lost items")
	}
}

func TestCRCMismatchDetected(t *testing.T) {
	f, _ := filter.New(1024, 3)
	f.Add([]byte("data"))
	enc := Marshal(f)
	// Flip a bit in the bits section
	enc[14] ^= 0xFF
	_, err := Unmarshal(enc)
	if err != ErrCRCMismatch {
		t.Errorf("expected ErrCRCMismatch, got %v", err)
	}
}

func TestCRCMismatchTrailerCorrupted(t *testing.T) {
	f, _ := filter.New(1024, 3)
	f.Add([]byte("item"))
	enc := Marshal(f)
	// Flip a bit in the CRC trailer itself
	enc[len(enc)-1] ^= 0x01
	_, err := Unmarshal(enc)
	if err != ErrCRCMismatch {
		t.Errorf("expected ErrCRCMismatch on trailer corruption, got %v", err)
	}
}

func TestCodecBitsSlicePreserved(t *testing.T) {
	f, _ := filter.New(1024, 3)
	f.Add([]byte("x"))
	enc := Marshal(f)
	back, err := Unmarshal(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Bits()) != len(f.Bits()) {
		t.Fatalf("bits len %d != %d", len(back.Bits()), len(f.Bits()))
	}
	for i := range f.Bits() {
		if back.Bits()[i] != f.Bits()[i] {
			t.Errorf("bits[%d] = %#x, want %#x", i, back.Bits()[i], f.Bits()[i])
		}
	}
}

func TestMarshalNil(t *testing.T) {
	if got := Marshal(nil); got != nil {
		t.Errorf("Marshal(nil) = %v, want nil", got)
	}
}

func TestUnmarshalTooShort(t *testing.T) {
	if _, err := Unmarshal([]byte{1, 2, 3}); err != ErrTooShort {
		t.Errorf("err = %v, want ErrTooShort", err)
	}
}

func TestUnmarshalNil(t *testing.T) {
	if _, err := Unmarshal(nil); err != ErrTooShort {
		t.Errorf("err = %v, want ErrTooShort", err)
	}
}

func TestUnmarshalBadMagic(t *testing.T) {
	b := make([]byte, 20)
	b[0] = 0xFF
	if _, err := Unmarshal(b); err != ErrBadMagic {
		t.Errorf("err = %v, want ErrBadMagic", err)
	}
}

func TestUnmarshalTruncatedBitsV1(t *testing.T) {
	b := make([]byte, headerSizeV1+4)
	binary.BigEndian.PutUint32(b[0:4], magicV1)
	binary.BigEndian.PutUint32(b[4:8], 4096) // needs 512 bytes
	binary.BigEndian.PutUint32(b[8:12], 7)
	if _, err := Unmarshal(b); err != ErrBitsTruncated {
		t.Errorf("err = %v, want ErrBitsTruncated", err)
	}
}

func TestUnmarshalTruncatedBitsV2(t *testing.T) {
	f, _ := filter.New(4096, 7)
	enc := Marshal(f)
	// Truncate: remove last 100 bytes
	truncated := enc[:len(enc)-100]
	_, err := Unmarshal(truncated)
	if err != ErrBitsTruncated {
		t.Errorf("err = %v, want ErrBitsTruncated", err)
	}
}

func TestUnmarshalZeroM(t *testing.T) {
	b := make([]byte, headerSizeV1)
	binary.BigEndian.PutUint32(b[0:4], magicV1)
	binary.BigEndian.PutUint32(b[4:8], 0) // m == 0
	binary.BigEndian.PutUint32(b[8:12], 7)
	if _, err := Unmarshal(b); err == nil {
		t.Error("expected error for m=0")
	}
}

func TestUnsupportedVersion(t *testing.T) {
	b := make([]byte, 20)
	binary.BigEndian.PutUint32(b[0:4], magicV2)
	b[4] = 99 // unsupported version
	_, err := Unmarshal(b)
	if err != ErrUnsupportedVersion {
		t.Errorf("err = %v, want ErrUnsupportedVersion", err)
	}
}

func TestVersionFunction(t *testing.T) {
	f, _ := filter.New(512, 3)
	v2 := Marshal(f)
	v1 := MarshalV1(f)

	if ver, err := Version(v2); err != nil || ver != 2 {
		t.Errorf("Version(v2) = %d, %v", ver, err)
	}
	if ver, err := Version(v1); err != nil || ver != 1 {
		t.Errorf("Version(v1) = %d, %v", ver, err)
	}
	if _, err := Version(nil); err != ErrTooShort {
		t.Errorf("Version(nil) err = %v, want ErrTooShort", err)
	}
	if _, err := Version([]byte{0xFF, 0xFF, 0xFF, 0xFF}); err != ErrBadMagic {
		t.Errorf("Version(bad) err = %v, want ErrBadMagic", err)
	}
}
