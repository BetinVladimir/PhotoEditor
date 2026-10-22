package v2render

import (
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredNativeReplayMeasuredBudgetExcludesIndex(t *testing.T) {
	const index int64 = 16 << 20
	costs := v2input.SemanticQualifiedCosts{RAMBytes: 9, VRAMBytes: 8, ScratchBytes: 7}
	for _, tc := range []struct {
		name                         string
		ram, vram, scratch, overhead int64
		valid                        bool
	}{
		{"exact native plus index", 9, 8, 7 + index, index, true},
		{"index cannot cover native scratch", 9, 8, index, index, false},
		{"native scratch one short", 9, 8, 6 + index, index, false},
		{"ram one short", 8, 8, 7 + index, index, false},
		{"vram one short", 9, 7, 7 + index, index, false},
		{"index absent", 9, 8, 7, 0, false},
		{"index exceeds total", 9, 8, index - 1, index, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"input_index_scratch_bytes": tc.overhead, "resource_estimate": map[string]int64{"ram_bytes": tc.ram, "vram_bytes": tc.vram, "scratch_bytes": tc.scratch}})
			err := authoredNativeReplayMeasuredBudget(raw, costs)
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && !errors.Is(err, v2qualification.ErrUnqualified) {
				t.Fatalf("expected unqualified, got %v", err)
			}
		})
	}
}
