package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type exportFrameEvidenceStub struct{evidence ExportFrameEvidence;calls int;err error}
func(s *exportFrameEvidenceStub)ReadExportFrameEvidence(context.Context,v2input.SessionSnapshotItem)(ExportFrameEvidence,error){s.calls++;return s.evidence,s.err}
func TestExportFrameLineagePreservesExactFrozenAuthoredRecipe(t *testing.T){
 for _,mode:=range []string{"valid","foreign_item","portable_hash","frozen_hash","portable_substitution","content_alias","recipe_alias","source_error","cancelled"}{t.Run(mode,func(t *testing.T){
  ref:=func(label string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(label))}}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref("source"),CanvasHash:rawSHA([]byte("canvas"))};style:=ref("style")
  authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,"base_style":style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}});if err!=nil{t.Fatal(err)}
  frozen,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:binding,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},ICCLockSHA:rawSHA([]byte("icc")),ModelLockSHA:rawSHA([]byte("model"))},nil);if err!=nil{t.Fatal(err)}
  item:=v2input.SessionSnapshotItem{Ordinal:0,Binding:binding,WorkingImage:binding.Source,Recipe:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(frozen)}}
  _,snapshotRaw,err:=v2input.CompilePinnedSessionInputSnapshot(uuid.New(),[]v2input.SessionSnapshotItem{item});if err!=nil{t.Fatal(err)}
  snapshotRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(snapshotRaw)};cause:=errors.New("native evidence unavailable")
  source:=&exportFrameEvidenceStub{evidence:ExportFrameEvidence{Frame:ExportFrame{Content:ref("encoded"),PortableRecipe:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(authored)},Path:"images/one.tiff",ExternalID:"one",Rating:4,Pick:"picked"},FrozenRecipe:frozen,PortableRecipe:authored}}
  resolver,err:=NewExportLineageFrameResolver(snapshotRef,snapshotRaw,source);if err!=nil{t.Fatal(err)};ctx:=context.Background()
  switch mode{case "foreign_item":item.Binding.AssetVersionID=uuid.New();case "portable_hash":source.evidence.Frame.PortableRecipe.SHA256=rawSHA([]byte("other"));case "frozen_hash":source.evidence.FrozenRecipe=append(frozen,' ');case "portable_substitution":source.evidence.PortableRecipe=[]byte("{}");source.evidence.Frame.PortableRecipe.SHA256=rawSHA([]byte("{}"));case "content_alias":source.evidence.Frame.Content=item.WorkingImage;case "recipe_alias":source.evidence.Frame.PortableRecipe.ArtifactID=item.Recipe.ArtifactID;case "source_error":source.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  frame,err:=resolver.ResolveExportFrame(ctx,item)
  if mode=="valid"{if err!=nil||frame.PortableRecipe!=source.evidence.Frame.PortableRecipe||frame.Rating!=4{t.Fatal("valid lineage refused",err)}}else if err==nil||frame.Content.ArtifactID!=uuid.Nil{t.Fatal("invalid lineage exposed frame",err)}
  if mode=="source_error"&&!errors.Is(err,cause){t.Fatal("source cause lost")}
  if (mode=="foreign_item"||mode=="cancelled")&&source.calls!=0{t.Fatal("invalid scope read source")}
 })}
}
