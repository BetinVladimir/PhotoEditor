package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestResolvedInlineMaskGeometryRequiresTypedEdgeAndOrigin(t *testing.T){
 for _,mode:=range []string{"valid","edge","hash","missing-origin","foreign"}{t.Run(mode,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}
  raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"override":map[string]any{"mask_refs":[]any{map[string]any{"mask":mask}}}}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"};children,err:=exportResolvedChildren(parent,raw);if err!=nil{t.Fatal(err)}
  if mode=="edge"{children=nil};node:=ExportClosureNode{Entry:parent,Body:raw,Children:children};if mode=="hash"{node.Body=[]byte("{}")}
  origin:=&resolvedOriginSequence{};source:=ExportResolvedMaskGeometrySource{Reader:&exportMaskNodeStub{node:node},Origin:origin,Parents:[]v2input.Entry{parent}}
  if mode=="missing-origin"{source.Origin=nil}
  ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256};if mode=="foreign"{ref.ArtifactID=uuid.New()}
  geometry,err:=source.ResolveExportMaskGeometry(context.Background(),ref)
  if mode=="valid"{if err!=nil||geometry.Width!=int(image.Canvas.Width)||origin.calls!=1{t.Fatal("inline geometry",err)}}else if err==nil{t.Fatal("unqualified inline geometry accepted")}
  all:=ExportAllMaskGeometrySource{Reader:source.Reader,Parents:source.Parents,ResolvedOrigin:source.Origin}
  combined,combinedErr:=all.ResolveExportMaskGeometry(context.Background(),ref)
  if mode=="valid"{if combinedErr!=nil||combined!=geometry||origin.calls!=2{t.Fatal("combined inline dispatch",combinedErr)}}else if combinedErr==nil{t.Fatal("combined geometry bypassed refusal")}
  if mode=="valid"{all.Parents=append(all.Parents,parent);if _,err:=all.ResolveExportMaskGeometry(context.Background(),ref);err==nil{t.Fatal("duplicate parent accepted")}}
 })}
}

func TestOverrideInlineMaskGeometryUsesQualifiedReaderWithoutResolvedProducer(t *testing.T){
 _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}
 body,err:=canonical.Marshal(map[string]any{"mask_refs":[]any{map[string]any{"mask":mask}}});if err!=nil{t.Fatal(err)}
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"Override"}
 ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
 reader:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:body,Children:[]v2input.Entry{{Ref:ref,Contract:"MaskPixels"}}}}
 // Mock reader stands for full native/scoped Override validation; this test
 // covers binary geometry routing only, not Override schema admission.
 source:=ExportAllMaskGeometrySource{Reader:reader,Parents:[]v2input.Entry{parent}}
 geometry,err:=source.ResolveExportMaskGeometry(context.Background(),ref)
 if err!=nil||geometry.Width!=int(image.Canvas.Width){t.Fatal("override geometry missing",err)}
 reader.node.Children[0].Contract="WorkingPixels"
 if _,err:=source.ResolveExportMaskGeometry(context.Background(),ref);err==nil{t.Fatal("relabeled mask accepted")}
}
