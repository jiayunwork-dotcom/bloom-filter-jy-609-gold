// Package capacity provides capacity planning, saturation monitoring, and
// parameter recommendation for Bloom filters. It answers questions like:
//   - What parameters (m, k) should I use for N items at target FPR?
//   - How full is my current filter?
//   - When should I rotate/expand?
//   - What is the remaining safe capacity?
package capacity

import (
	"errors"
	"fmt"
	"math"

	"bloom-filter/internal/filter"
)

// --- Errors ---

var (
	// ErrInvalidFPR is returned when the target FPR is not in (0,1).
	ErrInvalidFPR = errors.New("capacity: FPR must be in (0, 1)")
	// ErrInvalidCount is returned when expectedN is zero.
	ErrInvalidCount = errors.New("capacity: expected count must be > 0")
	// ErrNilFilter is returned when a nil filter is passed.
	ErrNilFilter = errors.New("capacity: nil filter")
)

// --- Recommendation ---

// Params holds recommended Bloom filter parameters.
type Params struct {
	M           uint    // number of bits
	K           uint    // number of hash functions
	Bytes       uint    // storage in bytes (m+7)/8
	ExpectedN   uint    // designed capacity
	TargetFPR   float64 // target false positive rate
	ActualFPR   float64 // FPR at ExpectedN with chosen m and k
	BitsPerItem float64 // m / expectedN
}

// Recommend computes optimal Bloom filter parameters for the given expected
// number of insertions and target false positive rate.
func Recommend(expectedN uint, targetFPR float64) (Params, error) {
	if expectedN == 0 {
		return Params{}, ErrInvalidCount
	}
	if targetFPR <= 0 || targetFPR >= 1 {
		return Params{}, ErrInvalidFPR
	}
	m := filter.RequiredBits(expectedN, targetFPR)
	k := filter.OptimalK(m, expectedN)
	f, _ := filter.New(m, k)
	actualFPR := f.FalsePositiveRate(int(expectedN))
	return Params{
		M:           m,
		K:           k,
		Bytes:       (m + 7) / 8,
		ExpectedN:   expectedN,
		TargetFPR:   targetFPR,
		ActualFPR:   actualFPR,
		BitsPerItem: float64(m) / float64(expectedN),
	}, nil
}

// --- Saturation monitoring ---

// Status represents the health status of a Bloom filter.
type Status int

const (
	StatusHealthy  Status = iota // well within capacity
	StatusWarning                // approaching capacity
	StatusCritical               // at or beyond safe capacity
)

// String returns a human-readable status label.
func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusWarning:
		return "warning"
	case StatusCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Report holds the result of a saturation check.
type Report struct {
	// Filter parameters
	M uint
	K uint

	// Current state
	InsertedN    int     // number of items inserted
	CurrentFPR   float64 // estimated FPR at current N
	Saturation   float64 // fraction of bits set (0.0 to 1.0)
	BitsSet      int     // count of set bits
	BitsTotal    int     // total bits (m)

	// Capacity info
	MaxN         int     // max insertions at warning threshold FPR
	RemainingN   int     // how many more items before warning
	WarningFPR   float64 // threshold FPR for warning status
	CriticalFPR  float64 // threshold FPR for critical status

	// Overall health
	Status       Status
	StatusLabel  string
}

// MonitorConfig controls the thresholds for saturation monitoring.
type MonitorConfig struct {
	WarningFPR  float64 // FPR threshold for warning (default 0.05)
	CriticalFPR float64 // FPR threshold for critical (default 0.10)
}

// DefaultMonitorConfig returns sensible default thresholds.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		WarningFPR:  0.05,
		CriticalFPR: 0.10,
	}
}

// Monitor checks the saturation of a filter given the number of items inserted.
// It returns a Report with health status and remaining capacity.
func Monitor(f *filter.BloomFilter, insertedN int, cfg MonitorConfig) (Report, error) {
	if f == nil {
		return Report{}, ErrNilFilter
	}
	if cfg.WarningFPR <= 0 || cfg.WarningFPR >= 1 {
		return Report{}, ErrInvalidFPR
	}
	if cfg.CriticalFPR <= 0 || cfg.CriticalFPR >= 1 {
		return Report{}, ErrInvalidFPR
	}

	bitsSet := countBitsSet(f.Bits())
	saturation := float64(bitsSet) / float64(f.M())
	currentFPR := f.FalsePositiveRate(insertedN)
	maxN := f.MaxInsertions(cfg.WarningFPR)
	remaining := maxN - insertedN
	if remaining < 0 {
		remaining = 0
	}

	status := StatusHealthy
	if currentFPR >= cfg.CriticalFPR {
		status = StatusCritical
	} else if currentFPR >= cfg.WarningFPR {
		status = StatusWarning
	}

	return Report{
		M:           f.M(),
		K:           f.K(),
		InsertedN:   insertedN,
		CurrentFPR:  currentFPR,
		Saturation:  saturation,
		BitsSet:     bitsSet,
		BitsTotal:   int(f.M()),
		MaxN:        maxN,
		RemainingN:  remaining,
		WarningFPR:  cfg.WarningFPR,
		CriticalFPR: cfg.CriticalFPR,
		Status:      status,
		StatusLabel: status.String(),
	}, nil
}

