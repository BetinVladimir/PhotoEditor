package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestResolvedSourceScopeUsesActualFrozenSourceBeforeFallback(t *testing.T){
 for _,mode:=range []string{"valid","foreign","canvas","node","override","mask-valid","mask-canvas"}{t.Run(mode,func(t *testing.T){
  _,sourceEntry,imageRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:sourceEntry.Ref,CanvasHash:canvas}
  params:=map[string]any{"image":sourceEntry.Ref}
  if mode=="foreign"{params["image"]=v2input.Ref{ArtifactID:uuid.New(),SHA256:sourceEntry.Ref.SHA256}}
  if mode=="canvas"{binding.CanvasHash=rawSHA([]byte("foreign"))}
  if mode=="override"{params["override"]=map[string]any{"schema_version":1,"input_sha256":rawSHA([]byte("foreign")),"modules":map[string]any{"strength":.5},"revision":1}}
  if mode=="mask-valid"||mode=="mask-canvas"{
   mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}
   if mode=="mask-canvas"{mask.Canvas.Width++}
   params["override"]=map[string]any{"schema_version":1,"input_sha256":image.Canvas.SourceSHA256,"modules":map[string]any{"strength":.5},"mask_refs":[]any{map[string]any{"binding_id":"repair-1","purpose":"inpaint_target","mask":mask,"confirmed":true,"strength":1}},"revision":1}
  }
  node:=ExportClosureNode{Entry:sourceEntry,Body:imageRaw};if mode=="node"{node.Entry.Contract="Params"}
  other:=&resolvedOriginOtherSpy{}
  scope:=ExportResolvedSourceScope{Sources:&exportMaskNodeStub{node:node},Bindings:[]v2input.SessionVersionBinding{binding},Other:other}
  raw,err:=canonical.Marshal(map[string]any{"params":params});if err!=nil{t.Fatal(err)}
  _,err=scope.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"},raw)
  if (err==nil)!=(mode=="valid"||mode=="mask-valid"){t.Fatal("unexpected source scope",mode,err)}
  expected:=0;if mode=="valid"||mode=="mask-valid"{expected=1};if other.calls!=expected{t.Fatal("invalid source reached fallback")}
 })}
}
