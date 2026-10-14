package workflow

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type replacementAdoptionTestQueue struct { items []*ReplacementDetectionAdoptionCandidate; cursors []uuid.UUID }
func(q *replacementAdoptionTestQueue) Next(_ context.Context,after uuid.UUID)(*ReplacementDetectionAdoptionCandidate,error){
 q.cursors=append(q.cursors,after)
 if len(q.items)==0{return nil,nil};out:=q.items[0];q.items=q.items[1:];return out,nil
}
type replacementAdoptionTestLoader struct { source ReplacementDetectionDocuments; failure error; calls int }
func(l *replacementAdoptionTestLoader) Load(ctx context.Context,c ReplacementDetectionAdoptionCandidate)(ReplacementDetectionDocuments,error){
 l.calls++;if _,ok:=ctx.Deadline();!ok{return ReplacementDetectionDocuments{},errors.New("missing deadline")}
 err:=l.failure;l.failure=nil;return l.source,err
}
type replacementAdoptionTestRunner struct { calls int; result uuid.UUID; root v2input.Ref; attempt int; fence int64 }
func(r *replacementAdoptionTestRunner) Run(_ context.Context,_ ReplacementDetectionDocuments,a int,f int64,result uuid.UUID,root v2input.Ref)(bool,error){r.calls++;r.attempt=a;r.fence=f;r.result=result;r.root=root;return true,nil}
func TestReplacementAdoptionDispatcherFailureWrapAndRecovery(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 candidate:=&ReplacementDetectionAdoptionCandidate{Tenant:descriptor.Tenant,Run:descriptor.Run,Task:descriptor.Task,Snapshot:uuid.New(),Result:uuid.New(),Descriptor:raw,Attempt:2,Fence:7,Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 q:=&replacementAdoptionTestQueue{items:[]*ReplacementDetectionAdoptionCandidate{candidate,nil,candidate}}
 transient:=errors.New("source unavailable")
 l:=&replacementAdoptionTestLoader{source:ReplacementDetectionDocuments{Snapshot:candidate.Snapshot,Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}},failure:transient}
 r:=&replacementAdoptionTestRunner{}
 d:=&ReplacementDetectionAdoptionDispatcher{Candidates:q,Loader:l,Runner:r}
 if ok,err:=d.ProcessOne(context.Background());ok||!errors.Is(err,transient){t.Fatal(ok,err)}
 if ok,err:=d.ProcessOne(context.Background());ok||err!=nil{t.Fatal(ok,err)}
 if ok,err:=d.ProcessOne(context.Background());!ok||err!=nil{t.Fatal(ok,err)}
 if len(q.cursors)!=3||q.cursors[0]!=uuid.Nil||q.cursors[1]!=candidate.Run||q.cursors[2]!=uuid.Nil{t.Fatal(q.cursors)}
 if r.calls!=1||r.result!=candidate.Result||r.root!=candidate.Root||r.attempt!=2||r.fence!=7{t.Fatal("runner packet changed")}
}
func TestReplacementAdoptionDispatcherRejectsCancellationAndConcurrentTick(t *testing.T){
 q:=&replacementAdoptionTestQueue{}
 d:=&ReplacementDetectionAdoptionDispatcher{Candidates:q,Loader:&replacementAdoptionTestLoader{},Runner:&replacementAdoptionTestRunner{}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=d.ProcessOne(ctx);err!=context.Canceled||len(q.cursors)!=0{t.Fatal(err)}
 d.mu.Lock()
 ok,err:=d.ProcessOne(context.Background())
 d.mu.Unlock()
 if ok||err!=nil||len(q.cursors)!=0{t.Fatal("concurrent tick queued",ok,err)}
}