// --- Scaling advisor ---

// ScaleAdvice holds recommendations for scaling a full/near-full filter.
type ScaleAdvice struct {
	CurrentM      uint
	CurrentK      uint
	CurrentN      int
	CurrentFPR    float64
	ShouldScale   bool
	Reason        string
	NewParams     Params  // recommended new parameters (if ShouldScale)
	GrowthFactor  float64 // newM / currentM
}

// Advise checks whether the filter should be scaled up and provides new
// parameters if so. It targets doubling the capacity while maintaining
// the same FPR as the current load would ideally have.
func Advise(f *filter.BloomFilter, insertedN int, targetFPR float64) (ScaleAdvice, error) {
	if f == nil {
		return ScaleAdvice{}, ErrNilFilter
	}
	if targetFPR <= 0 || targetFPR >= 1 {
		return ScaleAdvice{}, ErrInvalidFPR
	}

	currentFPR := f.FalsePositiveRate(insertedN)
	maxN := f.MaxInsertions(targetFPR)

	advice := ScaleAdvice{
		CurrentM:   f.M(),
		CurrentK:   f.K(),
		CurrentN:   insertedN,
		CurrentFPR: currentFPR,
	}

	// If we're at or beyond 80% of max capacity, recommend scaling
	threshold := int(float64(maxN) * 0.8)
	if insertedN >= threshold && insertedN > 0 {
		advice.ShouldScale = true
		advice.Reason = fmt.Sprintf("usage at %d/%d (%.0f%% of max capacity at FPR %.4f)",
			insertedN, maxN, float64(insertedN)/float64(maxN)*100, targetFPR)

		// Recommend new filter with 2x capacity
		newN := uint(insertedN) * 2
		if newN < 100 {
			newN = 100
		}
		newParams, err := Recommend(newN, targetFPR)
		if err != nil {
			return advice, nil
		}
		advice.NewParams = newParams
		advice.GrowthFactor = float64(newParams.M) / float64(f.M())
	} else {
		advice.ShouldScale = false
		advice.Reason = fmt.Sprintf("usage at %d/%d (%.0f%% of max capacity)",
			insertedN, maxN, float64(insertedN)/float64(maxN)*100)
	}

	return advice, nil
}

// --- Utility ---

// EstimateFPR computes the theoretical false positive rate for given parameters.
// This is a standalone function that does not require an actual filter instance.
func EstimateFPR(m, k, n uint) float64 {
	if m == 0 {
		return 1
	}
	exponent := -float64(k) * float64(n) / float64(m)
	return math.Pow(1-math.Exp(exponent), float64(k))
}

// EstimateInserted estimates the number of items inserted into a filter by
// counting the set bits. Uses the formula: n ≈ -(m/k) * ln(1 - X/m)
// where X is the number of set bits.
func EstimateInserted(f *filter.BloomFilter) (int, error) {
	if f == nil {
		return 0, ErrNilFilter
	}
	bitsSet := countBitsSet(f.Bits())
	if bitsSet == 0 {
		return 0, nil
	}
	m := float64(f.M())
	k := float64(f.K())
	x := float64(bitsSet)
	if x >= m {
		// All bits set — cannot estimate
		return int(m / k), nil
	}
	n := -(m / k) * math.Log(1-x/m)
	if n < 0 {
		return 0, nil
	}
	return int(math.Round(n)), nil
}

// CompareParams generates a comparison between two parameter sets showing
// the tradeoff between them (memory, FPR, capacity).
type Comparison struct {
	ParamA       Params
	ParamB       Params
	MemoryRatio  float64 // B.Bytes / A.Bytes
	FPRRatio     float64 // B.ActualFPR / A.ActualFPR
	CapacityGain float64 // B.ExpectedN / A.ExpectedN
}

// Compare two parameter recommendations.
func Compare(a, b Params) Comparison {
	memRatio := 1.0
	if a.Bytes > 0 {
		memRatio = float64(b.Bytes) / float64(a.Bytes)
	}
	fprRatio := 1.0
	if a.ActualFPR > 0 {
		fprRatio = b.ActualFPR / a.ActualFPR
	}
	capGain := 1.0
	if a.ExpectedN > 0 {
		capGain = float64(b.ExpectedN) / float64(a.ExpectedN)
	}
	return Comparison{
		ParamA:       a,
		ParamB:       b,
		MemoryRatio:  memRatio,
		FPRRatio:     fprRatio,
		CapacityGain: capGain,
	}
}

func countBitsSet(bits []byte) int {
	count := 0
	for _, b := range bits {
		count += popcount(b)
	}
	return count
}

func popcount(b byte) int {
	// Brian Kernighan's bit counting
	count := 0
	for b != 0 {
		b &= b - 1
		count++
	}
	return count
}
