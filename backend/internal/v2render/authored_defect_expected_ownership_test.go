package v2render
import("testing";"photoeditor/backend/internal/v2qualification")
func TestAuthoredDefectExpectedOwnsMPSPolicyBidirectionally(t *testing.T){
 original:=v2qualification.Expected{MPSResources:&v2qualification.MPSResourcePolicy{AllocatorBytes:20000000000},RequiredChecks:[]string{"quality"},EvidenceSHAs:map[string]struct{}{"a":{}}}
 owned:=ownAuthoredDefectExpected(original)
 original.MPSResources.AllocatorBytes=1;original.RequiredChecks[0]="changed";delete(original.EvidenceSHAs,"a")
 if owned.MPSResources.AllocatorBytes!=20000000000||owned.RequiredChecks[0]!="quality"{t.Fatal("trusted input aliases copy")};if _,ok:=owned.EvidenceSHAs["a"];!ok{t.Fatal("map aliases input")}
 owned.MPSResources.AllocatorBytes=2;if original.MPSResources.AllocatorBytes!=1{t.Fatal("copy aliases trusted policy")}
 if ownAuthoredDefectExpected(v2qualification.Expected{}).MPSResources!=nil{t.Fatal("nil policy changed")}
}
