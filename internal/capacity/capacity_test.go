package capacity

import (
	"math"
	"testing"

	"bloom-filter/internal/filter"
)

func TestRecommend(t *testing.T) {
	p, err := Recommend(1000, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if p.M == 0 || p.K == 0 {
		t.Error("M and K should be non-zero")
	}
	if p.ActualFPR > p.TargetFPR {
		t.Errorf("ActualFPR %v > TargetFPR %v", p.ActualFPR, p.TargetFPR)
	}
	if p.BitsPerItem < 1 {
		t.Errorf("BitsPerItem %v too low", p.BitsPerItem)
	}
	if p.Bytes != (p.M+7)/8 {
		t.Errorf("Bytes mismatch: %d vs %d", p.Bytes, (p.M+7)/8)
	}
}

func TestRecommendErrors(t *testing.T) {
	if _, err := Recommend(0, 0.01); err != ErrInvalidCount {
		t.Errorf("expected ErrInvalidCount, got %v", err)
	}
	if _, err := Recommend(100, 0); err != ErrInvalidFPR {
		t.Errorf("expected ErrInvalidFPR, got %v", err)
	}
	if _, err := Recommend(100, 1.0); err != ErrInvalidFPR {
		t.Errorf("expected ErrInvalidFPR, got %v", err)
	}
	if _, err := Recommend(100, -0.5); err != ErrInvalidFPR {
		t.Errorf("expected ErrInvalidFPR, got %v", err)
	}
}

func TestMonitorHealthy(t *testing.T) {
	f, _ := filter.NewFromCapacity(1000, 0.01)
	// Insert only 100 items (well below capacity)
	for i := 0; i < 100; i++ {
		f.Add([]byte{byte(i), byte(i >> 8)})
	}
	r, err := Monitor(f, 100, DefaultMonitorConfig())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", r.StatusLabel)
	}
	if r.RemainingN <= 0 {
		t.Errorf("expected positive remaining, got %d", r.RemainingN)
	}
	if r.Saturation <= 0 || r.Saturation >= 1 {
		t.Errorf("saturation out of range: %v", r.Saturation)
	}
}

func TestMonitorCritical(t *testing.T) {
	// Small filter, insert many items
	f, _ := filter.New(256, 3)
	for i := 0; i < 500; i++ {
		f.Add([]byte{byte(i), byte(i >> 8)})
	}
	r, err := Monitor(f, 500, DefaultMonitorConfig())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusCritical {
		t.Errorf("expected critical, got %s (FPR=%v)", r.StatusLabel, r.CurrentFPR)
	}
}

func TestMonitorWarning(t *testing.T) {
	// Create a filter that will be in warning range with moderate load
	f, _ := filter.NewFromCapacity(100, 0.05)
	for i := 0; i < 95; i++ {
		f.Add([]byte{byte(i)})
	}
	cfg := MonitorConfig{WarningFPR: 0.04, CriticalFPR: 0.10}
	r, err := Monitor(f, 95, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// At near-capacity, should be warning or critical
	if r.Status == StatusHealthy {
		t.Errorf("expected warning or critical at 95/100, got healthy (FPR=%v)", r.CurrentFPR)
	}
}

func TestMonitorErrors(t *testing.T) {
	if _, err := Monitor(nil, 0, DefaultMonitorConfig()); err != ErrNilFilter {
		t.Errorf("expected ErrNilFilter, got %v", err)
	}
	f, _ := filter.New(1024, 3)
	if _, err := Monitor(f, 0, MonitorConfig{WarningFPR: 0, CriticalFPR: 0.1}); err != ErrInvalidFPR {
		t.Errorf("expected ErrInvalidFPR, got %v", err)
	}
}

func TestAdviseNoScale(t *testing.T) {
	f, _ := filter.NewFromCapacity(1000, 0.01)
	for i := 0; i < 100; i++ {
		f.Add([]byte{byte(i)})
	}
	a, err := Advise(f, 100, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if a.ShouldScale {
		t.Errorf("should not recommend scaling at 100/1000: %s", a.Reason)
	}
}

func TestAdviseScale(t *testing.T) {
	f, _ := filter.NewFromCapacity(100, 0.01)
	for i := 0; i < 90; i++ {
		f.Add([]byte{byte(i)})
	}
	a, err := Advise(f, 90, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if !a.ShouldScale {
		t.Errorf("should recommend scaling at 90/100: %s", a.Reason)
	}
	if a.NewParams.ExpectedN < 180 {
		t.Errorf("new capacity should be >= 2x: %d", a.NewParams.ExpectedN)
	}
	if a.GrowthFactor <= 1.0 {
		t.Errorf("growth factor should be > 1: %v", a.GrowthFactor)
	}
}

func TestAdviseErrors(t *testing.T) {
	if _, err := Advise(nil, 0, 0.01); err != ErrNilFilter {
		t.Errorf("expected ErrNilFilter, got %v", err)
	}
	f, _ := filter.New(1024, 3)
	if _, err := Advise(f, 0, 2.0); err != ErrInvalidFPR {
		t.Errorf("expected ErrInvalidFPR, got %v", err)
	}
}

func TestEstimateFPR(t *testing.T) {
	// Compare with filter.FalsePositiveRate
	f, _ := filter.New(10000, 7)
	for n := 100; n <= 1000; n += 100 {
		expected := f.FalsePositiveRate(n)
		got := EstimateFPR(10000, 7, uint(n))
		if math.Abs(got-expected) > 1e-9 {
			t.Errorf("EstimateFPR(10000,7,%d) = %v, want %v", n, got, expected)
		}
	}
}

func TestEstimateInserted(t *testing.T) {
	f, _ := filter.New(10000, 7)
	for i := 0; i < 200; i++ {
		f.Add([]byte{byte(i), byte(i >> 8)})
	}
	est, err := EstimateInserted(f)
	if err != nil {
		t.Fatal(err)
	}
	// Estimate should be within 20% of actual
	if math.Abs(float64(est)-200) > 40 {
		t.Errorf("EstimateInserted = %d, want ~200", est)
	}
}

func TestEstimateInsertedEmpty(t *testing.T) {
	f, _ := filter.New(1024, 3)
	est, err := EstimateInserted(f)
	if err != nil {
		t.Fatal(err)
	}
	if est != 0 {
		t.Errorf("empty filter estimate = %d, want 0", est)
	}
}

func TestEstimateInsertedNil(t *testing.T) {
	if _, err := EstimateInserted(nil); err != ErrNilFilter {
		t.Errorf("expected ErrNilFilter, got %v", err)
	}
}

func TestCompare(t *testing.T) {
	a, _ := Recommend(100, 0.01)
	b, _ := Recommend(1000, 0.01)
	c := Compare(a, b)
	if c.MemoryRatio <= 1 {
		t.Errorf("10x capacity should use more memory: ratio=%v", c.MemoryRatio)
	}
	if c.CapacityGain != 10 {
		t.Errorf("capacity gain should be 10, got %v", c.CapacityGain)
	}
}

func TestStatusString(t *testing.T) {
	if StatusHealthy.String() != "healthy" {
		t.Error("healthy string")
	}
	if StatusWarning.String() != "warning" {
		t.Error("warning string")
	}
	if StatusCritical.String() != "critical" {
		t.Error("critical string")
	}
	if Status(99).String() != "unknown" {
		t.Error("unknown string")
	}
}
