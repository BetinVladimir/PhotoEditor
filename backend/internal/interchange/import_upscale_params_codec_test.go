package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestImportUpscaleRemapsSourceWithoutChangingControls(t *testing.T){
 for _,mode:=range []string{"valid","4x","model_scale","source","canvas","lock","tile","extra","policy","cancelled"}{t.Run(mode,func(t *testing.T){
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("old"))},CanvasHash:rawSHA([]byte("canvas"))}
  target:=old;target.AssetVersionID=uuid.New();target.Source=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("new"))}
  p:=importUpscaleParams{Image:old.Source,Scale:2,Model:"realesrgan_x2",Lock:rawSHA([]byte("model")),Adapter:"work_srgb_delta_v1",Tile:256,Overlap:32}
  switch mode{case "4x":p.Scale=4;p.Model="realesrgan_x4";case "model_scale":p.Scale=4;case "source":p.Image.ArtifactID=uuid.New();case "canvas":target.CanvasHash=rawSHA([]byte("other"));case "lock":p.Lock="invalid";case "tile":p.Tile=1024}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  if mode=="extra"{raw=append([]byte(`{"unknown":1,`),raw[1:]...);raw,err=canonical.Bytes(raw);if err!=nil{t.Fatal(err)}}
  sentinel:=errors.New("model revoked");policy:=&preparedLayerOperationStub{};if mode=="policy"{policy.failure=sentinel}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  codec:=ImportUpscaleParamsCodec{Qualifier:policy}
  prepared,err:=codec.RemapImportDocument(ctx,"Params",raw,old,target,nil)
  if (err==nil)!=(mode=="valid"||mode=="4x"){t.Fatal("unexpected upscale remap",err)}
  if mode=="valid"||mode=="4x"{
   actual,err:=decodeImportUpscaleParams(prepared);if err!=nil{t.Fatal(err)};expected:=p;expected.Image=target.Source
   if actual!=expected||policy.calls!=1||len(policy.versions)!=1||policy.versions[0]!="image.upscale:1"{t.Fatal("controls/operation/source changed")}
   original,err:=decodeImportUpscaleParams(raw);if err!=nil||original!=p{t.Fatal("caller bytes changed",err)}
  }else if prepared!=nil{t.Fatal("failed remap returned document")}
  if mode=="policy"&&!errors.Is(err,sentinel){t.Fatal("policy refusal lost",err)}
  if mode!="valid"&&mode!="4x"&&mode!="policy"&&policy.calls!=0{t.Fatal("invalid Params reached policy")}
 })}
}
