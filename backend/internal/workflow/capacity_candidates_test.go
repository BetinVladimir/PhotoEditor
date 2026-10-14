package workflow
import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)
type capacityCandidateRows struct{pgx.Rows;remaining int;closed bool;failure error;cancel context.CancelFunc}
func(r *capacityCandidateRows)Next()bool{if r.remaining>0{r.remaining--;return true};if r.cancel!=nil{r.cancel()};return false}
func(r *capacityCandidateRows)Scan(dest ...any)error{
 *dest[0].(*uuid.UUID)=uuid.New();*dest[1].(*uuid.UUID)=uuid.New();*dest[2].(*string)="batch";*dest[3].(*time.Time)=time.Now()
 *dest[4].(**int64)=nil;*dest[5].(*bool)=false;*dest[6].(*int64)=0;return nil
}
func(r *capacityCandidateRows)Close(){r.closed=true}
func(r *capacityCandidateRows)Err()error{return r.failure}
type capacityCandidateTx struct{pgx.Tx;calls int;rows *capacityCandidateRows;failure error}
func(tx *capacityCandidateTx)Query(_ context.Context,q string,args ...any)(pgx.Rows,error){
 tx.calls++
 if q!="SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy FROM photo.capacity_queue_candidates($1)"||len(args)!=1||args[0]!="cpu"{panic("unexpected capacity query")}
 return tx.rows,tx.failure
}
func TestCapacityCandidatesDiscardIncompleteSnapshot(t *testing.T){
 failure:=errors.New("stream failed")
 for _,mode:=range []string{"complete","stream error","empty cancelled","partial cancelled","query error"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  rows:=&capacityCandidateRows{remaining:2};tx:=&capacityCandidateTx{rows:rows}
  switch mode{case "stream error":rows.failure=failure;case "empty cancelled":rows.remaining=0;rows.cancel=cancel;case "partial cancelled":rows.cancel=cancel;case "query error":tx.failure=failure}
  got,err:=ReadCapacityCandidates(ctx,tx,"cpu")
  if mode=="complete"{if err!=nil||len(got)!=2{t.Fatal(got,err)}}else{
   if err==nil||got!=nil{t.Fatal("partial snapshot escaped",got,err)}
   if (mode=="stream error"||mode=="query error")&&!errors.Is(err,failure){t.Fatal("stream error lost",err)}
   if (mode=="empty cancelled"||mode=="partial cancelled")&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
  }
  if mode!="query error"&&!rows.closed{t.Fatal("row stream leaked")}
 })}
 ctx,cancel:=context.WithCancel(context.Background());cancel();tx:=&capacityCandidateTx{}
 if _,err:=ReadCapacityCandidates(ctx,tx,"cpu");err!=context.Canceled||tx.calls!=0{t.Fatal("cancelled read reached database",err,tx.calls)}
}
