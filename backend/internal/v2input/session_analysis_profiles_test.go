package v2input

import (
	"encoding/json"
	"testing"
)

func TestSessionDescribeProfileAccountsForLargestFrameAndTotalWork(t *testing.T) {
	raw, err := compileSessionDescribeProfile("cuda", 24<<30, 10<<30,
		[]int64{20_000_000, 8_000_000})
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if json.Unmarshal(raw, &profile) != nil ||
		profile.ResourceEstimate.RAMBytes != 2<<30+64*20_000_000 ||
		profile.ResourceEstimate.PixelCount != 28_000_000 ||
		profile.ResourceEstimate.VRAMBytes != 6<<30 {
		t.Fatal("incorrect resource estimate")
	}
	if _, err := compileSessionDescribeProfile("cuda", 8<<30, 10<<30,
		[]int64{200_000_000}); err == nil {
		t.Fatal("accepted an image that exceeds RAM budget")
	}
	if _, err := compileSessionDescribeProfile("cuda", 24<<30, 4<<30,
		[]int64{1}); err == nil {
		t.Fatal("accepted an undersized VRAM budget")
	}
}
