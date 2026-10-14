package workflow
import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorPreparationRejectsInvalidPolicyAndWitnessBeforeSQL(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 policy,err:=CompilePerspectiveLinesExecutionPolicy(detector.workingICC,strings.Repeat("a",64));if err!=nil{t.Fatal(err)}
 if _,err=r.PrepareAndActivate(context.Background(),PerspectiveLinesExecutionPolicy{},nil,nil);err!=v2input.ErrInvalid{t.Fatal("zero policy reached SQL",err)}
 foreign:=policy;foreign.workingICC=strings.Repeat("b",64)
 if _,err=r.PrepareAndActivate(context.Background(),foreign,nil,nil);err!=v2input.ErrInvalid{t.Fatal("foreign policy reached SQL",err)}
 if _,err=r.sealedForActivation(context.Background(),policy,VerifiedReplacementDetectionInputs{});err!=v2input.ErrInvalid{t.Fatal("unverified seal probe reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=r.PrepareAndActivate(ctx,policy,nil,nil);err!=context.Canceled{t.Fatal("cancelled preparation reached SQL",err)}
 if db.calls!=0{t.Fatal("invalid preparation reached database")}
}
