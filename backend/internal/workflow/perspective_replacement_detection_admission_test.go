package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorAdmissionRejectsInvalidContextBeforeDB(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=r.WasAdmitted(ctx);err!=context.Canceled{t.Fatal("cancelled acknowledgement reached SQL",err)}
 r.Snapshot=uuid.Nil
 if _,err=r.WasAdmitted(context.Background());err!=v2input.ErrInvalid{t.Fatal("missing snapshot reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid acknowledgement reached database")}
}
