package v2qualification
import "testing"
// New schema2 MPS timing must not be routed through legacy schema1 Verify.
func TestVerifiedMPSTimingUsesQualifiedMeasuredP95(t *testing.T) {
 value,expected:=mpsFixture(t)
 value["metrics"].(map[string]any)["p95_ms"]=2.25
 raw,sha:=mpsEncode(t,value)
 estimate,err:=VerifiedTimingMilliseconds(raw,sha,expected)
 if err!=nil||estimate==nil||*estimate!=3 {t.Fatal("qualified MPS timing unavailable or rounded incorrectly",estimate,err)}
 if _,err=Verify(raw,sha,expected);err==nil {t.Fatal("legacy verifier accepted MPS resource envelope")}
 expected.LicenseCleared=false
 if estimate,err=VerifiedTimingMilliseconds(raw,sha,expected);err==nil||estimate!=nil {t.Fatal("unlicensed MPS timing accepted")}
}
func TestVerifiedMPSTimingRefusesChangedReportIdentity(t *testing.T) {
 value,expected:=mpsFixture(t);raw,sha:=mpsEncode(t,value)
 expected.DeviceProfileSHA=expected.ModelLockSHA
 if estimate,err:=VerifiedTimingMilliseconds(raw,sha,expected);err==nil||estimate!=nil {t.Fatal("foreign device timing accepted")}
}
