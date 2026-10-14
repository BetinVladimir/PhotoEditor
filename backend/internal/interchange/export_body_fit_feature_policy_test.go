package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type fitFeaturePolicyReaderStub struct{report ExportBodyDependencyQualification;reads,profiles,deny int;change bool}
func(s *fitFeaturePolicyReaderStub)ReadExportBodyDependencyQualification(_ context.Context,_ ImportDocumentObjects,_ v2input.Ref,expected v2qualification.Expected)(ExportBodyDependencyQualification,error){
 s.reads++;if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="quality"{return ExportBodyDependencyQualification{},v2input.ErrInvalid};expected.RequiredChecks[0]="changed"
 out:=s.report;if s.change&&s.reads>1{out.Object.VersionID="changed"};return out,nil
}
func(s *fitFeaturePolicyReaderStub)MatchExportBodyFitFeatureProfile(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,expected v2qualification.Expected)error{
 s.profiles++;raw[0]='!';fit.Body[0]='!';if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="quality"{return v2input.ErrInvalid};expected.RequiredChecks[0]="changed";if s.profiles==s.deny{return v2qualification.ErrUnqualified};return nil
}
type fitFeatureInputPolicyStub struct{calls,deny int}
func(s *fitFeatureInputPolicyStub)QualifyExportBodyFitFeatureOriginal(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte)error{
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';if s.calls==s.deny{return v2input.ErrInvalid};return nil
}
func TestFitFeaturePolicyRechecksReportProfileAndOriginalInputs(t *testing.T){
 for _,mode:=range []string{"valid","report_changed","profile_before","profile_after","inputs_before","inputs_after","parent_hash","foreign_alignment","params_hash","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  alignment:=v2input.Entry{Contract:"PoseSet",Ref:ref()}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":ref(),"before_pose":alignment.Ref,"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  report:=ref();expected:=v2qualification.Expected{Operation:"pose.detect",Version:"1",Backend:"mps",LicenseCleared:true,ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New(),RequiredChecks:[]string{"quality"}}
  reader:=&fitFeaturePolicyReaderStub{report:ExportBodyDependencyQualification{Report:report,Object:objectstore.Object{VersionID:"immutable"}},change:mode=="report_changed"}
  inputs:=&fitFeatureInputPolicyStub{}
  if mode=="profile_before"{reader.deny=1};if mode=="profile_after"{reader.deny=2};if mode=="inputs_before"{inputs.deny=1};if mode=="inputs_after"{inputs.deny=2}
  faceExpected:=expected;faceExpected.Operation="face.landmarks"
  policy,err:=NewExportBodyFitFeaturePolicy(reader,statsParamsObjects{[]byte(`{}`)},inputs,[]ExportBodyDependencyPolicyConfig{{Report:report,Expected:expected},{Report:ref(),Expected:faceExpected}});if err!=nil{t.Fatal(err)}
  expected.RequiredChecks[0]="caller mutation"
  params:=[]byte(`{}`);origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pose.detect",Version:"1"},Params:ref()}
  if mode=="parent_hash"{parent.Ref.SHA256=rawSHA([]byte("foreign"))};if mode=="foreign_alignment"{alignment.Ref=ref()};if mode=="params_hash"{origin.Params.SHA256=rawSHA([]byte("foreign"))}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=policy.QualifyExportBodyFitFeatureProfile(ctx,origin,parent,raw,fit,alignment,params)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected registration admission",err)}
  if mode=="valid"&&(reader.reads!=2||reader.profiles!=2||inputs.calls!=2){t.Fatal("missing final gates")}
  if (mode=="parent_hash"||mode=="foreign_alignment"||mode=="params_hash"||mode=="cancelled")&&reader.reads!=0{t.Fatal("invalid scope reached report IO")}
  if string(params)!="{}"||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated caller evidence")}
 })}
}
