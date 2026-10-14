package interchange

import (
 "context"
 "math"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestStatsPersonDocumentsBindOriginalRootParamsAndReport(t *testing.T){
 for _,mode:=range []string{"valid","inline-confidence","root-hash","params-hash","report-hash","mask-hash","model","backend","cancelled"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:imageEntry.Ref,CanvasHash:canvas}
  model:=rawSHA([]byte("person model"))
  mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:.9,ModelLockID:&model}
  root,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  params,err:=canonical.Marshal(map[string]any{"image":binding.Source,"model_id":"lraspp_mobilenet_v3_large","model_lock_id":model,"preprocessing_id":"person_guided_v1","long_side":768,"guided_tile":512});if err!=nil{t.Fatal(err)}
  metadata,err:=v2input.ResolveSemanticSourceMetadata(binding,imageRaw);if err!=nil{t.Fatal(err)}
  scale:=768/math.Max(float64(metadata.Width),float64(metadata.Height))
  report,err:=canonical.Marshal(map[string]any{"image":binding.Source,"mask":v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256},"preview_width":int(math.Max(1,math.RoundToEven(float64(metadata.Width)*scale))),"preview_height":int(math.Max(1,math.RoundToEven(float64(metadata.Height)*scale))),"model_id":"lraspp_mobilenet_v3_large","model_lock_id":model,"backend":"mps","preprocessing_id":"person_guided_v1","person_class":15,"confidence":.9,"person_fraction":.2,"source_clip_fraction":0,"alpha_excluded_fraction":0,"warnings":[]string{}});if err!=nil{t.Fatal(err)}
  origin:=ExportStatsPersonOrigin{Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(root)},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)},Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(report)},Mask:objectstore.Object{SHA256:mask.Artifact.SHA256}}
  backend:="mps";ctx:=context.Background()
  switch mode{case "inline-confidence":mask.Confidence=.8;case "root-hash":origin.Root.SHA256=rawSHA([]byte("foreign"));case "params-hash":origin.Params.SHA256=rawSHA([]byte("foreign"));case "report-hash":origin.Report.SHA256=rawSHA([]byte("foreign"));case "mask-hash":origin.Mask.SHA256=rawSHA([]byte("foreign"));case "model":model=rawSHA([]byte("different"));case "backend":backend="cuda";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  stats,err:=canonical.Marshal(review.StatsParams{Image:review.ArtifactRef{ID:binding.Source.ArtifactID,SHA256:binding.Source.SHA256},Person:mask});if err!=nil{t.Fatal(err)}
  err=VerifyExportStatsPersonDocuments(ctx,origin,ExportStatsPersonDocuments{Root:root,Params:params,Report:report},stats,imageRaw,binding,model,backend)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected person content binding",mode,err)}
 })}
}
