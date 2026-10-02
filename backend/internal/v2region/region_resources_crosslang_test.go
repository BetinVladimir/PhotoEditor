package v2region

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRegionSourceBudgetWorkerParity(t *testing.T) {
	raw := os.Getenv("PHOTO_REGION_SOURCE_BUDGETS")
	if raw == "" {
		t.Skip("Python worker resource vectors required")
	}
	var cases []struct {
		Width, Height                      int64
		Alpha                              bool
		PixelCount, RAMBytes, ScratchBytes int64
	}
	if err := json.Unmarshal([]byte(raw), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 10 {
		t.Fatal("missing boundary coverage")
	}
	for _, c := range cases {
		budget, err := (RegionSourceShape{Width: c.Width, Height: c.Height, HasAlpha: c.Alpha}).ResourceBudget()
		if err != nil || budget.PixelCount != c.PixelCount || budget.RAMBytes != c.RAMBytes || budget.ScratchBytes != c.ScratchBytes {
			t.Fatal("server/worker source budget mismatch", c, budget, err)
		}
	}
}
