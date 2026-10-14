package interchange
import("context";"errors";"testing";"time";"github.com/google/uuid";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/objectstore";"photoeditor/backend/internal/v2input";"photoeditor/backend/internal/v2qualification")
func TestImportPersonMaskRuntimeReportReadsActualPinnedBytesAndRechecksAuthority(t *testing.T){
 for _,mode:=range []string{"valid","bytes","model","failed_check","version","revoked","mime","cancelled","policy_ownership"}{t.Run(mode,func(t *testing.T){
  hash:=func(s string)string{return rawSHA([]byte(s))}
  fixture,evidence:=hash("fixture"),hash("evidence")
  report:=v2qualification.Report{SchemaVersion:1,Operation:"mask.person",Version:"1",ModelLockSHA:hash("model"),RuntimeLockSHA:hash("runtime"),DeviceProfileSHA:hash("device"),DatasetSHA:hash("dataset"),InputFixtureSHAs:[]string{fixture},Checks:[]v2qualification.Check{{ID:"quality",Passed:true,EvidenceSHA:evidence}},Metrics:v2qualification.Metrics{P50MS:1,P95MS:2},Status:"passed",IssuerID:uuid.New(),CreatedAt:time.Date(2026,10,11,0,0,0,0,time.UTC)}
  expected:=v2qualification.Expected{Operation:report.Operation,Version:report.Version,Backend:"mps",ModelLockSHA:report.ModelLockSHA,RuntimeLockSHA:report.RuntimeLockSHA,DeviceProfileSHA:report.DeviceProfileSHA,DatasetSHA:report.DatasetSHA,IssuerID:report.IssuerID,RequiredChecks:[]string{"quality"},TestFixtureSHAs:map[string]struct{}{fixture:{}},EvidenceSHAs:map[string]struct{}{evidence:{}},LicenseCleared:true}
  if mode=="model"{report.ModelLockSHA=hash("foreign")};if mode=="failed_check"{report.Checks[0].Passed=false}
  body,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  policy,err:=canonical.Marshal(expected);if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
  object:=objectstore.Object{Key:"qualification",VersionID:"v1",SHA256:ref.SHA256,Bytes:int64(len(body)),ContentType:"application/json"}
  reader:=&importReaderStub{body:append([]byte(nil),body...)};if mode=="bytes"{reader.body[0]='!'};if mode=="mime"{object.ContentType="text/plain"}
  calls:=0;sentinel:=errors.New("qualification revoked")
  lookup:=func()(objectstore.Object,error){calls++;current:=object;if mode=="version"&&calls==2{current.VersionID="v2"};if mode=="revoked"&&calls==2{return objectstore.Object{},sentinel};if mode=="policy_ownership"&&calls==1{for i:=range policy{policy[i]='!'}};return current,nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=qualifyImportRestorationRuntimeReport(ctx,"mask.person",reader,ref,policy,lookup)
  valid:=mode=="valid"||mode=="policy_ownership"
  if (err==nil)!=valid{t.Fatal("unexpected physical report result",err)}
  if valid&&(calls!=2||reader.opens!=1){t.Fatal("physical report or final SQL check missing")}
  if mode=="revoked"&&!errors.Is(err,sentinel){t.Fatal("current authority refusal lost",err)}
  if mode=="mime"||mode=="cancelled"{if reader.opens!=0{t.Fatal("invalid metadata read object")}}
 })}
}
