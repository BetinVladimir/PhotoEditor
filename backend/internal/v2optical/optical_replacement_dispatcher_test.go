package v2optical

import (
 "context"
 "errors"
 "bytes"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

type opticalDispatchDouble struct{negative NegativeReplacementWavePass;perspective ManualPerspectiveReplacementWavePass;budget int;calls int;failure error}
func(f *opticalDispatchDouble)AdvanceProjectReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision,int)(NegativeReplacementWavePass,error){return f.negative,nil}
func(f *opticalDispatchDouble)AdvanceProjectManualReplacementWave(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ NegativeDeclinedDecision,limit int)(ManualPerspectiveReplacementWavePass,error){f.calls++;f.budget=limit;return f.perspective,f.failure}

func TestOpticalReplacementDispatcherSharesBudgetAndPreservesDeferred(t *testing.T){
 negative,manual,other:=uuid.New(),uuid.New(),uuid.New()
 f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{Receipts:[]NegativeReplacementReceipt{{OriginalTask:negative}},Deferred:[]NegativeReplacementNode{{OriginalTask:manual},{OriginalTask:other}}},perspective:ManualPerspectiveReplacementWavePass{Receipts:[]ManualPerspectiveReplacementReceipt{{OriginalTask:manual}},Deferred:[]NegativeReplacementNode{{OriginalTask:negative},{OriginalTask:other}}}}
 out,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},3)
 if err!=nil||f.calls!=1||f.budget!=2||len(out.PerspectiveReceipts)!=1||len(out.Deferred)!=1||out.Deferred[0].OriginalTask!=other{t.Fatal("dispatch budget or inventory changed",err)}
 f.failure=errors.New("ambiguous commit")
 out,err=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},3)
 if !errors.Is(err,f.failure)||len(out.Receipts)!=1||len(out.PerspectiveReceipts)!=1||len(out.Deferred)!=2{t.Fatal("failure lost receipts or deferred nodes",err)}
}

func TestOpticalReplacementDispatcherHonorsSettlementAndBudget(t *testing.T){
 for _,waiting:=range []bool{false,true}{
  f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{WaitingForSettlement:waiting,Receipts:[]NegativeReplacementReceipt{{}},Deferred:[]NegativeReplacementNode{{}}}}
  if _,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1);err!=nil||f.calls!=0{t.Fatal("unexpected second pass",err)}
 }
}

func TestOpticalReplacementDispatcherRecoveredReceiptDoesNotStarvePerspective(t *testing.T){
 negative,manual:=uuid.New(),uuid.New()
 f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{Receipts:[]NegativeReplacementReceipt{{OriginalTask:negative,RecoveredExisting:true}},Deferred:[]NegativeReplacementNode{{OriginalTask:manual}}},perspective:ManualPerspectiveReplacementWavePass{Receipts:[]ManualPerspectiveReplacementReceipt{{OriginalTask:manual}}}}
 out,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if err!=nil||f.calls!=1||f.budget!=1||len(out.Receipts)!=1||len(out.PerspectiveReceipts)!=1{t.Fatal("historical receipt starved ready perspective",err)}
 f.negative.Receipts[0].RecoveredExisting=false;f.calls=0
 if _,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1);err!=nil||f.calls!=0{t.Fatal("new admission exceeded shared limit",err)}
}

func TestOpticalReplacementRecoveryObservationsAreNotReceiptEvidence(t *testing.T){
 negative:=NegativeReplacementReceipt{OriginalTask:uuid.New()}
 before,err:=json.Marshal(negative);if err!=nil{t.Fatal(err)}
 negative.RecoveredExisting=true
 after,err:=json.Marshal(negative);if err!=nil||!bytes.Equal(before,after){t.Fatal("local negative recovery changed receipt wire",err)}
 perspective:=ManualPerspectiveReplacementReceipt{OriginalTask:uuid.New()}
 before,err=json.Marshal(perspective);if err!=nil{t.Fatal(err)}
 perspective.RecoveredExisting=true
 after,err=json.Marshal(perspective);if err!=nil||!bytes.Equal(before,after){t.Fatal("local perspective recovery changed receipt wire",err)}
 var replay ManualPerspectiveReplacementReceipt
 if err=json.Unmarshal(after,&replay);err!=nil||replay.RecoveredExisting{t.Fatal("serialized receipt supplied recovery authority",err)}
}
func TestOpticalReplacementDispatcherCountsOnlyNewPerspectiveAdmissions(t *testing.T){
 f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{Deferred:[]NegativeReplacementNode{{OriginalTask:uuid.New()}}},perspective:ManualPerspectiveReplacementWavePass{Receipts:[]ManualPerspectiveReplacementReceipt{{RecoveredExisting:true},{RecoveredExisting:false}}}}
 out,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if err!=nil||out.Publications!=1||len(out.PerspectiveReceipts)!=2{t.Fatal("recovered perspective charged publication budget",err,out.Publications)}
}

func TestOpticalReplacementDispatcherRetainsCapacityPendingInventory(t *testing.T){
 negative,manual:=uuid.New(),uuid.New()
 f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{Pending:[]NegativeReplacementNode{{OriginalTask:negative}},Deferred:[]NegativeReplacementNode{{OriginalTask:manual}}},perspective:ManualPerspectiveReplacementWavePass{Pending:[]NegativeReplacementNode{{OriginalTask:manual}},Deferred:[]NegativeReplacementNode{{OriginalTask:negative}}}}
 out,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if err!=nil||len(out.Deferred)!=0||len(out.Pending)!=2||out.Pending[0].OriginalTask!=negative||out.Pending[1].OriginalTask!=manual{t.Fatal("capacity pending inventory lost",err,out.Pending)}
 f.failure=errors.New("publication failure")
 out,err=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if !errors.Is(err,f.failure)||len(out.Pending)!=2{t.Fatal("failure hid pending inventory",err)}
}

func TestOpticalReplacementDispatcherPreservesUnconfirmedAttempt(t *testing.T){
 completed,attempted,pending:=uuid.New(),uuid.New(),uuid.New()
 failure:=errors.New("commit outcome unknown")
 f:=&opticalDispatchDouble{negative:NegativeReplacementWavePass{Receipts:[]NegativeReplacementReceipt{{OriginalTask:completed,RecoveredExisting:true}},Deferred:[]NegativeReplacementNode{{OriginalTask:attempted},{OriginalTask:pending}}},perspective:ManualPerspectiveReplacementWavePass{Unconfirmed:[]NegativeReplacementNode{{OriginalTask:attempted}},Pending:[]NegativeReplacementNode{{OriginalTask:pending}}},failure:failure}
 out,err:=(OpticalReplacementDispatcher{Negative:f,Perspective:f}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if !errors.Is(err,failure)||len(out.Receipts)!=1||len(out.PerspectiveReceipts)!=0||len(out.Unconfirmed)!=1||out.Unconfirmed[0].OriginalTask!=attempted||len(out.Pending)!=1||out.Pending[0].OriginalTask!=pending{t.Fatal("ambiguous attempt inventory lost or promoted",err)}
 if f.calls!=1{t.Fatal("ambiguous publication retried")}
}
