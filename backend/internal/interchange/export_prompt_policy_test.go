package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type promptPolicyReaderStub struct{qualification ExportPromptQualification;calls int;profileCalls int;change bool;deny bool;denyAfter int;cancel context.CancelFunc}
func(s *promptPolicyReaderStub)ReadExportPromptQualification(_ context.Context,_ ImportDocumentObjects,_ v2input.Ref,expected v2qualification.Expected)(ExportPromptQualification,error){s.calls++;if s.cancel!=nil&&s.calls==2{s.cancel()};if len(expected.RequiredChecks)>0{expected.RequiredChecks[0]="mutated"};out:=s.qualification;if s.change&&s.calls>1{out.Object.VersionID="changed"};return out,nil}
func(s *promptPolicyReaderStub)QualifyExportPromptProfile(_ context.Context,_ ExportSeriesMaskOrigin,_ v2input.SessionVersionBinding,expected v2qualification.Expected)error{s.profileCalls++;if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="quality"{return v2input.ErrInvalid};if s.deny||(s.denyAfter>0&&s.profileCalls>s.denyAfter){return v2input.ErrInvalid};return nil}
func TestPromptMaskPolicyOwnsLocksAndRechecksQualification(t *testing.T){
 for _,mode:=range []string{"valid","changed","profile","source","revoked_after_report","cancelled_after_report"}{t.Run(mode,func(t *testing.T){
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))};report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("qualification"))}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref,CanvasHash:rawSHA([]byte("canvas"))}
  expected:=v2qualification.Expected{Operation:"mask.prompt",Version:"1",Backend:"mps",ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New(),RequiredChecks:[]string{"quality"},LicenseCleared:true}
  reader:=&promptPolicyReaderStub{qualification:ExportPromptQualification{Report:report,Object:objectstore.Object{VersionID:"immutable"}},change:mode=="changed",deny:mode=="profile"}
  policy,err:=NewExportPromptMaskPolicy(reader,statsParamsObjects{[]byte(`{}`)},[]v2input.SessionVersionBinding{binding},report,expected);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  if mode=="revoked_after_report"{reader.denyAfter=1}
  if mode=="cancelled_after_report"{reader.cancel=cancel}
  expected.RequiredChecks[0]="caller mutation"
  source:=ref;if mode=="source"{source.ArtifactID=uuid.New()}
  evidence,err:=policy.ResolveExportSeriesMaskPolicy(ctx,ExportSeriesMaskOrigin{Operation:"mask.prompt",Version:"1"},source)
  if mode=="valid"{if err!=nil||evidence.Binding!=binding||evidence.ModelID!="mobilesam"||evidence.Backend!="mps"||reader.calls!=2||reader.profileCalls!=2{t.Fatal(err)}}else if err==nil||evidence!=(ExportSeriesMaskPolicyEvidence{}){t.Fatal("unqualified prompt policy returned",mode,err)}
 })}
}
func TestPromptProfileRequiresDatabase(t *testing.T){
 if err:=(SQLExportDocumentCatalog{}).QualifyExportPromptProfile(context.Background(),ExportSeriesMaskOrigin{},v2input.SessionVersionBinding{},v2qualification.Expected{});err!=v2input.ErrInvalid{t.Fatal(err)}
}
