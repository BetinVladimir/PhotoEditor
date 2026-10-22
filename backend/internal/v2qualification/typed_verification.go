package v2qualification

import "math"

const MeasuredGPUPeak = "measured_peak"
// ResourceEvidence preserves the provenance of the GPU admission bound. MPS
// allocator ceilings are never written into Report.Metrics.PeakVRAMBytes.
// This is resource admission evidence, not image-quality or execution authority.
type ResourceEvidence struct {
 RAMBytes int64 `json:"ram_bytes"`
 GPUBytes int64 `json:"gpu_bytes"`
 ScratchBytes int64 `json:"scratch_bytes"`
 GPUKind string `json:"gpu_kind"`
 measured Metrics
}
// Verification is a closed, value-owned result of verifying original bytes.
// It exposes no synthetic legacy Report for an MPS schema-2 document.
type Verification struct { resources ResourceEvidence }
func (v Verification) Resources() ResourceEvidence { return v.resources }
// MeasuredMetrics is deliberately unavailable for MPS. Its false result cannot
// be promoted into a zero or allocator-derived measured peak report.
func (r ResourceEvidence) MeasuredMetrics()(Metrics,bool){if r.GPUKind!=MeasuredGPUPeak{return Metrics{},false};return r.measured,true}
func (r ResourceEvidence) Valid() bool {
 return (r.GPUKind==MeasuredGPUPeak||r.GPUKind==MPSAllocatorMetric)&&r.RAMBytes>=0&&r.GPUBytes>=0&&r.ScratchBytes>=0&&r.RAMBytes<=9007199254740991&&r.GPUBytes<=9007199254740991&&r.ScratchBytes<=9007199254740991&&(r.GPUKind!=MPSAllocatorMetric||r.GPUBytes>0)
}
// VerifyTyped dispatches by deployment-controlled backend, never report claims.
// Legacy Verify remains unchanged and keeps rejecting schema-2/MPSResources.
func VerifyTyped(raw []byte, sha string, expected Expected)(Verification,error){
 if expected.Backend=="mps"&&MPSOperation(expected.Operation){
  report,err:=VerifyMPS(raw,sha,expected);if err!=nil{return Verification{},err}
  return Verification{resources:ResourceEvidence{RAMBytes:int64(math.Ceil(report.Metrics.RSSBytes)),GPUBytes:int64(math.Ceil(report.GPU.Ceiling)),ScratchBytes:int64(math.Ceil(report.Metrics.ScratchBytes)),GPUKind:MPSAllocatorMetric}},nil
 }
 report,err:=Verify(raw,sha,expected);if err!=nil{return Verification{},err}
 return Verification{resources:ResourceEvidence{RAMBytes:int64(math.Ceil(report.Metrics.PeakRSSBytes)),GPUBytes:int64(math.Ceil(report.Metrics.PeakVRAMBytes)),ScratchBytes:int64(math.Ceil(report.Metrics.ScratchBytes)),GPUKind:MeasuredGPUPeak,measured:report.Metrics}},nil
}
