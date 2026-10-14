package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type alignmentBackgroundOriginStub struct{origin ExportSeriesMaskOrigin;calls int;change bool}
func(s *alignmentBackgroundOriginStub)LookupExportAlignmentBackgroundOrigin(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,image ExportClosureNode)(ExportSeriesMaskOrigin,error){
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';image.Body[0]='!';out:=s.origin;if s.change&&s.calls>1{out.Fence++};return out,nil
}
func TestAlignmentBackgroundReaderPinsInlineAndOriginalDocuments(t *testing.T){
 for _,mode:=range []string{"valid","manifest","changed","bytes","collision","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  alignment:=v2input.Entry{Contract:"PairAlignment",Ref:ref()};after:=ref()
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":alignment.Ref,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params,err:=canonical.Marshal(map[string]any{"before":image.Entry.Ref,"after":after,"background_mask":mask});if err!=nil{t.Fatal(err)}
  registration:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pair.register",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}}
  if mode=="manifest"{mask.Confidence=.5}
  root,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  doc:=func(key string,body []byte)ExportSeriesMaskDocument{return ExportSeriesMaskDocument{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Object:objectstore.Object{Key:key,VersionID:"immutable",SHA256:rawSHA(body),Bytes:int64(len(body)),ContentType:"application/json"}}}
  source:=&alignmentBackgroundOriginStub{change:mode=="changed",origin:ExportSeriesMaskOrigin{Task:uuid.New(),Operation:"mask.person",Version:"1",Attempt:1,Fence:1,Root:doc("root",root),Params:doc("params",[]byte(`{}`)),Report:doc("report",[]byte(`{}`)),Mask:objectstore.Object{SHA256:mask.Artifact.SHA256}}}
  if mode=="collision"{source.origin.Report.Ref.ArtifactID=source.origin.Params.Ref.ArtifactID}
  bodies:=map[string][]byte{"root":root,"params":[]byte(`{}`),"report":[]byte(`{}`)};if mode=="bytes"{bodies["report"]=[]byte(`[]`)}
  reader:=ExportAlignmentBackgroundOriginReader{Source:source,Objects:seriesObjectsStub{bodies:bodies}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  origin,bundle,err:=reader.ReadExportAlignmentBackgroundOrigin(ctx,registration,parent,raw,fit,alignment,params,image)
  if mode=="valid"{if err!=nil||origin!=source.origin||len(bundle)==0||source.calls!=2{t.Fatal("original mask evidence missing",err)}}else if err==nil||bundle!=nil||origin!=(ExportSeriesMaskOrigin{}){t.Fatal("invalid original mask evidence returned",err)}
  if rawSHA(params)!=registration.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated caller")}
 })}
}
