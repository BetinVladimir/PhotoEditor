package v2qualification

import "math"

// VerifiedTimingMilliseconds derives a scheduling estimate from the same exact
// report verification used by qualification import. It does not establish task
// dimension/device binding; callers must retain that independent authority.
// A zero measurement means unavailable, preserving the unknown-cost fallback.
func VerifiedTimingMilliseconds(raw []byte,reportSHA string,expected Expected)(*int64,error){
 if expected.MPSResources!=nil {
  report,err:=VerifyMPS(raw,reportSHA,expected)
  if err!=nil{return nil,err}
  value:=int64(math.Ceil(report.Metrics.P95MS))
  return &value,nil
 }
 report,err:=Verify(raw,reportSHA,expected)
 if err!=nil{return nil,err}
 if report.Metrics.P95MS==0{return nil,nil}
 value:=int64(math.Ceil(report.Metrics.P95MS))
 return &value,nil
}
