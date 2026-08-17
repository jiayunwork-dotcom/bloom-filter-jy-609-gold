package filter

import (
	"errors"
	"math"

	"bloom-filter/internal/hash"
)

// ErrInvalidParams is returned by New/NewFromParts when m or k is zero.
var ErrInvalidParams = errors.New("filter: m and k must be > 0")

// BloomFilter is a space-efficient probabilistic set backed by a bit array.
type BloomFilter struct {
	m    uint
	k    uint
	bits []byte
}

// New creates a BloomFilter with m bits and k hash functions.
// It returns ErrInvalidParams if m == 0 or k == 0.
func New(m, k uint) (*BloomFilter, error) {
	if m == 0 || k == 0 {
		return nil, ErrInvalidParams
	}
	return &BloomFilter{m: m, k: k, bits: make([]byte, (m+7)/8)}, nil
}

// NewFromParts reconstructs a filter from its raw parts (used by codec).
func NewFromParts(m, k uint, bits []byte) (*BloomFilter, error) {
	if m == 0 || k == 0 {
		return nil, ErrInvalidParams
	}
	need := (m + 7) / 8
	if uint(len(bits)) != need {
		return nil, errors.New("filter: bits length mismatch")
	}
	cp := make([]byte, len(bits))
	copy(cp, bits)
	return &BloomFilter{m: m, k: k, bits: cp}, nil
}

// M returns the number of bits in the filter.
func (f *BloomFilter) M() uint { return f.m }

// K returns the number of hash functions.
func (f *BloomFilter) K() uint { return f.k }

// Bits returns a copy-free view of the underlying bit array.
func (f *BloomFilter) Bits() []byte { return f.bits }

// Add inserts data into the filter.
func (f *BloomFilter) Add(data []byte) {
	for i := uint(0); i < f.k; i++ {
		f.set(hash.Hash(data, i, f.m))
	}
}

// Test reports whether data is possibly present. It returns true when data may
// have been added (possible false positive) and false when data is definitely
// absent.
func (f *BloomFilter) Test(data []byte) bool {
	for i := uint(0); i < f.k; i++ {
		if !f.get(hash.Hash(data, i, f.m)) {
			return false
		}
	}
	return true
}

// FalsePositiveRate returns the approximate false-positive rate after n
// insertions: (1 - exp(-k*n/m))^k.
func (f *BloomFilter) FalsePositiveRate(n int) float64 {
	if f.m == 0 {
		return 1
	}
	exponent := -float64(f.k) * float64(n) / float64(f.m)
	return math.Pow(1-math.Exp(exponent), float64(f.k))
}

// MaxInsertions returns the maximum number of elements that can be inserted
// while keeping the false positive rate at or below maxFPR.
// Returns 0 if maxFPR is out of (0,1) range or if the filter cannot hold any
// elements within the constraint.
func (f *BloomFilter) MaxInsertions(maxFPR float64) int {
	if maxFPR <= 0 || maxFPR >= 1 || f.m == 0 || f.k == 0 {
		return 0
	}
	// FPR(n) = (1 - exp(-k*n/m))^k <= maxFPR
	// => 1 - exp(-k*n/m) <= maxFPR^(1/k)
	// => exp(-k*n/m) >= 1 - maxFPR^(1/k)
	// => -k*n/m >= ln(1 - maxFPR^(1/k))
	// => n <= -m * ln(1 - maxFPR^(1/k)) / k
	inner := 1 - math.Pow(maxFPR, 1.0/float64(f.k))
	if inner <= 0 {
		return 0
	}
	n := -float64(f.m) * math.Log(inner) / float64(f.k)
	if n < 0 {
		return 0
	}
	return int(n)
}

// --- Capacity planning helpers ---

// OptimalK computes the optimal number of hash functions for a filter with
// m bits expecting n insertions: k = (m/n) * ln(2).
// Returns at least 1.
func OptimalK(m, n uint) uint {
	if n == 0 || m == 0 {
		return 1
	}
	k := float64(m) / float64(n) * math.Ln2
	ki := uint(math.Round(k))
	if ki < 1 {
		ki = 1
	}
	return ki
}

// RequiredBits computes the minimum number of bits needed for n insertions
// at the given target false positive rate: m = -n * ln(fpr) / (ln2)^2.
// Adds a small margin (rounds up by 1 extra bit per 64) to account for
// rounding in OptimalK. Returns at least 8 (1 byte). fpr must be in (0,1).
func RequiredBits(n uint, fpr float64) uint {
	if n == 0 || fpr <= 0 || fpr >= 1 {
		return 8
	}
	m := -float64(n) * math.Log(fpr) / (math.Ln2 * math.Ln2)
	// Add ~2% margin to compensate for discrete k rounding
	m *= 1.02
	mi := uint(math.Ceil(m))
	if mi < 8 {
		mi = 8
	}
	return mi
}

// NewFromCapacity creates a BloomFilter sized for expectedN insertions with
// false positive rate at most maxFPR. Automatically computes optimal m and k.
// Returns ErrInvalidParams if expectedN == 0 or maxFPR is not in (0,1).
func NewFromCapacity(expectedN uint, maxFPR float64) (*BloomFilter, error) {
	if expectedN == 0 || maxFPR <= 0 || maxFPR >= 1 {
		return nil, ErrInvalidParams
	}
	m := RequiredBits(expectedN, maxFPR)
	k := OptimalK(m, expectedN)
	return New(m, k)
}

func (f *BloomFilter) set(idx uint) {
	bi := idx / 8
	if bi >= uint(len(f.bits)) {
		return
	}
	f.bits[bi] |= 1 << (idx % 8)
}

func (f *BloomFilter) get(idx uint) bool {
	bi := idx / 8
	if bi >= uint(len(f.bits)) {
		return false
	}
	return f.bits[bi]&(1<<(idx%8)) != 0
}
