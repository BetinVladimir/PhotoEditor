package v2render
import (
 "math"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
// Resource admission costs remain typed: the MPS allocator ceiling is a
// conservative bound, never rewritten into a measured VRAM peak report.
func verifiedAuthoredNativeCosts(raw []byte, sha string, expected v2qualification.Expected) (v2input.SemanticQualifiedCosts,error) {
 if expected.Operation!="image.upscale"||expected.Version!="1" {return v2input.SemanticQualifiedCosts{},v2qualification.ErrUnqualified}
 if expected.MPSResources!=nil {
  report,err:=v2qualification.VerifyMPS(raw,sha,expected)
  if err!=nil{return v2input.SemanticQualifiedCosts{},err}
  return v2input.SemanticQualifiedCosts{RAMBytes:int64(math.Ceil(report.Metrics.RSSBytes)),VRAMBytes:int64(math.Ceil(report.GPU.Ceiling)),ScratchBytes:int64(math.Ceil(report.Metrics.ScratchBytes))},nil
 }
 report,err:=v2qualification.Verify(raw,sha,expected)
 if err!=nil{return v2input.SemanticQualifiedCosts{},err}
 return geometryNativeMeasuredCosts(report.Metrics)
}
