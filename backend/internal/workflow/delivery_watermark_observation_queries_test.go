package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
)

func TestWatermarkObservationQueriesRefuseForeignReadsBeforeTransaction(t *testing.T){
 pool:=&watermarkSourceQueryPool{tx:&watermarkSourceQueryTx{}}
 lease:=delivery.ExpansionLease{Tenant:uuid.New(),Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),SlotCount:1}
 sha:=renderDerivativeBytesSHA([]byte("params"))
 q:=watermarkObservationQueries{db:pool,lease:lease,operation:"derivative.watermark",paramsSHA:sha}
 query:=`SELECT photo.v30_watermark_saved_continuation_params($1,$2,$3,$4,$5)`
 for _,scenario:=range []string{"sql","expansion","token","slot","operation","sha","type","cancel"}{t.Run(scenario,func(t *testing.T){
  sql:=query;args:=[]any{lease.Expansion,lease.Token,0,q.operation,sha};ctx:=context.Background()
  switch scenario{
  case "sql":sql="SELECT arbitrary()"
  case "expansion":args[0]=uuid.New()
  case "token":args[1]=uuid.New()
  case "slot":args[2]=1
  case "operation":args[3]="export.encode"
  case "sha":args[4]=renderDerivativeBytesSHA([]byte("changed"))
  case "type":args[2]=int64(0)
  case "cancel":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
  }
  var body []byte
  err:=q.QueryRow(ctx,sql,args...).Scan(&body)
  if err==nil||pool.begins!=0{t.Fatal("foreign read reached transaction",scenario,err)}
  if scenario=="cancel"&&err!=context.Canceled{t.Fatal(err)}
 })}
}

func TestWatermarkPresentationObserverRefusesMissingRuntime(t *testing.T){
 var pipeline DeliveryWatermarkChildPipeline
 result,err:=pipeline.advancePresentation(context.Background(),delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},DeliveryResolvedWatermarkChild{})
 if err==nil||result.admitted||len(result.prefix)!=0{t.Fatal("missing runtime accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=pipeline.advancePresentation(ctx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},DeliveryResolvedWatermarkChild{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
}
