package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorPublicationPacketOwnsBytesAndTypedInputs(t *testing.T){
 value,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
 plan:=PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}
 scope,body,task,entries,err:=plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 if scope.Tenant!=value.Tenant||scope.Run!=value.Run||scope.Owner!=value.Task||scope.Purpose!="task_params"||task.ID!=value.Task||task.Operation!="optical.perspective_lines"||task.Params.ID!=uuid.NewSHA1(value.Task,[]byte("params"))||string(body)!=value.ParamsJCS{t.Fatal("publication packet drift")}
 params,source:=0,0
 for _,entry:=range entries{
  if entry.Contract=="PerspectiveLineDetectionParams"&&entry.Ref.ArtifactID==task.Params.ID{params++}
  if entry.Contract=="WorkingImage"&&entry.Ref==value.Base.Source{source++}
 }
 if params!=1||source!=1{t.Fatal("typed authority roots missing")}
 body[0]='x';entries[0].Contract="changed";task.Inputs[0].SHA256="changed"
 _,owned,_,_,err:=plan.PreparePublication(context.Background());if err!=nil||string(owned)!=value.ParamsJCS{t.Fatal("publication memory escaped",err)}
 plan.detector.scope.Run=uuid.New()
 if _,_,_,_,err:=plan.PreparePublication(context.Background());err!=v2input.ErrInvalid{t.Fatal("mismatched plan and descriptor accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,_,_,err:=plan.PreparePublication(ctx);err!=context.Canceled{t.Fatal("cancelled publication compiled",err)}
}
