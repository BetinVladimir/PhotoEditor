package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorTransportSealRequiresVerifiedInputs(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 _,_,task,_,err:=r.Plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 closure,err:=r.Plan.BuildInputClosure(context.Background(),task.Params,&replacementTransportSink{});if err!=nil{t.Fatal(err)}
 if err=r.SealTransport(context.Background(),closure,VerifiedReplacementDetectionInputs{});err!=v2input.ErrInvalid{t.Fatal("unverified IO reached SQL",err)}
 altered:=closure;altered.task=uuid.New()
 if err=r.SealTransport(context.Background(),altered,VerifiedReplacementDetectionInputs{});err!=v2input.ErrInvalid{t.Fatal("foreign closure reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=r.SealTransport(ctx,closure,VerifiedReplacementDetectionInputs{});err!=context.Canceled{t.Fatal("cancelled registration reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid registration reached database")}
}
