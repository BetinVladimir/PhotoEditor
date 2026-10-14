package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type boundBodyPersonStub struct{calls int;deny bool}
func(s *boundBodyPersonStub)QualifyExportBodyPerson(_ context.Context,_ v2input.Entry,raw []byte,image ExportClosureNode)error{s.calls++;raw[0]='!';image.Body[0]='!';if s.deny{return v2input.ErrInvalid};return nil}
type boundBodyDependencyStub struct{calls int;deny bool}
func(s *boundBodyDependencyStub)QualifyExportBodyDependency(_ context.Context,_ v2input.Entry,raw []byte,node ExportClosureNode)error{s.calls++;raw[0]='!';node.Body[0]='!';if s.deny{return v2input.ErrInvalid};return nil}
type boundBodyFitStub struct{calls int}
func(s *boundBodyFitStub)QualifyExportBodyFitInputs(context.Context,v2input.Entry,[]byte,ExportClosureNode)error{s.calls++;return nil}
// Mock ports establish composition requirements, not native content validity.
func TestBoundBodyOriginPolicyRequiresAllAuthorities(t *testing.T){
 for _,mode:=range []string{"valid","person","dependency","image","producer","fit","missing_fit_authority"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,_:=bodyPersonFixture(t)
  var params map[string]any;if json.Unmarshal(raw,&params)!=nil{t.Fatal("fixture")}
  var refs struct{Poses v2input.Ref `json:"poses"`};if json.Unmarshal(raw,&refs)!=nil{t.Fatal("fixture")}
  poseBody:=[]byte(`{}`);refs.Poses.SHA256=rawSHA(poseBody);params["poses"]=refs.Poses
  var fitDoc *exportCompositionDocument
  if mode=="fit"||mode=="missing_fit_authority"{delete(params,"coefficients");fit:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))};params["fit"]=fit;fitDoc=&exportCompositionDocument{SHA:fit.SHA256,Body:[]byte(`{}`)}}
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)};parent.Ref.SHA256=rawSHA(raw)
  bundle:=exportBodyOriginBundle{Schema:1,Params:exportCompositionDocument{SHA:parent.Ref.SHA256,Body:raw},Image:exportCompositionDocument{SHA:image.Entry.Ref.SHA256,Body:image.Body},Poses:exportCompositionDocument{SHA:refs.Poses.SHA256,Body:poseBody},Fit:fitDoc}
  if mode=="image"{bundle.Image.Body=[]byte(`{}`)}
  bundleRaw,err:=canonical.Marshal(bundle);if err!=nil{t.Fatal(err)}
  source:=&exportParamsOperationStub{evidence:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"geometry.body",Version:"1",Attempt:1,Fence:1},change:mode=="producer"}
  person:=&boundBodyPersonStub{deny:mode=="person"};dependencies:=&boundBodyDependencyStub{deny:mode=="dependency"};fit:=&boundBodyFitStub{}
  policy:=BoundExportBodyOriginPolicy{Source:source,Images:&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{image.Entry.Ref.ArtifactID:image}},Person:person,Dependencies:dependencies,FitInputs:fit}
  if mode=="missing_fit_authority"{policy.FitInputs=nil}
  err=policy.QualifyExportBodyOrigin(context.Background(),source.evidence,parent,raw,bundleRaw)
  if mode=="valid"||mode=="fit"{if err!=nil||person.calls!=1||source.calls!=2{t.Fatal("bound policy incomplete",err)};expected:=1;if mode=="fit"{expected=2;if fit.calls!=1{t.Fatal("fit input authority missing")}};if dependencies.calls!=expected{t.Fatal("dependency not qualified")}}else if err==nil{t.Fatal("body authority refusal bypassed",mode)}
  if rawSHA(raw)!=parent.Ref.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256{t.Fatal("callback mutated caller evidence")}
 })}
}
