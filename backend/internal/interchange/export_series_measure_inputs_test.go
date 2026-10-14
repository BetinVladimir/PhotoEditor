package interchange

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestSeriesMeasureInputRefsPreserveWorkerOrderAndGeometry(t *testing.T){
 for _,mode:=range []string{"valid","protected","canvas","hash","source"}{t.Run(mode,func(t *testing.T){
  _,entry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
  if mode=="protected"{mask.Protected=true};if mode=="canvas"{mask.Canvas.Width++}
  source:=entry.Ref;if mode=="source"{source.ArtifactID=uuid.New()}
  raw,err:=canonical.Marshal(map[string]any{"image":source,"regions":[]any{map[string]any{"mask":mask},map[string]any{"mask":mask}}});if err!=nil{t.Fatal(err)}
  origin:=ExportSeriesMeasureOrigin{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}};if mode=="hash"{origin.Params.SHA256=rawSHA([]byte("other"))}
  refs,err:=exportSeriesMeasureInputRefs(origin,raw,ExportClosureNode{Entry:entry,Body:imageRaw})
  if mode!="valid"{if err==nil||refs!=nil{t.Fatal("invalid Params input inventory accepted",mode,err)};return}
  expected:=4;if image.Alpha!=nil{expected++}
  if err!=nil||len(refs)!=expected||refs[0]!=entry.Ref||refs[1].ArtifactID!=image.Pixels.ID||refs[len(refs)-1].ArtifactID!=mask.Artifact.ID||refs[len(refs)-2]!=refs[len(refs)-1]{t.Fatal("worker input order/duplicates changed",refs,err)}
 })}
}
