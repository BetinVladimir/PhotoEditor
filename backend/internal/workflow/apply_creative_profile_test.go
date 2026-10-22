package workflow

import (
 "encoding/json"
 "testing"
)

func TestCreativeProfileAdmitsRGBAlphaAndGeneratedCoverage(t *testing.T) {
 const pixels int64 = 24000000
 raw, err := applyCreativeProfile("mps", "locked", pixels, 1024)
 if err != nil { t.Fatal(err) }
 var profile struct { Estimate ResourceEstimate `json:"resource_estimate"` }
 if err := json.Unmarshal(raw,&profile); err != nil { t.Fatal(err) }
 if profile.Estimate.OutputBytes < 18*pixels+6*1048576 || profile.Estimate.ScratchBytes < 166*pixels ||
  profile.Estimate.RAMBytes < 192*pixels+512*1048576 { t.Fatal("creative outputs or masks are not fully admitted") }
}
func TestCreativeProfileRefusesInvalidDimensions(t *testing.T) {
 for _, pixels := range []int64{0,-1,200000001} {
  if _, err := applyCreativeProfile("cpu","locked",pixels,1024); err == nil { t.Fatal("invalid pixels accepted") }
 }
 if _, err := applyCreativeProfile("fallback","locked",1,1024); err == nil { t.Fatal("unknown backend accepted") }
}
