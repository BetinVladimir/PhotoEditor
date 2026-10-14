package workflow

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type boundedHeadRow struct{head *CapacityCandidate;class string;cap int64}
func(r boundedHeadRow) Scan(dest ...any)error{
 if len(dest)!=9{return errors.New("wrong bounded head scan")}
 if r.head!=nil{
  candidate:=r.head
  *dest[0].(**uuid.UUID)=&candidate.TaskID
  *dest[1].(**uuid.UUID)=&candidate.TenantID
  *dest[2].(**string)=&candidate.OriginalQueue
  *dest[3].(**time.Time)=&candidate.CreatedAt
  *dest[4].(**int64)=candidate.EstimatedMilliseconds
  *dest[5].(**bool)=&candidate.Heavy
  *dest[6].(**int64)=&candidate.TenantQueuedHeavy
  *dest[7].(**string)=&r.class
 }
 *dest[8].(*int64)=r.cap
 return nil
}
type boundedHeadTx struct{pgx.Tx;row boundedHeadRow;calls int}
func(tx *boundedHeadTx) QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 tx.calls++
 if query!="SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,effective_queue,max_runnable_cost_seconds\nFROM photo.capacity_queue_bounded_head_choice($1,$2,$3,$4,$5,$6,$7,$8)"||len(args)!=8{
  return boundedHeadErrorRow{}
 }
 return tx.row
}
type boundedHeadErrorRow struct{}
func(boundedHeadErrorRow)Scan(...any)error{return errors.New("wrong bounded head query")}

func TestCapacityBoundedHeadReadOneOrNoEligibleHead(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:30,TenantHeavyQueued:2}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 deficit:=map[string]int64{"interactive":30,"capture":30,"batch":30,"training":30}
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"capture",CreatedAt:now.Add(-time.Minute)}
 for _,tc:=range []struct{name string;row boundedHeadRow;valid,hasHead bool}{
  {"selected",boundedHeadRow{head:&head,class:"capture",cap:3600},true,true},
  {"none fits",boundedHeadRow{cap:3600},true,false},
  {"wrong class",boundedHeadRow{head:&head,class:"batch",cap:3600},false,false},
  {"invalid cap",boundedHeadRow{head:&head,class:"capture",cap:100},false,false},
 }{t.Run(tc.name,func(t *testing.T){
  tx:=&boundedHeadTx{row:tc.row}
  got,err:=ReadCapacityBoundedHeadChoice(context.Background(),tx,"cpu",now,policy,uuid.Nil,0,deficit,nil)
  if (err==nil)!=tc.valid||tx.calls!=1{t.Fatal(got,err,tx.calls)}
  if tc.valid&&((got.Head!=nil)!=tc.hasHead||got.MaxRunnableCostSeconds!=3600){t.Fatal(got)}
 })}
 tx:=&boundedHeadTx{row:boundedHeadRow{head:&head,class:"capture",cap:3600}}
 deficit["capture"]=29
 if _,err:=ReadCapacityBoundedHeadChoice(context.Background(),tx,"cpu",now,policy,uuid.Nil,0,deficit,nil);err==nil{t.Fatal("over-deficit head accepted")}
 if tx.calls!=1{t.Fatal("head query not reached")}
}

func TestCapacityBoundedHeadChargesOnlyCommittedSelection(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:30,TenantHeavyQueued:2}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 d,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 if err=d.BeginBoundedHeadCycle(3600,now);err!=nil{t.Fatal(err)}
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-time.Minute)}
 choice:=CapacityBoundedHeadChoice{Head:&head,EffectiveClass:"interactive",MaxRunnableCostSeconds:3600}
 first,err:=d.AcceptBoundedHead(choice,now);if err!=nil||first==nil{t.Fatal(first,err)}
 if _,err=d.AcceptBoundedHead(choice,now);err==nil{t.Fatal("unsettled selection replaced")}
 if err=d.Settle(first,false);err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=120||d.lastTenant!=uuid.Nil{t.Fatal("failed reservation charged DRR")}
 second,err:=d.AcceptBoundedHead(choice,now);if err!=nil||second==nil{t.Fatal(second,err)}
 if err=d.Settle(second,true);err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=90||d.lastTenant!=head.TenantID||d.classCursor!=1{t.Fatal("committed reservation did not advance DRR")}
}

func TestCapacityBoundedCycleFailureDropsInMemoryGeneration(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:30,TenantHeavyQueued:2}
 selector,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 selector.deficits["interactive"]=120
 dispatcher:=&CapacityDispatcher{leader:&CapacityLeader{},selector:selector}
 sourceErr:=errors.New("head source failed after quantum")
 count,got:=dispatcher.failBoundedCycle(2,sourceErr)
 if count!=2||!errors.Is(got,sourceErr)||dispatcher.selector!=nil{
  t.Fatal("failed cycle retained a reusable DRR generation",count,got,dispatcher.selector)
 }
}

func TestCapacityBoundedInitialLeaderFailureDropsSelector(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:30,TenantHeavyQueued:2}
 selector,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 selector.deficits["interactive"]=120
 dispatcher:=&CapacityDispatcher{leader:&CapacityLeader{},selector:selector,pool:"cpu"}
 count,err:=dispatcher.DispatchBoundedHeadCycle(context.Background(),1)
 if count!=0||!errors.Is(err,ErrCapacityLeaderUnavailable)||dispatcher.selector!=nil{
  t.Fatal("initial leader failure retained bounded DRR state",count,err,dispatcher.selector)
 }
 if count,err=dispatcher.DispatchBoundedHeadCycle(context.Background(),1);count!=0||err==nil{
  t.Fatal("terminal dispatcher retried old generation",count,err)
 }
}
