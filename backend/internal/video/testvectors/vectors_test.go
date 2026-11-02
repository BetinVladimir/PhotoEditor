package testvectors

import "testing"

func TestLoadTimeFrameTicks(t *testing.T) {
	var v struct {
		FrameTicks map[string]int64 `json:"frame_ticks"`
	}
	Load(t, "time", &v)
	if got := v.FrameTicks["25/1"]; got != 28224000 {
		t.Fatalf("25/1 = %d, want 28224000", got)
	}
}
