package workflow

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2optical"
)
type replacementDetectionBaseDouble struct{pass v2optical.NegativeReplacementWavePass}
func(b replacementDetectionBaseDouble) AdvanceProjectReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,v2optical.NegativeDeclinedDecision,int)(v2optical.NegativeReplacementWavePass,error){return b.pass,nil}
type replacementDetectionWaveDouble struct{pass ReplacementDetectionWavePass;failure error;calls,budget int}
func(d *replacementDetectionWaveDouble) Advance(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ v2optical.NegativeDeclinedDecision,limit int)(ReplacementDetectionWavePass,error){d.calls++;d.budget=limit;return d.pass,d.failure}
func TestReplacementDetectionDispatcherPreservesUnfinishedPerspective(t *testing.T){
 node:=v2optical.NegativeReplacementNode{OriginalTask:uuid.New()}
 detector:=&replacementDetectionWaveDouble{pass:ReplacementDetectionWavePass{Publications:1,Admissions:[]ReplacementDetectionAdmission{{OriginalTask:node.OriginalTask,Run:uuid.New(),Task:uuid.New()}},Pending:[]v2optical.NegativeReplacementNode{node}}}
 d:=ReplacementDetectionWaveDispatcher{Base:replacementDetectionBaseDouble{pass:v2optical.NegativeReplacementWavePass{Publications:1,Deferred:[]v2optical.NegativeReplacementNode{node}}},Detection:detector}
 out,err:=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{},3)
 if err!=nil||detector.budget!=2||out.Publications!=2||len(out.DetectorAdmissions)!=1||len(out.Deferred)!=1||len(out.DetectorPending)!=1||len(out.Pending)!=0||len(out.PerspectiveReceipts)!=0{t.Fatal("prerequisite promoted to replacement",out,err)}
 detector.failure=errors.New("ambiguous detector admission")
 detector.pass.Unconfirmed=[]v2optical.NegativeReplacementNode{node}
 out,err=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{},3)
 if !errors.Is(err,detector.failure)||len(out.DetectorUnconfirmed)!=1||len(out.DetectorAdmissions)!=1||len(out.Deferred)!=1{t.Fatal("partial evidence lost",err)}
}
func TestReplacementDetectionDispatcherHonorsExhaustedBudget(t *testing.T){
 detector:=&replacementDetectionWaveDouble{}
 d:=ReplacementDetectionWaveDispatcher{Base:replacementDetectionBaseDouble{pass:v2optical.NegativeReplacementWavePass{Publications:1,Deferred:[]v2optical.NegativeReplacementNode{{OriginalTask:uuid.New()}}}},Detection:detector}
 if _,err:=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{},1);err!=nil||detector.calls!=0{t.Fatal("exhausted budget dispatched",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=d.AdvanceProjectReplacementWave(ctx,auth.Identity{},uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{},1);err!=context.Canceled{t.Fatal(err)}
}
func TestReplacementDetectionDispatcherOnlyPerspectiveReceiptClearsDeferred(t *testing.T){
 original,waiting:=uuid.New(),uuid.New()
 detector:=&replacementDetectionWaveDouble{pass:ReplacementDetectionWavePass{AutomaticReceipts:[]v2optical.AutomaticPerspectiveReplacementReceipt{{OriginalTask:original,RecoveredExisting:true}}}}
 d:=ReplacementDetectionWaveDispatcher{Base:replacementDetectionBaseDouble{pass:v2optical.NegativeReplacementWavePass{Deferred:[]v2optical.NegativeReplacementNode{{OriginalTask:original},{OriginalTask:waiting}}}},Detection:detector}
 out,err:=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{},1)
 if err!=nil||len(out.PerspectiveReceipts)!=1||len(out.Deferred)!=1||out.Deferred[0].OriginalTask!=waiting||out.Publications!=0{t.Fatal("replacement evidence inventory changed",out,err)}
}
