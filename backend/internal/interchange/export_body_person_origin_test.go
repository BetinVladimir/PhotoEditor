package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestBodyPersonProjectionBindsActualImageAndMaskGeometry(t *testing.T){
 for _,mode:=range []string{"valid","image_hash","image_ref","canvas","selector","params_hash"}{t.Run(mode,func(t *testing.T){
  _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
  if mode=="canvas"{mask.Canvas.Width++};if mode=="selector"{mask.Selector="manual"}
  raw,err:=canonical.Marshal(map[string]any{"image":source.Ref,"poses":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("poses"))},"person":mask,"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  node:=ExportClosureNode{Entry:source,Body:imageRaw}
  if mode=="image_hash"{node.Body=[]byte(`{}`)};if mode=="image_ref"{node.Entry.Ref.ArtifactID=uuid.New()};if mode=="params_hash"{parent.Ref.SHA256=rawSHA([]byte("foreign"))}
  projection,err:=exportBodyPersonProjection(parent,raw,node)
  if mode=="valid"{if err!=nil||projection.Image.ID!=source.Ref.ArtifactID||projection.Person.Artifact!=mask.Artifact{t.Fatal("body person projection",err)}}else if err==nil{t.Fatal("foreign body person accepted")}
 })}
}
func TestBodyPersonOriginRequiresDatabaseAndContext(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if out,err:=source.LookupExportBodyPersonOrigin(context.Background(),v2input.Entry{},nil,ExportClosureNode{});err!=v2input.ErrInvalid||out!=(ExportStatsPersonOrigin{}){t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if out,err:=source.LookupExportBodyPersonOrigin(ctx,v2input.Entry{},nil,ExportClosureNode{});err!=context.Canceled||out!=(ExportStatsPersonOrigin{}){t.Fatal(err)}
}
