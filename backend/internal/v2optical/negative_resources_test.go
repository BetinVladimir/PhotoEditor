package v2optical

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestNegativeResourceParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-resource-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Width    int64
		Height   int64
		Alpha    bool
		Expected NegativeResourceEstimate
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, err := NegativeResources(c.Width, c.Height, c.Alpha)
		if err != nil || got != c.Expected {
			t.Fatal(c, got, err)
		}
	}
	for _, d := range [][2]int64{{0, 1}, {1, 0}, {200000001, 1}, {math.MaxInt64, math.MaxInt64}} {
		got, err := NegativeResources(d[0], d[1], false)
		if err == nil || got != (NegativeResourceEstimate{}) {
			t.Fatal(d, got, err)
		}
	}
}
