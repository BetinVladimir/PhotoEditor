package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestReplacementDetectedLinesRejectsCancelledObservationBeforeDB(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 out,err:=r.ReadDetectedLines(ctx)
 if err!=context.Canceled||out.ResultID!=uuid.Nil||db.calls!=0{t.Fatal("cancelled observation reached DB",err)}
}
func TestReplacementDetectedLinesMissingDBDoesNotReturnEvidence(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 out,err:=r.ReadDetectedLines(context.Background())
 if err==nil||out.ResultID!=uuid.Nil{t.Fatal("missing source returned evidence",out,err)}
}
func TestReplacementDetectedLinesRequiresCallerTransactionAndOwnedEvidence(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.LockDetectedLines(context.Background(),nil);err==nil{t.Fatal("missing transaction accepted")}
 var locked LockedReplacementDetectedLines
 if _,_,err:=locked.LinesDocument();err==nil{t.Fatal("unproven segments compiled")}
 if locked.Root().ArtifactID!=uuid.Nil||locked.Status()!=""{t.Fatal("zero evidence returned")}
 raw:=locked.ResultBytes();raw=append(raw,'x')
 if len(locked.ResultBytes())!=0{t.Fatal("owned bytes mutated")}
}
