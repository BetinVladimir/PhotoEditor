package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestUpscaleRuntimeSelectsOnlyConfiguredScale(t *testing.T){
 x2,x4:=&preparedLayerOperationStub{},&preparedLayerOperationStub{}
 router:=importUpscaleRuntimeRouter{scales:map[int]ImportLayerOperationQualifier{2:x2,4:x4}}
 for _,scale:=range []int{2,4}{
  model:="realesrgan_x2";if scale==4{model="realesrgan_x4"}
  p:=importUpscaleParams{Image:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},Scale:scale,Model:model,Lock:rawSHA([]byte("model")),Adapter:"work_srgb_delta_v1",Tile:256,Overlap:32}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  if err:=router.QualifyImportedLayerOperation(context.Background(),"image.upscale","1",raw,v2input.SessionVersionBinding{});err!=nil{t.Fatal(err)}
  if raw[0]!='{'{t.Fatal("policy modified caller Params")}
  delete(router.scales,scale)
  if err:=router.QualifyImportedLayerOperation(context.Background(),"image.upscale","1",raw,v2input.SessionVersionBinding{});err==nil{t.Fatal("missing scale borrowed another runtime")}
 }
 if x2.calls!=1||x4.calls!=1{t.Fatal("scale runtime dispatch mixed")}
}
func TestUpscaleQualificationInstallationOwnsScaleAndLockConfig(t *testing.T){
 configured:=ImportUpscaleQualifications{2:{},4:{}}
 owned,err:=configured.owned();if err!=nil{t.Fatal(err)};delete(configured,2)
 if len(owned)!=2{t.Fatal("workflow configuration aliases caller")}
 if _,err:=(ImportUpscaleQualifications{3:{}}).owned();err==nil{t.Fatal("unsupported scale installed")}
 if _,err:=(ImportUpscaleQualifications{}).owned();err==nil{t.Fatal("empty runtime installation")}
}
