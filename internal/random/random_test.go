package random

import (
	"strings"
	"testing"
)

func TestCode(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		c := Code(6)
		if len(c) != 6 {
			t.Fatalf("length %d", len(c))
		}
		for _, r := range c {
			if !strings.ContainsRune(CodeAlphabet, r) {
				t.Fatalf("%q has %q", c, r)
			}
		}
		seen[c] = true
	}
	if len(seen) < 990 {
		t.Fatalf("only %d distinct codes in 1000", len(seen))
	}
}

func TestIntnUniformEnough(t *testing.T) {
	counts := make([]int, 4)
	for range 4000 {
		counts[Intn(4)]++
	}
	for i, c := range counts {
		if c < 800 || c > 1200 {
			t.Fatalf("bucket %d got %d of 4000", i, c)
		}
	}
}

func TestTokenAndID(t *testing.T) {
	if a, b := Token(), Token(); a == b || len(a) != 43 {
		t.Fatalf("tokens %q %q", a, b)
	}
	if id := ID("p_"); !strings.HasPrefix(id, "p_") || len(id) != 14 {
		t.Fatalf("id %q", id)
	}
}
