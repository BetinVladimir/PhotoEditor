package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type seriesMaskOriginStub struct{origin ExportSeriesMaskOrigin;calls int;change bool}
func(s *seriesMaskOriginStub)LookupExportSeriesMaskOrigin(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Ref)(ExportSeriesMaskOrigin,error){s.calls++;out:=s.origin;if s.change&&s.calls>1{out.Fence++};return out,nil}
func TestSeriesMaskOriginReaderRequiresExactInlineManifestAndStableProducer(t *testing.T){
 for _,mode:=range []string{"valid","manifest","changed","bytes"}{t.Run(mode,func(t *testing.T){
  _,entry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("mask"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
  raw,err:=canonical.Marshal(map[string]any{"image":entry.Ref,"regions":[]any{map[string]any{"mask":mask}}});if err!=nil{t.Fatal(err)}
  measure:=ExportSeriesMeasureOrigin{Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  if mode=="manifest"{mask.Confidence=.5}
  root,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  doc:=func(key string,body []byte)ExportSeriesMaskDocument{return ExportSeriesMaskDocument{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Object:objectstore.Object{Key:key,VersionID:"immutable",SHA256:rawSHA(body),Bytes:int64(len(body)),ContentType:"application/json"}}}
  source:=&seriesMaskOriginStub{origin:ExportSeriesMaskOrigin{Task:uuid.New(),Operation:"mask.person",Version:"1",Attempt:1,Fence:1,Root:doc("root",root),Params:doc("params",[]byte(`{}`)),Report:doc("report",[]byte(`{}`)),Mask:objectstore.Object{SHA256:mask.Artifact.SHA256}},change:mode=="changed"}
  bodies:=map[string][]byte{"root":root,"params":[]byte(`{}`),"report":[]byte(`{}`)};if mode=="bytes"{bodies["report"]=[]byte(`[]`)}
  reader:=ExportSeriesMaskOriginReader{Source:source,Objects:seriesObjectsStub{bodies:bodies}}
  origin,bundle,err:=reader.ReadExportSeriesMaskOrigin(context.Background(),measure,v2input.Entry{},raw,ExportClosureNode{Entry:entry,Body:imageRaw},v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256})
  if mode=="valid"{if err!=nil||origin!=source.origin||len(bundle)==0||source.calls!=2{t.Fatal(err)}}else if err==nil||bundle!=nil||origin!=(ExportSeriesMaskOrigin{}){t.Fatal("unqualified mask documents returned",mode,err)}
 })}
}
func TestSeriesMaskOriginSQLRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.LookupExportSeriesMaskOrigin(context.Background(),ExportSeriesMeasureOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Ref{});err!=v2input.ErrInvalid{t.Fatal(err)}
}
