package v2portrait

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestLocalPortraitResourceParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/local-resource-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Width    int64                 `json:"width"`
		Height   int64                 `json:"height"`
		Expected LocalResourceEstimate `json:"expected"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		got, err := LocalPortraitResources(f.Width, f.Height)
		if err != nil || got != f.Expected {
			t.Fatal(f, got, err)
		}
	}
	for _, pair := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {200000001, 1}, {20000, 10001}, {math.MaxInt64, math.MaxInt64}} {
		got, err := LocalPortraitResources(pair[0], pair[1])
		if err == nil || got != (LocalResourceEstimate{}) {
			t.Fatal("invalid dimensions admitted", pair, got, err)
		}
	}
}
