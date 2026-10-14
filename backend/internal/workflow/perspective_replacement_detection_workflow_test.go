package workflow

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorWorkflowRejectsForeignPolicyAndTransport(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 source:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 _,_,task,_,err:=source.Plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 closure,err:=source.Plan.BuildInputClosure(context.Background(),task.Params,&replacementTransportSink{});if err!=nil{t.Fatal(err)}
 policy,err:=CompilePerspectiveLinesExecutionPolicy(detector.workingICC,strings.Repeat("a",64));if err!=nil{t.Fatal(err)}
 foreign:=policy;foreign.workingICC=strings.Repeat("b",64)
 if _,err=source.CreateDraftWorkflow(context.Background(),foreign,closure);err!=v2input.ErrInvalid{t.Fatal("foreign policy reached SQL",err)}
 altered:=closure;altered.task=uuid.New()
 if _,err=source.CreateDraftWorkflow(context.Background(),policy,altered);err!=v2input.ErrInvalid{t.Fatal("foreign transport reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=source.CreateDraftWorkflow(ctx,policy,closure);err!=context.Canceled{t.Fatal("cancelled workflow reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid workflow reached database",db.calls)}
}
