package workflow
import("context";"github.com/google/uuid";"photoeditor/backend/internal/canonical";"strings";"testing";"photoeditor/backend/internal/v2optical";"photoeditor/backend/internal/v2input")
func TestReplacementDetectionPlanRejectsUnprovedAndCancelledSource(t *testing.T){
 var step v2optical.PreparedMappedReplacementStep
 if _,err:=CompilePerspectiveReplacementDetectionPlan(context.Background(),step);err!=v2input.ErrInvalid{t.Fatal("unproved detector plan accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=CompilePerspectiveReplacementDetectionPlan(ctx,step);err!=context.Canceled{t.Fatal("cancelled detector plan accepted",err)}
 plan:=PerspectiveReplacementDetectionPlan{descriptor:`{"schema_version":1}`}
 exposed:=plan.Descriptor();exposed[0]='x'
 if plan.Descriptor()[0]!='{'{t.Fatal("descriptor memory escaped")}
}

func TestReplacementDetectionDescriptorRecompilesSourceParamsAndProfile(t *testing.T){
 value,detector:=replacementDetectionDescriptorFixture(t)
 _=detector
 wire,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
 if _,err=decodeReplacementDetectionDescriptor(wire);err!=nil{t.Fatal(err)}
 for _,mutate:=range []func(*perspectiveReplacementDetectionDescriptor){
  func(v *perspectiveReplacementDetectionDescriptor){v.Run=uuid.New()},
  func(v *perspectiveReplacementDetectionDescriptor){v.Task=uuid.New()},
  func(v *perspectiveReplacementDetectionDescriptor){v.ProfileJCS=`{}`},
  func(v *perspectiveReplacementDetectionDescriptor){v.ParamsJCS=`{}`},
  func(v *perspectiveReplacementDetectionDescriptor){v.Base.Source.SHA256=strings.Repeat("e",64)},
  func(v *perspectiveReplacementDetectionDescriptor){v.Actor=uuid.Nil},
  func(v *perspectiveReplacementDetectionDescriptor){v.Review=uuid.New()},
  func(v *perspectiveReplacementDetectionDescriptor){v.Replacement.Task=uuid.New()},
  func(v *perspectiveReplacementDetectionDescriptor){v.Replacement.OriginalTask=uuid.New()},
  func(v *perspectiveReplacementDetectionDescriptor){v.Replacement.Parents=[]v2optical.NegativeReplacementParent{{Original:uuid.New(),Task:uuid.New(),Kind:"replacement"}}},
 }{
  bad:=value;mutate(&bad);body,err:=canonical.Marshal(bad);if err!=nil{t.Fatal(err)}
  if _,err=decodeReplacementDetectionDescriptor(body);err==nil{t.Fatal("altered detector descriptor accepted")}
 }
 // Structurally valid saved bytes still cannot replace physical source proofs.
 if _,err=RestorePerspectiveReplacementDetectionPlan(context.Background(),v2optical.PreparedMappedReplacementStep{},wire);err==nil{t.Fatal("saved descriptor became source authority")}
}

func replacementDetectionDescriptorFixture(t *testing.T)(perspectiveReplacementDetectionDescriptor,PerspectiveLinesPlan){
 t.Helper()

 scope,raw,icc:=perspectiveLinesFixture(t,false)
 node:=v2optical.NegativeReplacementNode{OriginalTask:uuid.New(),OriginalRun:uuid.New(),Task:uuid.New(),Run:uuid.New(),Parents:[]v2optical.NegativeReplacementParent{}}
 review:=uuid.New();decisionHash:=strings.Repeat("d",64)
 namespace:=uuid.NewSHA1(scope.Tenant,[]byte("V29.NegativeReplacement/v2:"+scope.Project.String()+":"+review.String()+":"+decisionHash))
 node.Task=uuid.NewSHA1(namespace,[]byte("task:"+node.OriginalTask.String()))
 node.Run=uuid.NewSHA1(namespace,[]byte("run:"+node.OriginalRun.String()+":"+node.OriginalTask.String()))
 scope.Run=uuid.NewSHA1(node.Run,[]byte("V29.PerspectiveReplacementDetection/v1"))
 detector,err:=CompilePerspectiveLinesPlan(scope,raw,icc);if err!=nil{t.Fatal(err)}
 value:=perspectiveReplacementDetectionDescriptor{SchemaVersion:1,Kind:"v29_perspective_replacement_detection_v1",Tenant:scope.Tenant,Project:scope.Project,Review:review,DecisionHash:decisionHash,Actor:uuid.New(),Session:uuid.New(),Replacement:node,Run:scope.Run,Task:detector.Task(),Base:scope.Base,SourceJCS:string(raw),ParamsJCS:string(detector.Params()),ProfileJCS:string(detector.Profile())}
 return value,detector
}
