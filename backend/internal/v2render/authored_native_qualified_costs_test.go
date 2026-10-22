package v2render
import (
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2qualification"
)
func authoredNativeMPSCostFixture(t *testing.T)(map[string]any,v2qualification.Expected){
 t.Helper();pin:=func(s string)string{return strings.Repeat(s,64)};issuer:=uuid.New()
 expected:=v2qualification.Expected{Operation:"image.upscale",Version:"1",Backend:"mps",ModelLockSHA:pin("a"),RuntimeLockSHA:pin("b"),DeviceProfileSHA:pin("c"),DatasetSHA:pin("d"),IssuerID:issuer,RequiredChecks:[]string{"quality","metal_real_inference","gpu_memory_envelope"},CalibrationSHAs:map[string]struct{}{pin("0"):{}},TestFixtureSHAs:map[string]struct{}{pin("e"):{}},EvidenceSHAs:map[string]struct{}{pin("f"):{}},LicenseCleared:true,MPSResources:&v2qualification.MPSResourcePolicy{Metric:v2qualification.MPSAllocatorMetric,AllocatorBytes:20000000000,P95MS:100,RSSBytes:4096,ScratchBytes:8192}}
 checks:=[]any{};for _,id:=range expected.RequiredChecks{checks=append(checks,map[string]any{"id":id,"passed":true,"evidence_sha":pin("f")})}
 value:=map[string]any{"schema_version":2,"backend":"mps","operation":"image.upscale","version":"1","model_lock_sha":expected.ModelLockSHA,"runtime_lock_sha":expected.RuntimeLockSHA,"device_profile_sha":expected.DeviceProfileSHA,"dataset_sha":expected.DatasetSHA,"issuer_id":issuer.String(),"created_at":"2026-10-18T00:00:00Z","input_fixture_shas":[]string{pin("e")},"checks":checks,"metrics":map[string]any{"p50_ms":1,"p95_ms":2,"peak_rss_bytes":1024,"scratch_bytes":2048},"gpu_memory":map[string]any{"metric":v2qualification.MPSAllocatorMetric,"allocator_ceiling_bytes":15000000000,"driver_sample_max_bytes":15,"peak_measurement_available":false},"status":"passed"};return value,expected
}
func authoredNativeCostWire(t *testing.T,value any)([]byte,string){t.Helper();raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(raw);return raw,hex.EncodeToString(sum[:])}
func TestAuthoredNativeMPSCostsKeepAllocatorBoundNotMeasuredPeak(t *testing.T){
 value,expected:=authoredNativeMPSCostFixture(t);raw,sha:=authoredNativeCostWire(t,value);costs,err:=verifiedAuthoredNativeCosts(raw,sha,expected)
 if err!=nil||costs.RAMBytes!=1024||costs.VRAMBytes!=15000000000||costs.ScratchBytes!=2048{t.Fatal("MPS conservative costs unavailable or changed",costs,err)}
 if _,err=v2qualification.Verify(raw,sha,expected);err==nil{t.Fatal("legacy peak verifier accepted schema2")}
}
func TestAuthoredNativeMPSCostsRejectPolicyAndIdentityDrift(t *testing.T){
 for _,scenario:=range []string{"license","device","missing_policy","claimed_peak","over_policy"}{t.Run(scenario,func(t *testing.T){value,expected:=authoredNativeMPSCostFixture(t)
 switch scenario{case "license":expected.LicenseCleared=false;case "device":expected.DeviceProfileSHA=expected.ModelLockSHA;case "missing_policy":expected.MPSResources=nil;case "claimed_peak":value["gpu_memory"].(map[string]any)["peak_measurement_available"]=true;case "over_policy":value["gpu_memory"].(map[string]any)["allocator_ceiling_bytes"]=21000000000}
 raw,sha:=authoredNativeCostWire(t,value);if _,err:=verifiedAuthoredNativeCosts(raw,sha,expected);err==nil{t.Fatal("unqualified MPS costs accepted")}})}
}
func TestAuthoredNativeLegacyMeasuredCostsRemainSeparate(t *testing.T){
 value,expected:=authoredNativeMPSCostFixture(t);delete(value,"backend");delete(value,"gpu_memory");value["schema_version"]=1;expected.MPSResources=nil;expected.Backend="cuda";value["metrics"]=map[string]any{"p50_ms":1,"p95_ms":2,"peak_rss_bytes":1024,"scratch_bytes":2048,"peak_vram_bytes":512}
 raw,sha:=authoredNativeCostWire(t,value);costs,err:=verifiedAuthoredNativeCosts(raw,sha,expected);if err!=nil||costs.VRAMBytes!=512{t.Fatal("legacy measured costs changed",costs,err)}
}
