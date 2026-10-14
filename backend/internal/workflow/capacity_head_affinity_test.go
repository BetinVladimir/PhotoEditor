package workflow

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type headAffinityRows struct{pgx.Rows;items []CapacityHeadAffinity;i int;closed bool}
func(r *headAffinityRows) Next()bool{if r.i>=len(r.items){return false};r.i++;return true}
func(r *headAffinityRows) Scan(dest ...any)error{
 item:=r.items[r.i-1];c:=item.Candidate
 *dest[0].(*uuid.UUID)=c.TaskID;*dest[1].(*uuid.UUID)=c.TenantID
 *dest[2].(*string)=c.OriginalQueue;*dest[3].(*time.Time)=c.CreatedAt
 *dest[4].(**int64)=c.EstimatedMilliseconds;*dest[5].(*bool)=c.Heavy
 *dest[6].(*int64)=c.TenantQueuedHeavy
 if item.ModelLockSHA!=""{pin:=item.ModelLockSHA;*dest[7].(**string)=&pin}
 *dest[8].(*bool)=item.Resident;return nil
}
func(r *headAffinityRows) Close(){r.closed=true}
func(r *headAffinityRows) Err()error{return nil}
type headAffinityTx struct{pgx.Tx;rows *headAffinityRows;calls int}
func(tx *headAffinityTx) Query(_ context.Context,q string,args ...any)(pgx.Rows,error){
 tx.calls++
 if q!="SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,model_lock_sha,resident\nFROM photo.capacity_queue_head_affinity($1,$2,$3,$4,$5,$6,$7,$8)"||len(args)!=8{return nil,errors.New("wrong scoped affinity query")}
 return tx.rows,nil
}
func TestCapacityHeadAffinityScopeAndFallback(t *testing.T){
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-40*time.Second)}
 selected:=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:"interactive",createdAt:head.CreatedAt,cost:1800}
 resident:=head;resident.TaskID=uuid.New();resident.CreatedAt=head.CreatedAt.Add(30*time.Second)
 pin:=strings.Repeat("a",64)
 for _,tc:=range []struct{name string;result CapacityHeadAffinity;valid bool}{
  {"fallback",CapacityHeadAffinity{Candidate:head},true},
  {"resident boundary",CapacityHeadAffinity{Candidate:resident,ModelLockSHA:pin,Resident:true},true},
  {"late",CapacityHeadAffinity{Candidate:func()CapacityCandidate{c:=resident;c.CreatedAt=c.CreatedAt.Add(time.Nanosecond);return c}(),ModelLockSHA:pin,Resident:true},false},
  {"foreign",CapacityHeadAffinity{Candidate:func()CapacityCandidate{c:=resident;c.TenantID=uuid.New();return c}(),ModelLockSHA:pin,Resident:true},false},
  {"unproven fallback",CapacityHeadAffinity{Candidate:resident},false},
  {"missing pin",CapacityHeadAffinity{Candidate:resident,Resident:true},false},
 }{t.Run(tc.name,func(t *testing.T){
  rows:=&headAffinityRows{items:[]CapacityHeadAffinity{tc.result}};tx:=&headAffinityTx{rows:rows}
  got,err:=ReadCapacityHeadAffinity(context.Background(),tx,"gpu",selected,now,policy,nil)
  if (err==nil)!=tc.valid||!rows.closed||tx.calls!=1{t.Fatal(got,err,rows.closed,tx.calls)}
 })}
 for _,ids:=range [][]uuid.UUID{{uuid.Nil},{resident.TaskID,resident.TaskID}}{
  tx:=&headAffinityTx{rows:&headAffinityRows{}}
  if _,err:=ReadCapacityHeadAffinity(context.Background(),tx,"gpu",selected,now,policy,ids);err==nil||tx.calls!=0{t.Fatal("invalid exclusions reached SQL",ids,err)}
 }
 tx:=&headAffinityTx{rows:&headAffinityRows{items:[]CapacityHeadAffinity{{Candidate:resident,ModelLockSHA:pin,Resident:true}}}}
 if _,err:=ReadCapacityHeadAffinity(context.Background(),tx,"gpu",selected,now,policy,[]uuid.UUID{resident.TaskID});err==nil{t.Fatal("excluded resident accepted")}
}
