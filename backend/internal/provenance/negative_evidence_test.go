package provenance

import (
 "context"
 "errors"
 "encoding/json"
 "os"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "testing"

 "photoeditor/backend/internal/auth"
)

func TestNegativeEvidenceRejectsMissingDependenciesAndCancellation(t *testing.T){
 reader:=BoundSigningNegativeEvidenceReader{}
 if _,err:=reader.ReadSigningNegativeEvidence(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err==nil{t.Fatal("unconfigured reader accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.ReadSigningNegativeEvidence(ctx,auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});!errors.Is(err,context.Canceled){t.Fatal("cancelled read lost cancellation",err)}
}

func TestNegativePixelDigestIncludesPrivateEvidence(t *testing.T){
 original:=SigningNegativePixelEvidence{taskDigest:"task"}
 before,err:=signingNegativePixelDigest(original);if err!=nil{t.Fatal(err)}
 for _,field:=range []string{"task","count","source","output"}{
  changed:=original
  switch field{case "task":changed.taskDigest="other";case "count":changed.changedPixels=1;case "source":changed.source.Canvas.Width=1;case "output":changed.image.Canvas.Width=1}
  after,err:=signingNegativePixelDigest(changed);if err!=nil||after==before{t.Fatal("private evidence omitted from fingerprint",field,err)}
 }
}

func TestNegativeEvidenceStampBindsExactHumanDecision(t *testing.T){
 raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin};if json.Unmarshal(raw,&fixture)!=nil{t.Fatal("bad fixture")}
 task:=CanonicalSigningTask{Task:uuid.New(),Result:uuid.New(),Operation:"optical.negative",Version:"1",ReviewRequired:true}
 taskSHA,err:=canonical.Hash(task);if err!=nil{t.Fatal(err)}
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:fixture.Pin.Report.SHA256}
 report:=fixture.Pin.Report
 original:=SigningNegativeEvidence{taskDigest:taskSHA,qualification:fixture.Pin,pixels:SigningNegativePixelEvidence{taskDigest:taskSHA,root:root},review:SigningNegativeReview{ID:uuid.New(),Task:task.Task,Result:task.Result,Root:root,Report:report,Accepted:true}}
 original.pixels.params.Calibration=fixture.Pin.Calibration;original.pixels.result.Report=report
 before,err:=compileSigningNegativeEvidenceStamp(task,original);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"reject","missing_review","task","result","root","report","calibration"}{
  changed:=original
  switch mode{case "reject":changed.review.Accepted=false;case "missing_review":changed.review.ID=uuid.Nil;case "task":changed.review.Task=uuid.New();case "result":changed.review.Result=uuid.New();case "root":changed.review.Root.ArtifactID=uuid.New();case "report":changed.review.Report.ArtifactID=uuid.New();case "calibration":changed.pixels.params.Calibration.ArtifactID=uuid.New()}
  if _,err:=compileSigningNegativeEvidenceStamp(task,changed);err==nil{t.Fatal("mismatched human decision accepted",mode)}
 }
 changed:=original;changed.review.ID=uuid.New();after,err:=compileSigningNegativeEvidenceStamp(task,changed);if err!=nil||after==before{t.Fatal("review identity omitted",err)}
}
