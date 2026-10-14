package workflow

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "crypto/sha256"
 "encoding/hex"
 "os"
 "testing"
)

func TestDecodeInstalledCapacityQueuePolicy(t *testing.T){
 body,err:=os.ReadFile("../../../configs/capacity-metal-verification.json");if err!=nil{t.Fatal(err)}
 hash:=sha256.Sum256(body);pin:=hex.EncodeToString(hash[:])
 policy,err:=decodeCapacityQueuePolicy(body,pin);if err!=nil{t.Fatal(err)}
 if policy.UnknownCostSeconds!=1800||policy.TenantHeavyQueued!=2||policy.Weights["interactive"]!=4{t.Fatal(policy)}
 changed:=append([]byte(nil),body...);changed[0]=' '
 if _,err=decodeCapacityQueuePolicy(changed,pin);err==nil{t.Fatal("accepted changed bytes")}
 if _,err=decodeCapacityQueuePolicy(body,"bad");err==nil{t.Fatal("accepted malformed pin")}
}

func TestDecodeCapacityQueuePolicyRejectsInvalidInstalledFields(t *testing.T){
 for _,body:=range []string{
  `{"schema_version":2,"queue_weights":{"interactive":4,"capture":3,"batch":2,"training":1},"unknown_cost_seconds":1800,"tenant_heavy_queued":2}`,
  `{"schema_version":1,"queue_weights":{"interactive":4,"capture":3,"batch":2,"other":1},"unknown_cost_seconds":1800,"tenant_heavy_queued":2}`,
  `{"schema_version":1,"queue_weights":{"interactive":4,"capture":3,"batch":2,"training":1},"unknown_cost_seconds":null,"tenant_heavy_queued":2}`,
 }{
  hash:=sha256.Sum256([]byte(body));if _,err:=decodeCapacityQueuePolicy([]byte(body),hex.EncodeToString(hash[:]));err==nil{t.Fatal("accepted invalid fields")}
 }
}

type capacityPolicyReadTx struct {pgx.Tx;body *string;err error;cancel context.CancelFunc;calls int;pin string}
func(tx *capacityPolicyReadTx) QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 tx.calls++
 if q!="SELECT photo.capacity_scheduler_policy($1)"||len(args)!=1||args[0]!=tx.pin{return capacityPolicyReadRow{err:errors.New("foreign policy query")}}
 return capacityPolicyReadRow{body:tx.body,err:tx.err,cancel:tx.cancel}
}
type capacityPolicyReadRow struct {body *string;err error;cancel context.CancelFunc}
func(r capacityPolicyReadRow) Scan(dest ...any)error{
 if r.cancel!=nil{r.cancel()}
 if r.err!=nil{return r.err}
 *dest[0].(**string)=r.body
 return nil
}
func TestReadCapacityQueuePolicyTransactionBoundary(t *testing.T){
 body,err:=os.ReadFile("../../../configs/capacity-metal-verification.json");if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(body);pin:=hex.EncodeToString(sum[:]);text:=string(body)
 sentinel:=errors.New("policy SQL unavailable")
 for _,mode:=range []string{"valid","early-cancel","scan-cancel","query-error","missing","digest"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  tx:=&capacityPolicyReadTx{body:&text,pin:pin}
  switch mode{
  case "early-cancel":cancel()
  case "scan-cancel":tx.cancel=cancel
  case "query-error":tx.err=sentinel
  case "missing":tx.body=nil
  case "digest":changed:=text+" ";tx.body=&changed
  }
  got,err:=ReadCapacityQueuePolicy(ctx,tx,pin)
  if mode=="valid"{if err!=nil||got.UnknownCostSeconds!=1800||tx.calls!=1{t.Fatal(got,err,tx.calls)};return}
  if err==nil||got.Weights!=nil||got.UnknownCostSeconds!=0||got.TenantHeavyQueued!=0{t.Fatal("invalid policy escaped",mode,got,err)}
  if mode=="early-cancel"&&tx.calls!=0{t.Fatal("cancelled read queried SQL")}
  if (mode=="early-cancel"||mode=="scan-cancel")&&!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
  if mode=="query-error"&&!errors.Is(err,sentinel){t.Fatal("lost SQL error",err)}
 })}
}
