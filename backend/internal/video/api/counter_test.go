package videoapi

import "testing"

func TestCounterBoundsHoldersPerKey(t *testing.T) {
	var c counter
	for i := 0; i < 3; i++ {
		if !c.acquire("u", 3) {
			t.Fatalf("acquire %d", i)
		}
	}
	if c.acquire("u", 3) {
		t.Fatal("fourth holder admitted")
	}
	if !c.acquire("other", 3) {
		t.Fatal("limits are per key")
	}
	c.release("u")
	if !c.acquire("u", 3) {
		t.Fatal("slot not freed")
	}
	for i := 0; i < 4; i++ {
		c.release("u")
	}
	if len(c.n) != 1 {
		t.Fatalf("leaked keys: %v", c.n)
	}
}
