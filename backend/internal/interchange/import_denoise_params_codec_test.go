package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestImportDenoiseRemapsSourceWithoutChangingControls(t *testing.T){
 for _,mode:=range []string{"valid","source","canvas","lock","tile","extra","policy","cancelled"}{t.Run(mode,func(t *testing.T){
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("old"))},CanvasHash:rawSHA([]byte("canvas"))}
  target:=old;target.AssetVersionID=uuid.New();target.Source=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("new"))}
  p:=importDenoiseParams{Image:old.Source,Strength:.75,Model:"scunet_color_real_psnr",Lock:rawSHA([]byte("model")),Adapter:"work_srgb_delta_v1",Tile:512,Overlap:64}
  switch mode{case "source":p.Image.ArtifactID=uuid.New();case "canvas":target.CanvasHash=rawSHA([]byte("other"));case "lock":p.Lock="invalid";case "tile":p.Tile=1024}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  if mode=="extra"{raw=append([]byte(`{"unknown":1,`),raw[1:]...);raw,err=canonical.Bytes(raw);if err!=nil{t.Fatal(err)}}
  sentinel:=errors.New("model revoked");policy:=&preparedLayerOperationStub{};if mode=="policy"{policy.failure=sentinel}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  codec:=ImportDenoiseParamsCodec{Qualifier:policy}
  prepared,err:=codec.RemapImportDocument(ctx,"Params",raw,old,target,nil)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected denoise remap",err)}
  if mode=="valid"{
   actual,err:=decodeImportDenoiseParams(prepared);if err!=nil{t.Fatal(err)};expected:=p;expected.Image=target.Source
   if actual!=expected||policy.calls!=1||len(policy.versions)!=1||policy.versions[0]!="image.denoise:1"{t.Fatal("controls/operation/source changed")}
   original,err:=decodeImportDenoiseParams(raw);if err!=nil||original!=p{t.Fatal("caller bytes changed",err)}
  }else if prepared!=nil{t.Fatal("failed remap returned document")}
  if mode=="policy"&&!errors.Is(err,sentinel){t.Fatal("policy refusal lost",err)}
  if mode!="valid"&&mode!="policy"&&policy.calls!=0{t.Fatal("invalid Params reached policy")}
 })}
}
