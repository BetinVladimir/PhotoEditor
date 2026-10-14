package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestReplacementDetectorTransportAdmissionRejectsTamperedClosureBeforeSQL(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 source:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 _,_,task,_,err:=source.Plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 closure,err:=source.Plan.BuildInputClosure(context.Background(),task.Params,&replacementTransportSink{});if err!=nil{t.Fatal(err)}
 altered:=closure;altered.task=uuid.New()
 if err=source.ConfirmInputClosure(context.Background(),altered);err!=v2input.ErrInvalid{t.Fatal("foreign closure reached SQL",err)}
 altered=closure;altered.built.Count++
 if err=source.ConfirmInputClosure(context.Background(),altered);err!=v2input.ErrInvalid{t.Fatal("wrong count reached SQL",err)}
 altered=closure;altered.documents=closure.Documents();altered.documents[closure.built.Manifest.ArtifactID]=[]byte(`{}`)
 if err=source.ConfirmInputClosure(context.Background(),altered);err!=v2input.ErrInvalid{t.Fatal("altered manifest reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=source.ConfirmInputClosure(ctx,closure);err!=context.Canceled{t.Fatal("cancelled confirmation reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid closure reached database",db.calls)}
}
