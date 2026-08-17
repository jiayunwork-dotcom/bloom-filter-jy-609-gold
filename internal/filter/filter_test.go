package filter

import "testing"

func TestNewOK(t *testing.T) {
	f, err := New(1024, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.M() != 1024 || f.K() != 5 {
		t.Errorf("M=%d K=%d, want 1024/5", f.M(), f.K())
	}
	if len(f.Bits()) != (1024+7)/8 {
		t.Errorf("bits len = %d", len(f.Bits()))
	}
}

func TestNewInvalidParams(t *testing.T) {
	if _, err := New(0, 5); err != ErrInvalidParams {
		t.Errorf("New(0,5) err = %v, want ErrInvalidParams", err)
	}
	if _, err := New(1024, 0); err != ErrInvalidParams {
		t.Errorf("New(1024,0) err = %v, want ErrInvalidParams", err)
	}
}

func TestAddAndTest(t *testing.T) {
	f, _ := New(4096, 7)
	f.Add([]byte("apple"))
	f.Add([]byte("banana"))
	if !f.Test([]byte("apple")) {
		t.Error("apple should test present")
	}
	if !f.Test([]byte("banana")) {
		t.Error("banana should test present")
	}
}

func TestTestAbsent(t *testing.T) {
	f, _ := New(4096, 7)
	f.Add([]byte("apple"))
	if f.Test([]byte("grape")) {
		t.Error("grape should test absent")
	}
}

func TestAddNilData(t *testing.T) {
	f, _ := New(4096, 7)
	f.Add(nil)
	if !f.Test(nil) {
		t.Error("nil should test present after Add(nil)")
	}
}

func TestFalsePositiveRateEmpty(t *testing.T) {
	f, _ := New(10000, 7)
	if r := f.FalsePositiveRate(0); r < 0 || r > 1e-9 {
		t.Errorf("rate for n=0 = %v, want ~0", r)
	}
}

func TestFalsePositiveRateBounds(t *testing.T) {
	f, _ := New(10000, 7)
	for n := 1; n <= 1000; n += 100 {
		r := f.FalsePositiveRate(n)
		if r < 0 || r > 1 {
			t.Errorf("rate out of [0,1] for n=%d: %v", n, r)
		}
	}
}
