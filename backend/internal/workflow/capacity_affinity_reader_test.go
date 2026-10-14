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

type affinityReaderTx struct {pgx.Tx;rows *affinityReaderRows;err error;ids []uuid.UUID;calls int}
func(tx *affinityReaderTx) Query(_ context.Context,q string,args ...any)(pgx.Rows,error){
 tx.calls++
 if q!="SELECT task_id,model_lock_sha,resident FROM photo.capacity_task_affinity($1)"||len(args)!=1{return nil,errors.New("foreign query")}
 tx.ids=append([]uuid.UUID(nil),args[0].([]uuid.UUID)...)
 return tx.rows,tx.err
}
type affinityReaderRows struct {pgx.Rows;items []CapacityAffinityEvidence;index int;closed bool;scanErr,errorAfter error;cancelAtEnd context.CancelFunc}
func(r *affinityReaderRows) Next()bool{if r.index>=len(r.items){if r.cancelAtEnd!=nil{r.cancelAtEnd()};return false};r.index++;return true}
func(r *affinityReaderRows) Scan(dest ...any)error{
 if r.scanErr!=nil{return r.scanErr};v:=r.items[r.index-1]
 *dest[0].(*uuid.UUID)=v.TaskID;*dest[1].(*string)=v.ModelLockSHA;*dest[2].(*bool)=v.Resident;return nil
}
func(r *affinityReaderRows) Close(){r.closed=true}
func(r *affinityReaderRows) Err()error{return r.errorAfter}

func TestCapacityAffinityReaderRestrictsSQLScope(t *testing.T){
 d,now:=capacityDRRFixture(t);ms:=int64(1000)
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-31*time.Second),EstimatedMilliseconds:&ms}
 boundary:=head;boundary.TaskID=uuid.New();boundary.CreatedAt=head.CreatedAt.Add(30*time.Second)
 outside:=boundary;outside.TaskID=uuid.New();outside.CreatedAt=outside.CreatedAt.Add(time.Nanosecond)
 foreign:=boundary;foreign.TaskID=uuid.New();foreign.TenantID=uuid.New()
 otherClass:=boundary;otherClass.TaskID=uuid.New();otherClass.OriginalQueue="batch"
 candidates:=[]CapacityCandidate{head,boundary,outside,foreign,otherClass}
 if err:=d.BeginCycle(candidates,now);err!=nil{t.Fatal(err)}
 // Use the exact head receipt to isolate the reader from tenant UUID ordering.
 selection:=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:"interactive",cost:1}
 rows:=&affinityReaderRows{items:[]CapacityAffinityEvidence{{TaskID:boundary.TaskID,ModelLockSHA:strings.Repeat("a",64),Resident:true}}}
 tx:=&affinityReaderTx{rows:rows}
 got,err:=ReadCapacityAffinity(context.Background(),tx,candidates,selection,now)
 if err!=nil||len(got)!=1||got[0].TaskID!=boundary.TaskID||!rows.closed{t.Fatal(got,err)}
 if len(tx.ids)!=2||tx.ids[0]!=head.TaskID||tx.ids[1]!=boundary.TaskID{t.Fatal("SQL scope escaped",tx.ids)}
}

func TestCapacityAffinityReaderRejectsInvalidSQLAndPropagatesErrors(t *testing.T){
 _,now:=capacityDRRFixture(t)
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now}
 selection:=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:"interactive"}
 valid:=CapacityAffinityEvidence{TaskID:head.TaskID,ModelLockSHA:strings.Repeat("a",64),Resident:true}
 sentinel:=errors.New("SQL failure")
 for _,mode:=range []string{"foreign","duplicate","digest","not-resident","scan","rows","query","cancel"}{t.Run(mode,func(t *testing.T){
  rows:=&affinityReaderRows{items:[]CapacityAffinityEvidence{valid}}
  tx:=&affinityReaderTx{rows:rows};ctx:=context.Background()
  switch mode{
  case "foreign":rows.items[0].TaskID=uuid.New()
  case "duplicate":rows.items=append(rows.items,valid)
  case "digest":rows.items[0].ModelLockSHA="bad"
  case "not-resident":rows.items[0].Resident=false
  case "scan":rows.scanErr=sentinel
  case "rows":rows.errorAfter=sentinel
  case "query":tx.err=sentinel
  case "cancel":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
  }
  got,err:=ReadCapacityAffinity(ctx,tx,[]CapacityCandidate{head},selection,now)
  if err==nil||got!=nil{t.Fatal("invalid SQL evidence accepted",got)}
  if (mode=="scan"||mode=="rows"||mode=="query")&&!errors.Is(err,sentinel){t.Fatal("lost SQL error",err)}
  if mode=="cancel"{if tx.calls!=0||!errors.Is(err,context.Canceled){t.Fatal("cancel queried SQL",err)}}else if mode!="query"&&!rows.closed{t.Fatal("rows leaked")}
 })}
}

func TestCapacityAffinityReaderDiscardsCancelledTerminalStream(t *testing.T){
 for _,empty:=range []bool{true,false}{
  _,now:=capacityDRRFixture(t)
  head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now}
  selection:=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:"interactive"}
  ctx,cancel:=context.WithCancel(context.Background())
  rows:=&affinityReaderRows{cancelAtEnd:cancel}
  if !empty{rows.items=[]CapacityAffinityEvidence{{TaskID:head.TaskID,ModelLockSHA:strings.Repeat("a",64),Resident:true}}}
  tx:=&affinityReaderTx{rows:rows}
  got,err:=ReadCapacityAffinity(ctx,tx,[]CapacityCandidate{head},selection,now)
  cancel()
  if got!=nil||!errors.Is(err,context.Canceled)||!rows.closed{t.Fatal("cancelled affinity escaped",empty,got,err,rows.closed)}
 }
}

func TestCapacityAffinityReaderRejectsNonFIFOHeadBeforeSQL(t *testing.T){
 for _,mode:=range []string{"older","uuid-tie","zero-clock"}{t.Run(mode,func(t *testing.T){
  _,now:=capacityDRRFixture(t)
  head:=CapacityCandidate{TaskID:uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-time.Second)}
  earlier:=head
  earlier.TaskID=uuid.MustParse("00000000-0000-4000-8000-000000000001")
  if mode=="older"{earlier.CreatedAt=head.CreatedAt.Add(-time.Second)}
  if mode=="zero-clock"{now=time.Time{}}
  selection:=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:"interactive"}
  tx:=&affinityReaderTx{}
  got,err:=ReadCapacityAffinity(context.Background(),tx,[]CapacityCandidate{head,earlier},selection,now)
  if err==nil||got!=nil||tx.calls!=0{t.Fatal("invalid head reached SQL",mode,got,err,tx.calls)}
 })}
}
