package taskrecovery

import (
	"fmt"
	"testing"
)

func cappedResourceProfile(cap string, pixels int64) []byte {
	return []byte(fmt.Sprintf(`{"backend":"cpu","max_pixels":%s,"resource_estimate":{"ram_bytes":536870912,"vram_bytes":0,"scratch_bytes":104858922,"output_bytes":240123904,"pixel_count":%d}}`, cap, pixels))
}
func TestExplicitZeroPixelCapIsHardNonPixelBound(t *testing.T) {
	e, err := ParseResourceEstimate(cappedResourceProfile("0", 0))
	if err != nil || e.PixelCount != 0 || e.RAMBytes != 536870912 {
		t.Fatal("nonimage import rejected", err)
	}
	if _, err = ParseResourceEstimate(cappedResourceProfile("0", 1)); err == nil {
		t.Fatal("zero cap became unlimited")
	}
}
func TestExplicitPixelCapRejectsInvalidOrNull(t *testing.T) {
	for _, cap := range []string{"null", "-1", "0.5", `"0"`, `"200000000"`, "true", "{}", "[]", "9007199254740992", "9223372036854775808"} {
		t.Run(cap, func(t *testing.T) {
			if _, err := ParseResourceEstimate(cappedResourceProfile(cap, 0)); err == nil {
				t.Fatal("invalid cap admitted")
			}
		})
	}
}
func TestAbsentPixelCapRetainsDefault200Million(t *testing.T) {
	for _, pixels := range []int64{0, 200000000, 200000001} {
		raw := []byte(fmt.Sprintf(`{"resource_estimate":{"ram_bytes":1,"vram_bytes":0,"scratch_bytes":0,"output_bytes":0,"pixel_count":%d}}`, pixels))
		_, err := ParseResourceEstimate(raw)
		if (err == nil) != (pixels <= 200000000) {
			t.Fatalf("default changed at %d", pixels)
		}
	}
}
