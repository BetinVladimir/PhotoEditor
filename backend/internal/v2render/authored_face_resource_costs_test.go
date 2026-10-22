package v2render
import("testing";"photoeditor/backend/internal/v2qualification")
func TestAuthoredFaceTypedResourcesPermitQualifiedMetalAboveTenGB(t *testing.T){
 value,expected:=authoredNativeMPSCostFixture(t);value["operation"]="face.detect";expected.Operation="face.detect";raw,sha:=authoredNativeCostWire(t,value);verified,err:=v2qualification.VerifyTyped(raw,sha,expected);if err!=nil{t.Fatal(err)}
 costs,err:=authoredFaceResourceCosts(verified.Resources());if err!=nil||costs.VRAMBytes!=15000000000{t.Fatal(costs,err)}
 if _,err:=authoredFaceMeasuredCosts(v2qualification.Metrics{PeakRSSBytes:1024,ScratchBytes:2048});err==nil{t.Fatal("legacy zero peak admitted")}
 r:=verified.Resources();r.GPUKind="sampled_peak";if _,err:=authoredFaceResourceCosts(r);err==nil{t.Fatal("unknown GPU measurement admitted")}
}
