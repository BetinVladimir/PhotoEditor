package v2optical
import (
 "context"
 "errors"
 "reflect"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
type multiDispatchDouble struct{
 base NegativeReplacementWavePass
 pass MultiReplacementWavePass
 failure error
 budgets []int
}
func(f *multiDispatchDouble)AdvanceProjectReplacementWave(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision,int)(NegativeReplacementWavePass,error){return f.base,nil}
func(f *multiDispatchDouble)AdvanceProjectMultiReplacementWave(_ context.Context,_ auth.Identity,_,_ uuid.UUID,_ NegativeDeclinedDecision,budget int)(MultiReplacementWavePass,error){f.budgets=append(f.budgets,budget);return f.pass,f.failure}
func TestMultiReplacementDispatcherBudgetRecoveryAndDeferred(t *testing.T){
 panorama,focus,other:=uuid.New(),uuid.New(),uuid.New()
 base:=&multiDispatchDouble{base:NegativeReplacementWavePass{Publications:1,Deferred:[]NegativeReplacementNode{{OriginalTask:panorama},{OriginalTask:focus},{OriginalTask:other}}}}
 pan:=&multiDispatchDouble{pass:MultiReplacementWavePass{Receipts:[]MultiReplacementReceipt{{OriginalTask:panorama,RecoveredExisting:true}}}}
 stack:=&multiDispatchDouble{pass:MultiReplacementWavePass{Receipts:[]MultiReplacementReceipt{{OriginalTask:focus}}}}
 out,err:=(MultiReplacementDispatcher{Base:base,Panorama:pan,FocusStack:stack}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},2)
 if err!=nil||out.Publications!=2||len(out.MultiReceipts)!=2||!reflect.DeepEqual(pan.budgets,[]int{1})||!reflect.DeepEqual(stack.budgets,[]int{1})||len(out.Deferred)!=1||out.Deferred[0].OriginalTask!=other{t.Fatal("budget/recovery/deferred changed",out,err)}
 pan.pass.Receipts[0].RecoveredExisting=false;pan.budgets=nil;stack.budgets=nil
 out,err=(MultiReplacementDispatcher{Base:base,Panorama:pan,FocusStack:stack}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},2)
 if err!=nil||out.Publications!=2||len(stack.budgets)!=0||len(out.Deferred)!=2{t.Fatal("new admission exceeded budget",err)}
}
func TestMultiReplacementDispatcherPreservesFailureEvidence(t *testing.T){
 done,attempted,pending:=uuid.New(),uuid.New(),uuid.New();failure:=errors.New("ambiguous commit")
 nodes:=[]NegativeReplacementNode{{OriginalTask:done},{OriginalTask:attempted},{OriginalTask:pending}}
 base:=&multiDispatchDouble{base:NegativeReplacementWavePass{Deferred:nodes}}
 pan:=&multiDispatchDouble{pass:MultiReplacementWavePass{Receipts:[]MultiReplacementReceipt{{OriginalTask:done}},Unconfirmed:[]NegativeReplacementNode{{OriginalTask:attempted}},Pending:[]NegativeReplacementNode{{OriginalTask:pending}}},failure:failure}
 stack:=&multiDispatchDouble{}
 out,err:=(MultiReplacementDispatcher{Base:base,Panorama:pan,FocusStack:stack}).AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},3)
 if !errors.Is(err,failure)||len(out.MultiReceipts)!=1||out.Publications!=1||len(out.Unconfirmed)!=1||len(out.Pending)!=1||!reflect.DeepEqual(out.Deferred,nodes)||len(stack.budgets)!=0{t.Fatal("failure evidence lost or retried",err)}
}
func TestMultiReplacementDispatcherSettlementCancellationAndPending(t *testing.T){
 node:=NegativeReplacementNode{OriginalTask:uuid.New()}
 base:=&multiDispatchDouble{base:NegativeReplacementWavePass{WaitingForSettlement:true,Deferred:[]NegativeReplacementNode{node}}}
 pan:=&multiDispatchDouble{pass:MultiReplacementWavePass{Pending:[]NegativeReplacementNode{node}}}
 d:=MultiReplacementDispatcher{Base:base,Panorama:pan}
 if _,err:=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1);err!=nil||len(pan.budgets)!=0{t.Fatal("unsettled graph advanced",err)}
 base.base.WaitingForSettlement=false
 out,err:=d.AdvanceProjectReplacementWave(context.Background(),auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1)
 if err!=nil||len(out.Pending)!=1||len(out.Deferred)!=0{t.Fatal("supported pending lost",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();pan.budgets=nil
 if _,err=d.AdvanceProjectReplacementWave(ctx,auth.Identity{},uuid.New(),uuid.New(),NegativeDeclinedDecision{},1);err!=context.Canceled||len(pan.budgets)!=0{t.Fatal("cancelled graph advanced",err)}
}
