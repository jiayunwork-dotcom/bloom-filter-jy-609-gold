package store

import (
	"os"
	"path/filepath"
	"testing"

	"bloom-filter/internal/codec"
	"bloom-filter/internal/filter"
)

// TestStoreCheckpointMatchesInMemory verifies that after a checkpoint,
// re-opening and reading the filter produces bit-identical state to the
// in-memory filter at checkpoint time. This is a cross-package invariant
// (store -> codec -> filter round-trip).
func TestStoreCheckpointMatchesInMemory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "contract.blm")
	s, err := Create(p, 8192, 5)
	if err != nil {
		t.Fatal(err)
	}
	items := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, it := range items {
		s.Add([]byte(it))
	}
	// Snapshot the in-memory state
	memBits := make([]byte, len(s.Filter().Bits()))
	copy(memBits, s.Filter().Bits())
	memM := s.Filter().M()
	memK := s.Filter().K()

	if err := s.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen and compare
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	if s2.Filter().M() != memM || s2.Filter().K() != memK {
		t.Errorf("M/K mismatch after checkpoint reopen: %d/%d vs %d/%d",
			s2.Filter().M(), s2.Filter().K(), memM, memK)
	}
	for i, b := range memBits {
		if s2.Filter().Bits()[i] != b {
			t.Errorf("bits[%d] differs: got %#x want %#x", i, s2.Filter().Bits()[i], b)
			break
		}
	}
	// Verify all items still present
	for _, it := range items {
		if !s2.Test([]byte(it)) {
			t.Errorf("item %q lost after checkpoint+reopen", it)
		}
	}
}

// TestStoreAppendThenReopenRoundTrip verifies that elements added after
// a checkpoint survive re-open (replay of post-checkpoint Add records).
// The bits after re-open must match the in-memory state at close time.
func TestStoreAppendThenReopenRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "append.blm")
	s, _ := Create(p, 4096, 7)
	s.Add([]byte("pre-checkpoint"))
	s.Checkpoint()
	s.Add([]byte("post-checkpoint-1"))
	s.Add([]byte("post-checkpoint-2"))

	// Capture final in-memory bits
	finalBits := make([]byte, len(s.Filter().Bits()))
	copy(finalBits, s.Filter().Bits())
	s.Close()

	s2, _ := Open(p)
	defer s2.Close()
	for i, b := range finalBits {
		if s2.Filter().Bits()[i] != b {
			t.Errorf("bits[%d] mismatch after append+reopen: %#x vs %#x", i, s2.Filter().Bits()[i], b)
			break
		}
	}
	if !s2.Test([]byte("post-checkpoint-1")) || !s2.Test([]byte("post-checkpoint-2")) {
		t.Error("post-checkpoint items lost")
	}
}

// TestStoreTruncatedTailDoesNotPollutePriorCheckpoint is the HIGH-DIFFICULTY
// test surface. It verifies that a corrupt/truncated trailing record after
// a valid checkpoint does not pollute the committed prefix: the filter state
// after recovery must match exactly the state at the last valid checkpoint
// plus any fully-written Add records between checkpoint and corruption.
func TestStoreTruncatedTailDoesNotPollutePriorCheckpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "truncate.blm")
	s, _ := Create(p, 4096, 7)
	s.Add([]byte("committed-1"))
	s.Add([]byte("committed-2"))
	s.Checkpoint()
	s.Add([]byte("committed-3")) // valid add after checkpoint
	s.Close()

	// Capture expected state: checkpoint + committed-3
	s2, _ := Open(p)
	expectedBits := make([]byte, len(s2.Filter().Bits()))
	copy(expectedBits, s2.Filter().Bits())
	s2.Close()

	// Append garbage that looks like a partial record (simulates crash mid-write)
	f, _ := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0o644)
	// Write a partial Add record: type=0x01, length=100, but only 5 bytes of payload (no CRC)
	f.Write([]byte{0x01, 0x00, 0x00, 0x00, 0x64, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE})
	f.Close()

	// Reopen: partial record should be discarded
	s3, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()

	// Verify state matches pre-corruption state exactly
	for i, b := range expectedBits {
		if s3.Filter().Bits()[i] != b {
			t.Errorf("bits[%d] polluted: got %#x want %#x", i, s3.Filter().Bits()[i], b)
			break
		}
	}
	if !s3.Test([]byte("committed-1")) || !s3.Test([]byte("committed-2")) || !s3.Test([]byte("committed-3")) {
		t.Error("committed items lost after truncated tail recovery")
	}
}

