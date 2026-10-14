package v2portrait

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestDonorResourceParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/donor-resource-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Dimensions [6]int64              `json:"dimensions"`
		Expected   DonorResourceEstimate `json:"expected"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, f := range cases {
		d := f.Dimensions
		got, err := DonorPortraitResources(d[0], d[1], d[2], d[3], d[4], d[5])
		if err != nil || got != f.Expected {
			t.Fatal(f, got, err)
		}
	}
	for _, d := range [][6]int64{{0, 100, 100, 100, 1, 1}, {100, 100, 100, 100, 101, 1},
		{math.MaxInt64, math.MaxInt64, 1, 1, 1, 1}, {100, 100, 1, 1, math.MaxInt64, math.MaxInt64}} {
		got, err := DonorPortraitResources(d[0], d[1], d[2], d[3], d[4], d[5])
		if err == nil || got != (DonorResourceEstimate{}) {
			t.Fatal(d, got, err)
		}
	}
}
