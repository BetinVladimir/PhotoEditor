package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorAdoptionTransactionsRejectInvalidAttemptBeforeBegin(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 for _,attempt:=range []int{0,7}{
  if _,err=r.ReadAdoptionEvidence(context.Background(),attempt,1,uuid.New());err!=v2input.ErrInvalid{t.Fatal("invalid evidence attempt reached database",err)}
  if err=r.CommitReplayedAdoption(context.Background(),attempt,1,uuid.New(),nil,"",nil,nil);err!=v2input.ErrInvalid{t.Fatal("invalid commit attempt reached database",err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=r.ReadAdoptionEvidence(ctx,1,1,uuid.New());err!=context.Canceled{t.Fatal("cancelled evidence accepted",err)}
 if err=r.CommitReplayedAdoption(ctx,1,1,uuid.New(),nil,"",nil,nil);err!=context.Canceled{t.Fatal("cancelled commit accepted",err)}
 if db.calls!=0{t.Fatal("invalid adoption began transaction")}
}
