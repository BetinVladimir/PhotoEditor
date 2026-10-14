package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorAdoptionEvidenceRequiresTransaction(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 if _,err=r.AdoptionEvidence(context.Background(),nil,1,1,uuid.New());err!=v2input.ErrInvalid{t.Fatal("missing transaction accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=r.AdoptionEvidence(ctx,nil,1,1,uuid.New());err!=context.Canceled{t.Fatal("cancelled evidence accepted",err)}
 if db.calls!=0{t.Fatal("evidence opened independent transaction")}
}
