package interchange
import (
 "context"
 "testing"
 "os"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
// Construction proves required ports/configuration only, not SQL admission.
func TestFaceBoxesAuthorityRequiresCompleteDetectorConfiguration(t *testing.T){
 for _,mode:=range []string{"valid","operation","report","reader","policy","images","documents","runtime"}{t.Run(mode,func(t *testing.T){
  report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("report"))}
  expected:=v2qualification.Expected{Operation:"face.detect",Version:"1",Backend:"mps",LicenseCleared:true,ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset"))}
  reader:=ExportBodyFitDetectorParamsReader{Source:SQLExportDocumentCatalog{},Objects:statsParamsObjects{[]byte(`{}`)}}
  var policy ExportBodyFitDetectorPolicyReader=SQLExportDocumentCatalog{}
  var images ExportClosureNodeResolver=&exportNodeResolverStub{};var documents ExportClosureNodeResolver=&exportNodeResolverStub{}
  runtime:=rawSHA([]byte("native"));switch mode{case "operation":expected.Operation="face.landmarks";case "report":report.ArtifactID=uuid.Nil;case "reader":reader.Source=nil;case "policy":policy=nil;case "images":images=nil;case "documents":documents=nil;case "runtime":runtime=""}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};probe:=&exportStyleProbeStub{runtime:runtime}
  authority,err:=NewQualifiedExportBodyFitFaceBoxes(reader,policy,documents,images,nil,report,expected,scratch,probe,runtime)
  if mode=="valid"{if err!=nil||authority==nil{t.Fatal("complete ports refused",err)}}else if err==nil||authority!=nil{t.Fatal("incomplete detector configuration accepted")}
 })}
}
func TestFaceBoxesAuthorityHonorsCancelledContext(t *testing.T){
 var authority *QualifiedExportBodyFitFaceBoxes
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=authority.QualifyExportBodyFitFaceBoxes(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{});err!=context.Canceled{t.Fatal(err)}
}

type fitDetectorOriginSequence struct{origin ExportBodyDependencyOrigin;calls int;change bool}
func(s *fitDetectorOriginSequence)LookupExportBodyFitDetectorOrigin(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,_ v2input.Entry)(ExportBodyDependencyOrigin,error){
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';out:=s.origin;if s.change&&s.calls>2{out.Producer.Fence++};return out,nil
}
type fitDetectorPolicySequence struct{report ExportBodyDependencyQualification;reads,matches,deny int;change bool}
func(s *fitDetectorPolicySequence)ReadExportBodyFitDetectorQualification(_ context.Context,_ ImportDocumentObjects,_ v2input.Ref,e v2qualification.Expected)(ExportBodyDependencyQualification,error){
 s.reads++;if len(e.RequiredChecks)!=1||e.RequiredChecks[0]!="quality"{return ExportBodyDependencyQualification{},v2input.ErrInvalid};e.RequiredChecks[0]="changed";out:=s.report;if s.change&&s.reads>1{out.Object.VersionID="changed"};return out,nil
}
func(s *fitDetectorPolicySequence)MatchExportBodyFitDetectorProfile(_ context.Context,_,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,_ v2input.Entry,e v2qualification.Expected)error{
 s.matches++;raw[0]='!';fit.Body[0]='!';params[0]='!';if len(e.RequiredChecks)!=1||e.RequiredChecks[0]!="quality"{return v2input.ErrInvalid};e.RequiredChecks[0]="changed";if s.matches==s.deny{return v2qualification.ErrUnqualified};return nil
}
// Fake Params/FaceBoxes/native receipts prove lifecycle, not schema semantics.
func TestFaceBoxesAuthorityLifecycleRechecksAllEvidence(t *testing.T){
 for _,mode:=range []string{"valid","report_changed","profile_before","profile_after","producer_changed","boxes_changed","image_changed","probe","output_size"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t);ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  landmark:=v2input.Entry{Contract:"FaceLandmarks",Ref:ref()};boxes:=v2input.Entry{Contract:"FaceBoxes",Ref:ref()}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":ref(),"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask,"before_faces":landmark.Ref,"after_faces":ref()},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params,err:=canonical.Marshal(map[string]any{"image":image.Entry.Ref,"faces":boxes.Ref});if err!=nil{t.Fatal(err)}
  landmarkOrigin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"face.landmarks",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}}
  original:=ref();source:=&fitDetectorOriginSequence{change:mode=="producer_changed",origin:ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"face.detect",Version:"1",Attempt:1,Fence:1},Params:original,ParamsObject:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:original.SHA256,Bytes:2,ContentType:"application/json"},Output:objectstore.Object{Key:"boxes",VersionID:"immutable",SHA256:boxes.Ref.SHA256,Bytes:2,ContentType:"application/json"}}}
  if mode=="output_size"{source.origin.Output.Bytes=3}
  report:=ref();policy:=&fitDetectorPolicySequence{report:ExportBodyDependencyQualification{Report:report,Object:objectstore.Object{VersionID:"immutable"}},change:mode=="report_changed"};if mode=="profile_before"{policy.deny=1};if mode=="profile_after"{policy.deny=2}
  documents:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{boxes.Ref.ArtifactID:{Entry:boxes,Body:[]byte(`{}`)}}};images:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{image.Entry.Ref.ArtifactID:image}}
  var docsResolver ExportClosureNodeResolver=documents;var imageResolver ExportClosureNodeResolver=images
  if mode=="boxes_changed"{docsResolver=fitFeatureChangingImage{documents}};if mode=="image_changed"{imageResolver=fitFeatureChangingImage{images}}
  expected:=v2qualification.Expected{Operation:"face.detect",Version:"1",Backend:"mps",LicenseCleared:true,ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),RequiredChecks:[]string{"quality"}}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("native"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  q,err:=NewQualifiedExportBodyFitFaceBoxes(ExportBodyFitDetectorParamsReader{Source:source,Objects:statsParamsObjects{[]byte(`{}`)}},policy,docsResolver,imageResolver,nil,report,expected,scratch,probe,runtime);if err!=nil{t.Fatal(err)}
  expected.RequiredChecks[0]="caller mutation"
  err=q.QualifyExportBodyFitFaceBoxes(context.Background(),landmarkOrigin,parent,raw,fit,landmark,params,image,boxes)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected detector lifecycle",err)}
  if mode=="valid"&&(source.calls!=4||policy.reads!=2||policy.matches!=2||documents.calls!=2||images.calls!=2||probe.calls!=1){t.Fatal("final proof omitted")}
  if (mode=="profile_before"||mode=="output_size")&&probe.calls!=0{t.Fatal("preflight reached native")}
  if rawSHA(params)!=landmarkOrigin.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated caller evidence")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("scratch leaked",err)}
 })}
}
