package v2qualification
import "testing"
func TestTypedVerificationMPSPreservesKindAndNoLegacyPeak(t *testing.T){
 for _,operation:=range []string{"face.detect","face.landmarks","face.parse","mask.person","features.scene","scene.parse","depth.predict","image.upscale"}{t.Run(operation,func(t *testing.T){value,expected:=mpsFixture(t);value["operation"]=operation;expected.Operation=operation;raw,sha:=mpsEncode(t,value)
 verified,err:=VerifyTyped(raw,sha,expected);if err!=nil{t.Fatal(err)};r:=verified.Resources();if _,ok:=r.MeasuredMetrics();ok{t.Fatal("MPS fabricated measured metrics")};if !r.Valid()||r.GPUKind!=MPSAllocatorMetric||r.GPUBytes!=20000000000||r.RAMBytes!=1024||r.ScratchBytes!=2048{t.Fatal("wrong typed evidence",r)}
 if _,err:=Verify(raw,sha,expected);err==nil{t.Fatal("legacy report fabricated for MPS")}
 r.GPUBytes=1;if verified.Resources().GPUBytes!=20000000000{t.Fatal("mutable returned resource aliases verification")}
 })}
}
func TestTypedVerificationMPSRefusesPolicyIdentityAndResourceDrift(t *testing.T){
 for _,name:=range []string{"missing_policy","device","backend","license","claimed_peak","over_policy","composite"}{t.Run(name,func(t *testing.T){value,expected:=mpsFixture(t)
 switch name{case "missing_policy":expected.MPSResources=nil;case "device":expected.DeviceProfileSHA=expected.ModelLockSHA;case "backend":expected.Backend="cuda";case "license":expected.LicenseCleared=false;case "claimed_peak":value["gpu_memory"].(map[string]any)["peak_measurement_available"]=true;case "over_policy":value["gpu_memory"].(map[string]any)["allocator_ceiling_bytes"]=20000000001;case "composite":expected.Operation="semantic.transfer";value["operation"]="semantic.transfer"}
 raw,sha:=mpsEncode(t,value);if _,err:=VerifyTyped(raw,sha,expected);err==nil{t.Fatal("unqualified resource evidence accepted")}
 })}
 if (Verification{}).Resources().Valid(){t.Fatal("zero verification admitted")}
}
func TestTypedVerificationLegacyKeepsMeasuredKind(t *testing.T){
 for _,backend:=range []string{"cpu","cuda"}{t.Run(backend,func(t *testing.T){value,expected:=mpsFixture(t);delete(value,"backend");delete(value,"gpu_memory");value["schema_version"]=1;value["metrics"]=map[string]any{"p50_ms":1,"p95_ms":2,"peak_rss_bytes":1024.1,"peak_vram_bytes":512.1,"scratch_bytes":2048.1};expected.Backend=backend;expected.MPSResources=nil;raw,sha:=mpsEncode(t,value)
 legacy,err:=Verify(raw,sha,expected);if err!=nil{t.Fatal(err)};typed,err:=VerifyTyped(raw,sha,expected);if err!=nil{t.Fatal(err)};r:=typed.Resources();if r.GPUKind!=MeasuredGPUPeak||r.RAMBytes!=1025||r.GPUBytes!=513||r.ScratchBytes!=2049||legacy.Metrics.PeakVRAMBytes!=512.1{t.Fatal("legacy semantics changed",r)}
 })}
}
