package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)
func TestReplacementSegmentsPlanBindsResultAndOwnsBytes(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 r:=ReplacementDetectionDocuments{Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 lines:=&v2optical.PerspectiveLineSegments{SchemaVersion:1,Lines:[]v2optical.PerspectiveLine{
  {A:v2optical.PerspectivePoint{X:0,Y:0.2},B:v2optical.PerspectivePoint{X:1,Y:0.2},Family:"horizontal"},
  {A:v2optical.PerspectivePoint{X:0,Y:0.8},B:v2optical.PerspectivePoint{X:1,Y:0.8},Family:"horizontal"},
  {A:v2optical.PerspectivePoint{X:0.2,Y:0},B:v2optical.PerspectivePoint{X:0.2,Y:1},Family:"vertical"},
  {A:v2optical.PerspectivePoint{X:0.8,Y:0},B:v2optical.PerspectivePoint{X:0.8,Y:1},Family:"vertical"},
 }}
 detection:=v2optical.DetectedPerspectiveLines{SchemaVersion:1,Base:descriptor.Base,Status:"available",Lines:lines}
 result,err:=canonical.Marshal(detection);if err!=nil{t.Fatal(err)}
 sha,err:=canonical.Hash(detection);if err!=nil{t.Fatal(err)}
 locked:=LockedReplacementDetectedLines{descriptor:string(raw),snapshot:r.Snapshot,resultJCS:string(result),observation:ReplacementDetectedLines{ResultID:uuid.New(),Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Detection:detection}}
 plan,err:=r.compileSegmentsPlan(context.Background(),locked);if err!=nil{t.Fatal(err)}
 replay,err:=r.compileSegmentsPlan(context.Background(),locked);if err!=nil||replay.Ref()!=plan.Ref(){t.Fatal("unstable identity",err)}
 if plan.Ref()==locked.Root()||plan.Scope().Owner==descriptor.Task{t.Fatal("detector root/owner reused")}
 bytes:=plan.Bytes();bytes[0]='x';if plan.Bytes()[0]=='x'{t.Fatal("mutable plan content")}
 locked.observation.ResultID=uuid.New()
 changed,err:=r.compileSegmentsPlan(context.Background(),locked);if err!=nil||changed.Ref().ArtifactID==plan.Ref().ArtifactID{t.Fatal("result change reused identity",err)}
 locked.snapshot=uuid.New()
 if _,err:=r.compileSegmentsPlan(context.Background(),locked);err==nil{t.Fatal("foreign snapshot accepted")}
}
func TestReplacementSegmentsPlanRejectsUnprovenAndCancelled(t *testing.T){
 r:=ReplacementDetectionDocuments{}
 if _,err:=r.PrepareSegmentsPlan(context.Background());err==nil{t.Fatal("missing source accepted")}
 if _,err:=r.LockSegmentsPlan(context.Background(),nil,ReplacementDetectionSegmentsPlan{});err==nil{t.Fatal("empty plan accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=r.compileSegmentsPlan(ctx,LockedReplacementDetectedLines{});err!=context.Canceled{t.Fatal(err)}
}