// TestV1ToV2MigrationRoundTrip verifies that a filter serialized in v1 format
// can be loaded, re-serialized in v2 format, and the resulting filter is
// bit-identical (codec version migration does not alter filter state).
func TestV1ToV2MigrationRoundTrip(t *testing.T) {
	f, _ := filter.New(2048, 5)
	f.Add([]byte("migrate-me"))
	f.Add([]byte("and-me"))

	v1Data := codec.MarshalV1(f)
	restored, err := codec.Unmarshal(v1Data)
	if err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	// Re-marshal as v2
	v2Data := codec.Marshal(restored)
	ver, _ := codec.Version(v2Data)
	if ver != 2 {
		t.Fatalf("re-marshal should produce v2, got %d", ver)
	}
	// Unmarshal v2 and compare bits
	final, err := codec.Unmarshal(v2Data)
	if err != nil {
		t.Fatalf("unmarshal v2: %v", err)
	}
	if final.M() != f.M() || final.K() != f.K() {
		t.Errorf("M/K changed: %d/%d vs %d/%d", final.M(), final.K(), f.M(), f.K())
	}
	for i := range f.Bits() {
		if final.Bits()[i] != f.Bits()[i] {
			t.Errorf("bits[%d] changed after v1->v2 migration: %#x vs %#x",
				i, final.Bits()[i], f.Bits()[i])
			break
		}
	}
}

// TestCapacityFPRInvariant verifies that NewFromCapacity produces a filter
// whose FalsePositiveRate at the expected N does not exceed the requested maxFPR.
// Also verifies MaxInsertions is consistent with FalsePositiveRate.
func TestCapacityFPRInvariant(t *testing.T) {
	cases := []struct {
		n      uint
		maxFPR float64
	}{
		{100, 0.01},
		{1000, 0.001},
		{500, 0.05},
	}
	for _, tc := range cases {
		f, err := filter.NewFromCapacity(tc.n, tc.maxFPR)
		if err != nil {
			t.Fatalf("NewFromCapacity(%d, %v): %v", tc.n, tc.maxFPR, err)
		}
		// FPR at expected N should be <= maxFPR
		fpr := f.FalsePositiveRate(int(tc.n))
		if fpr > tc.maxFPR {
			t.Errorf("NewFromCapacity(%d, %v): FPR at N = %v > maxFPR %v",
				tc.n, tc.maxFPR, fpr, tc.maxFPR)
		}
		// MaxInsertions should be >= n
		maxN := f.MaxInsertions(tc.maxFPR)
		if maxN < int(tc.n) {
			t.Errorf("MaxInsertions(%v) = %d, want >= %d", tc.maxFPR, maxN, tc.n)
		}
	}
}

// TestOptimalKConsistency verifies that OptimalK produces a k value that
// minimizes FPR (or is very close to minimum) for given m and n.
func TestOptimalKConsistency(t *testing.T) {
	m := uint(10000)
	n := uint(500)
	optK := filter.OptimalK(m, n)

	f, _ := filter.New(m, optK)
	fprOpt := f.FalsePositiveRate(int(n))

	// Try k-1 and k+1; optimal should be no worse
	if optK > 1 {
		f2, _ := filter.New(m, optK-1)
		fpr2 := f2.FalsePositiveRate(int(n))
		if fpr2 < fprOpt*0.99 { // allow 1% tolerance due to rounding
			t.Errorf("k=%d gives FPR %v but k-1=%d gives better FPR %v",
				optK, fprOpt, optK-1, fpr2)
		}
	}
	f3, _ := filter.New(m, optK+1)
	fpr3 := f3.FalsePositiveRate(int(n))
	if fpr3 < fprOpt*0.99 {
		t.Errorf("k=%d gives FPR %v but k+1=%d gives better FPR %v",
			optK, fprOpt, optK+1, fpr3)
	}
}
