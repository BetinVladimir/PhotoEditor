package workflow

import (
	"encoding/json"
	"testing"
)

func TestDustRepairProfileOwnsFullFrameRAMAndScratch(t *testing.T) {
	for _, pixels := range []int64{1, 12000000, 48000000, 200000000} {
		raw, err := applyDustRepairProfile(pixels, 4096)
		if err != nil {
			t.Fatal(err)
		}
		var profile struct {
			Backend  string           `json:"backend"`
			Estimate ResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(raw, &profile) != nil {
			t.Fatal("invalid profile")
		}
		required := pixels*128 + 256*1048576
		if required < 536870912 {
			required = 536870912
		}
		if profile.Backend != "cpu" || profile.Estimate.RAMBytes < required || profile.Estimate.PixelCount != pixels || profile.Estimate.VRAMBytes != 0 {
			t.Fatal("dust CPU admission underfunded")
		}
		if profile.Estimate.ScratchBytes < pixels*24+4096+16384 || profile.Estimate.OutputBytes < pixels*12+8192 {
			t.Fatal("source/output ownership underfunded")
		}
		if pixels == 48000000 && profile.Estimate.RAMBytes != 6412435456 {
			t.Fatal("worker/profile memory bound differs")
		}
	}
}

func TestDustRepairProfileRejectsInvalidDimensions(t *testing.T) {
	for _, pair := range [][2]int64{{0, 0}, {-1, 0}, {200000001, 0}, {1, -1}, {1, 1048577}} {
		if _, err := applyDustRepairProfile(pair[0], pair[1]); err == nil {
			t.Fatal("invalid dimensions accepted")
		}
	}
}

func TestDustDetectionProfileSequentialRAMAndRetainedScratch(t *testing.T) {
	for _, n := range []int64{12000000, 48000000, 200000000} {
		for _, frames := range []int{5, 64} {
			raw, err := applyDustDetectionProfile(n*int64(frames), n, 4096, frames)
			if err != nil {
				t.Fatal(err)
			}
			var p struct {
				Backend  string           `json:"backend"`
				Estimate ResourceEstimate `json:"resource_estimate"`
			}
			if json.Unmarshal(raw, &p) != nil {
				t.Fatal("invalid profile")
			}
			required := n*192 + 256*1048576
			if p.Backend != "cpu" || p.Estimate.RAMBytes < required || p.Estimate.VRAMBytes != 0 || p.Estimate.PixelCount != n*int64(frames) {
				t.Fatal("detector admission underfunded")
			}
			if p.Estimate.ScratchBytes < n*int64(frames)*12+4096 || p.Estimate.OutputBytes < 1048576 {
				t.Fatal("detector artifact ownership underfunded")
			}
		}
	}
	for _, v := range [][4]int64{{320, 64, 0, 4}, {4160, 64, 0, 65}, {500, 100, -1, 5}, {1000, 100, 0, 5}, {1000000005, 200000001, 0, 5}} {
		if _, err := applyDustDetectionProfile(v[0], v[1], v[2], int(v[3])); err == nil {
			t.Fatal("invalid detector dimensions admitted")
		}
	}
}
