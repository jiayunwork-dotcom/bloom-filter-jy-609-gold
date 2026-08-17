package hash

import "hash/fnv"

// Hash returns the i-th (0-indexed, 0 <= i < k) bit index in a Bloom filter of
// m bits for the given data, using double hashing (Kirsch-Mitzenmacher):
//
//	idx = (h1 + i*h2) mod m
//
// where h1 and h2 are two independent base hashes over data (FNV-1a and FNV-1).
// If m == 0 the function returns 0 (callers must guard against m == 0).
func Hash(data []byte, i, m uint) uint {
	if m == 0 {
		return 0
	}
	h1 := fnv64a(data)
	h2 := fnv64(data)
	return uint((h1 + uint64(i)*h2) % uint64(m))
}

func fnv64a(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

func fnv64(data []byte) uint64 {
	h := fnv.New64()
	h.Write(data)
	return h.Sum64()
}
