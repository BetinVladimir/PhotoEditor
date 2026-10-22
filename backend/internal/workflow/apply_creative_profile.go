package workflow

import (
 "encoding/json"
 "errors"
 "photoeditor/backend/internal/canonical"
)

// Creative effects publish RGB, optional legacy alpha and exact generated
// coverage. Full-frame masks are inputs even when inference runs over an ROI.
func applyCreativeProfile(backend, lock string, pixels, documentBytes int64) (json.RawMessage, error) {
 if pixels < 1 || pixels > 200000000 || documentBytes < 0 || documentBytes > 1048576 ||
  (backend != "cpu" && backend != "mps" && backend != "cuda") {
  return nil, errors.New("invalid creative resource dimensions")
 }
 ram := pixels*192 + 512*1048576
 if ram < 2<<30 { ram = 2<<30 }
 output := pixels*18 + 6*1048576
 return canonical.Marshal(struct {
  Backend string `json:"backend"`
  Precision string `json:"precision"`
  Tile int `json:"tile_size"`
  ModelLock string `json:"model_lock_id"`
  Estimate ResourceEstimate `json:"resource_estimate"`
 }{backend,"float32",512,lock,ResourceEstimate{
  RAMBytes:ram,VRAMBytes:map[string]int64{"cpu":0,"mps":1<<30,"cuda":1<<30}[backend],
  ScratchBytes:pixels*148+output+documentBytes+8*1048576,OutputBytes:output,PixelCount:pixels,
 }})
}
