package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type bodyQualificationPolicyStub struct{report ExportBodyDependencyQualification;reads,profiles,denyAt int;change bool}
func(s *bodyQualificationPolicyStub)ReadExportBodyDependencyQualification(_ context.Context,_ ImportDocumentObjects,_ v2input.Ref,expected v2qualification.Expected)(ExportBodyDependencyQualification,error){
 s.reads++;if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="quality"{return ExportBodyDependencyQualification{},v2input.ErrInvalid};expected.RequiredChecks[0]="callback mutation"
 out:=s.report;if s.change&&s.reads>1{out.Object.VersionID="changed"};return out,nil
}
func(s *bodyQualificationPolicyStub)MatchExportBodyDependencyProfile(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,_ v2input.Entry,expected v2qualification.Expected)error{
 s.profiles++;raw[0]='!';if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="quality"{return v2input.ErrInvalid};expected.RequiredChecks[0]="callback mutation";if s.profiles==s.denyAt{return v2qualification.ErrUnqualified};return nil
}
func TestBodyDependencyPolicyOwnsOperatorConfigAndRechecks(t *testing.T){
 for _,mode:=range []string{"valid","report_changed","profile_before","profile_after","unconfigured","cancelled"}{t.Run(mode,func(t *testing.T){
  report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("qualification"))}
  expected:=v2qualification.Expected{Operation:"pose.detect",Version:"1",Backend:"mps",ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New(),RequiredChecks:[]string{"quality"},LicenseCleared:true}
  reader:=&bodyQualificationPolicyStub{report:ExportBodyDependencyQualification{Report:report,Object:objectstore.Object{VersionID:"immutable"}},change:mode=="report_changed"}
  if mode=="profile_before"{reader.denyAt=1};if mode=="profile_after"{reader.denyAt=2}
  policy,err:=NewExportBodyDependencyPolicy(reader,statsParamsObjects{[]byte(`{}`)},[]ExportBodyDependencyPolicyConfig{{Report:report,Expected:expected}});if err!=nil{t.Fatal(err)}
  expected.RequiredChecks[0]="caller mutation"
  origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"pose.detect"}};if mode=="unconfigured"{origin.Producer.Operation="face.landmarks"}
  raw:=[]byte(`{}`);ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=policy.QualifyExportBodyDependencyProfile(ctx,origin,v2input.Entry{},raw,v2input.Entry{})
  if mode=="valid"{if err!=nil||reader.reads!=2||reader.profiles!=2{t.Fatal("policy incomplete",err)}}else if err==nil{t.Fatal("unqualified profile accepted")}
  if string(raw)!="{}"{t.Fatal("profile callback mutated caller")}
  if (mode=="unconfigured"||mode=="cancelled")&&(reader.reads!=0||reader.profiles!=0){t.Fatal("invalid policy reached IO")}
 })}
}
func TestBodyDependencyProfileSQLRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.MatchExportBodyDependencyProfile(context.Background(),ExportBodyDependencyOrigin{},v2input.Entry{},nil,v2input.Entry{},v2qualification.Expected{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.MatchExportBodyDependencyProfile(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,v2input.Entry{},v2qualification.Expected{});err!=context.Canceled{t.Fatal(err)}
}
