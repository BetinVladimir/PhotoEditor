package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorActivationRequiresVerifiedInputs(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 if err=r.Activate(context.Background(),VerifiedReplacementDetectionInputs{});err!=v2input.ErrInvalid{t.Fatal("unverified activation reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=r.Activate(ctx,VerifiedReplacementDetectionInputs{});err!=context.Canceled{t.Fatal("cancelled activation reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid activation reached database")}
}
