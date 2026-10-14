package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/semanticproposal"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type exportQualifiedPersonStub struct{t *testing.T;mask semanticproposal.VerifiedPersonMask;calls int;deny bool}
func(s *exportQualifiedPersonStub)ReadQualifiedPersonEvidence(_ context.Context,_ auth.Identity,_ uuid.UUID,_ v2input.SessionVersionBinding,_ v2input.Ref,_ v2input.Ref,expected v2qualification.Expected)(semanticproposal.VerifiedPersonMask,error){
 s.calls++;if len(expected.RequiredChecks)!=1||expected.RequiredChecks[0]!="coverage"{s.t.Fatal("shared policy mutated")};expected.RequiredChecks[0]="changed"
 if s.deny{return semanticproposal.VerifiedPersonMask{},v2qualification.ErrUnqualified};return s.mask,nil
}
func TestExportStatsPersonPolicyUsesQualifiedReaderAndOwnsPolicy(t *testing.T){
 for _,mode:=range []string{"valid","denied","foreign","hash","cancelled"}{t.Run(mode,func(t *testing.T){
  _,entry,body:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(body);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  model:=rawSHA([]byte("model"));mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:.9,ModelLockID:&model};rootBody,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  origin:=ExportStatsPersonOrigin{Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(rootBody)}};origin.Mask.SHA256=image.Pixels.SHA256
  reader:=&exportQualifiedPersonStub{t:t,mask:semanticproposal.VerifiedPersonMask{Root:origin.Root,Pixels:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Body:rootBody},deny:mode=="denied"}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas};report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("qualification"))}
  expected:=v2qualification.Expected{Operation:"mask.person",Version:"1",Backend:"mps",ModelLockSHA:model,RuntimeLockSHA:model,DeviceProfileSHA:model,DatasetSHA:model,LicenseCleared:true,RequiredChecks:[]string{"coverage"}}
  policy,err:=NewExportStatsPersonPolicy(reader,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),[]v2input.SessionVersionBinding{binding},report,expected);if err!=nil{t.Fatal(err)}
  expected.RequiredChecks[0]="outside mutation"
  source:=entry.Ref;ctx:=context.Background();switch mode{case "foreign":source.ArtifactID=uuid.New();case "hash":reader.mask.Body=[]byte("{}");case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  for i:=0;i<2;i++{out,err:=policy.ResolveExportStatsPersonPolicy(ctx,origin,source);if mode=="valid"{if err!=nil||out.QualificationSHA!=report.SHA256||out.Binding!=binding||out.ModelLock!=model||out.Backend!="mps"{t.Fatal("qualified reader path",err)}}else if err==nil{t.Fatal("unqualified policy accepted")}}
 })}
}
