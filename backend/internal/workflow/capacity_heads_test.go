package workflow

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type capacityHeadRow struct{ candidate CapacityCandidate; class string; cap int64 }
type capacityHeadRows struct{ pgx.Rows; items []capacityHeadRow; index int; closed bool }
func(r *capacityHeadRows) Next()bool{if r.index>=len(r.items){return false};r.index++;return true}
func(r *capacityHeadRows) Scan(dest ...any)error{
 item:=r.items[r.index-1]
 *dest[0].(*uuid.UUID)=item.candidate.TaskID
 *dest[1].(*uuid.UUID)=item.candidate.TenantID
 *dest[2].(*string)=item.candidate.OriginalQueue
 *dest[3].(*time.Time)=item.candidate.CreatedAt
 *dest[4].(**int64)=item.candidate.EstimatedMilliseconds
 *dest[5].(*bool)=item.candidate.Heavy
 *dest[6].(*int64)=item.candidate.TenantQueuedHeavy
 *dest[7].(*string)=item.class
 *dest[8].(*int64)=item.cap
 return nil
}
func(r *capacityHeadRows) Close(){r.closed=true}
func(r *capacityHeadRows) Err()error{return nil}
type capacityHeadTx struct{pgx.Tx;rows *capacityHeadRows;calls int}
func(tx *capacityHeadTx) Query(_ context.Context,q string,args ...any)(pgx.Rows,error){
 tx.calls++
 if q!="SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,effective_queue,max_runnable_cost_seconds\nFROM photo.capacity_queue_head_snapshot($1,$2,$3,$4)"||len(args)!=4{return nil,errors.New("wrong head query")}
 return tx.rows,nil
}
func TestCapacityHeadSnapshotRejectsChangedOrDuplicateScope(t *testing.T){
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"training",CreatedAt:now.Add(-11*time.Minute)}
 row:=capacityHeadRow{candidate:head,class:"batch",cap:3600}
 for _,tc:=range []struct{name string;items []capacityHeadRow;valid bool}{
  {"valid",[]capacityHeadRow{row},true},
  {"duplicate",[]capacityHeadRow{row,row},false},
  {"wrong class",[]capacityHeadRow{{candidate:head,class:"training",cap:3600}},false},
  {"changed cap",[]capacityHeadRow{row,{candidate:CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"batch",CreatedAt:now},class:"batch",cap:4000}},false},
  {"heavy cap",[]capacityHeadRow{{candidate:CapacityCandidate{TaskID:head.TaskID,TenantID:head.TenantID,OriginalQueue:"training",CreatedAt:head.CreatedAt,Heavy:true,TenantQueuedHeavy:2},class:"batch",cap:3600}},false},
 }{t.Run(tc.name,func(t *testing.T){
  rows:=&capacityHeadRows{items:tc.items};tx:=&capacityHeadTx{rows:rows}
  got,err:=ReadCapacityHeadSnapshot(context.Background(),tx,"gpu",now,policy)
  if (err==nil)!=tc.valid||!rows.closed||tx.calls!=1{t.Fatal(got,err,rows.closed,tx.calls)}
  if tc.valid&&(len(got.Heads)!=1||got.MaxRunnableCostSeconds!=3600){t.Fatal(got)}
 })}
}
