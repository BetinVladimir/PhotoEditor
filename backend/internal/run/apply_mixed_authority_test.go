package run
import("context";"testing";"github.com/google/uuid")
func TestApplyMixedRequirementsRetainIndependentRuntimeGroups(t *testing.T){
 r:=applyRuntimeSelection{PersonRequired:true,PersonBackend:"mps",PersonLock:"person-lock",RestorationBackend:"mps",RestorationLock:"restore-lock",CreativeBackend:"mps",CreativeLock:"creative-lock"}
 var n styleExecutionNeeds
 n.Geometry.Enabled=true;n.Beauty.Enabled=true;n.Extensions.Background.Mode="blur";n.Extensions.Culling.Enabled=true
 n.Extensions.Restoration.Denoise=denoiseModelID;n.Extensions.Restoration.Upscale="2x";n.Extensions.Inpainting.Enabled=true;n.Extensions.Generation.Enabled=true
 n.Extensions.Composition.Enabled=true;n.Extensions.Composition.OutpaintPolicy="review"
 got:=applyMixedRequirements(r,n)
 want:=map[string][2]string{}
 for _,op:=range []string{"mask.person","pose.detect","face.detect","face.landmarks","face.parse","depth.predict","features.scene"}{want[op]=[2]string{"mps","person-lock"}}
 for _,op:=range []string{"image.denoise","image.upscale"}{want[op]=[2]string{"mps","restore-lock"}}
 for _,op:=range []string{"image.inpaint","image.generate","image.outpaint"}{want[op]=[2]string{"mps","creative-lock"}}
 if len(got)!=len(want){t.Fatalf("missing/extraneous mixed operations: %v",got)}
 for op,pins:=range want{if got[op]!=pins{t.Fatalf("cross-group or missing pins for %s: %v",op,got[op])}}
}
func TestApplyMixedRequirementsDoNotInventUnrequestedOperations(t *testing.T){
 r:=applyRuntimeSelection{PersonBackend:"mps",PersonLock:"p",RestorationBackend:"cuda",RestorationLock:"r",CreativeBackend:"mps",CreativeLock:"c"}
 var n styleExecutionNeeds
 if got:=applyMixedRequirements(r,n);len(got)!=0{t.Fatal("unused providers became requirements",got)}
 n.Defects.Enabled=true
 got:=applyMixedRequirements(r,n)
 if len(got)!=3{t.Fatal("defect face axes not independently declared",got)}
 for _,op:=range []string{"face.detect","face.landmarks","face.parse"}{if got[op]!=[2]string{"mps","p"}{t.Fatal("wrong defect face pins",got)}}
}
func TestBindApplyMixedAuthorityLeavesNonMPSBytesUnchanged(t *testing.T){
 raw:=[]byte(`{"retained": 1}`)
 got,err:=bindApplyMixedAuthority(context.Background(),nil,uuid.New(),uuid.New(),applyRuntimeSelection{PersonBackend:"cuda",RestorationBackend:"cuda",CreativeBackend:"cpu"},styleExecutionNeeds{},raw)
 if err!=nil||string(got)!=string(raw){t.Fatal("non-MPS path changed canonical predecessor",err)}
}
