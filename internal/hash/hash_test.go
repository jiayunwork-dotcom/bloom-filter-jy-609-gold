package hash

import "testing"

func TestHashRange(t *testing.T) {
	m := uint(1000)
	for _, d := range [][]byte{[]byte("a"), []byte("hello"), []byte("world")} {
		for i := uint(0); i < 5; i++ {
			if idx := Hash(d, i, m); idx >= m {
				t.Errorf("Hash(%q,%d,%d)=%d out of range", d, i, m, idx)
			}
		}
	}
}

func TestHashDeterministic(t *testing.T) {
	a := Hash([]byte("payload"), 2, 1000)
	b := Hash([]byte("payload"), 2, 1000)
	if a != b {
		t.Errorf("Hash not deterministic: %d != %d", a, b)
	}
}

func TestHashDistinctI(t *testing.T) {
	m := uint(1 << 20)
	seen := map[uint]bool{}
	for i := uint(0); i < 7; i++ {
		idx := Hash([]byte("same-input"), i, m)
		if seen[idx] {
			t.Errorf("collision across i=%d at idx=%d (m=%d)", i, idx, m)
		}
		seen[idx] = true
	}
}

func TestHashZeroM(t *testing.T) {
	if got := Hash([]byte("x"), 0, 0); got != 0 {
		t.Errorf("Hash with m=0 = %d, want 0", got)
	}
}

func TestHashNilData(t *testing.T) {
	a := Hash(nil, 0, 512)
	b := Hash(nil, 0, 512)
	if a != b {
		t.Errorf("Hash(nil) not deterministic: %d != %d", a, b)
	}
	if a >= 512 {
		t.Errorf("Hash(nil,0,512)=%d out of range", a)
	}
}
