package media

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type policyRow struct { scan func(...any)error }
func (r policyRow) Scan(values ...any)error{return r.scan(values...)}
type policyDB struct { query func(context.Context,string,...any)pgx.Row }
func (d policyDB) QueryRow(ctx context.Context,q string,args ...any)pgx.Row{return d.query(ctx,q,args...)}

func TestPolicyReadsRefuseCancelledAndLateSuccessfulAdapters(t *testing.T) {
 for _,startup:=range []bool{false,true} {
  for _,late:=range []bool{false,true} {
   ctx,cancel:=context.WithCancel(context.Background())
   if !late {cancel()};calls:=0
   db:=policyDB{query:func(ctx context.Context,q string,args ...any)pgx.Row{
    calls++;deadline,ok:=ctx.Deadline();if !ok || time.Until(deadline)>5*time.Second {t.Fatal("unbounded policy read")}
    return policyRow{scan:func(values ...any)error{
     cancel();if startup{*values[0].(*bool)=true}else{value:=CurrentRendererPolicy;*values[0].(**string)=&value};return nil
    }}
   }}
   var err error
   if startup{err=RequireRendererPolicy(ctx,db)}else{_,err=PolicyForRun(ctx,db,uuid.New(),uuid.New())}
   if !errors.Is(err,context.Canceled){t.Fatal("late/cancelled policy result accepted",err)}
   if calls!=map[bool]int{false:0,true:1}[late]{t.Fatal("unexpected policy IO",calls)}
   cancel()
  }
 }
}

func TestFrozenPolicyExplicitDispatchAndUnknownRefusal(t *testing.T) {
 for _,policy:=range []string{LegacyRendererPolicy,CurrentRendererPolicy,"","unknown"} {
  db:=policyDB{query:func(context.Context,string,...any)pgx.Row{return policyRow{scan:func(values ...any)error{value:=policy;*values[0].(**string)=&value;return nil}}}}
  actual,err:=PolicyForRun(context.Background(),db,uuid.New(),uuid.New())
  if policy=="" || policy=="unknown" {if err==nil{t.Fatal("unknown frozen policy accepted")}}else if err!=nil || actual!=policy{t.Fatal(actual,err)}
 }
}

func TestRetryPolicyIsBoundToDurableLeaseAndNeverDefaults(t *testing.T) {
 tenant,event,token:=uuid.New(),uuid.New(),uuid.New()
 for _,policy:=range []string{LegacyRendererPolicy,CurrentRendererPolicy,"","unknown","NULL"} {
  db:=policyDB{query:func(ctx context.Context,q string,args ...any)pgx.Row{
   if q!="SELECT photo.media_renderer_policy_for_retry($1,$2,$3)" || len(args)!=3 || args[0]!=tenant || args[1]!=event || args[2]!=token {t.Fatal("retry scope lost")}
   if deadline,ok:=ctx.Deadline();!ok || time.Until(deadline)>5*time.Second {t.Fatal("unbounded retry read")}
   return policyRow{scan:func(values ...any)error{if policy!="NULL"{v:=policy;*values[0].(**string)=&v};return nil}}
  }}
  got,err:=PolicyForRetry(context.Background(),db,tenant,event,token)
  if policy==LegacyRendererPolicy || policy==CurrentRendererPolicy {if err!=nil || got!=policy{t.Fatal(got,err)}}else if err==nil {t.Fatal("missing/unknown retry policy defaulted")}
 }
 for _,late:=range []bool{false,true} {
  ctx,cancel:=context.WithCancel(context.Background());calls:=0;if !late{cancel()}
  db:=policyDB{query:func(context.Context,string,...any)pgx.Row{calls++;return policyRow{scan:func(values ...any)error{v:=CurrentRendererPolicy;*values[0].(**string)=&v;cancel();return nil}}}}
  _,err:=PolicyForRetry(ctx,db,tenant,event,token)
  if !errors.Is(err,context.Canceled) || calls!=map[bool]int{false:0,true:1}[late]{t.Fatal("cancelled retry read accepted",err,calls)}
  cancel()
 }
}

func TestPreviewPolicyUsesExactProducerAndRefusesAbsentPolicy(t *testing.T) {
 tenant,child,producer:=uuid.New(),uuid.New(),uuid.New()
 for _,policy:=range []string{LegacyRendererPolicy,CurrentRendererPolicy,"","unknown","NULL"} {
  db:=policyDB{query:func(ctx context.Context,q string,args ...any)pgx.Row{
   if q!="SELECT photo.media_renderer_policy_for_preview($1,$2,$3)" || len(args)!=3 || args[0]!=tenant || args[1]!=child || args[2]!=producer {t.Fatal("preview scope lost")}
   if deadline,ok:=ctx.Deadline();!ok || time.Until(deadline)>5*time.Second {t.Fatal("unbounded preview read")}
   return policyRow{scan:func(values ...any)error{if policy!="NULL"{v:=policy;*values[0].(**string)=&v};return nil}}
  }}
  got,err:=PolicyForPreview(context.Background(),db,tenant,child,producer)
  if policy==LegacyRendererPolicy || policy==CurrentRendererPolicy {if err!=nil || got!=policy{t.Fatal(got,err)}}else if err==nil {t.Fatal("missing/unknown preview policy defaulted")}
 }
 for _,late:=range []bool{false,true} {
  ctx,cancel:=context.WithCancel(context.Background());calls:=0;if !late{cancel()}
  db:=policyDB{query:func(context.Context,string,...any)pgx.Row{calls++;return policyRow{scan:func(values ...any)error{v:=LegacyRendererPolicy;*values[0].(**string)=&v;cancel();return nil}}}}
  _,err:=PolicyForPreview(ctx,db,tenant,child,producer)
  if !errors.Is(err,context.Canceled) || calls!=map[bool]int{false:0,true:1}[late]{t.Fatal("cancelled preview read accepted",err,calls)};cancel()
 }
}

func TestProfileHashForPolicyRejectsMissingExplicitPolicy(t *testing.T) {
 for _,policy:=range []string{"","unknown"} {if _,err:=ProfileHashForPolicy("preview1600","aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",policy);err==nil{t.Fatal("missing explicit policy accepted")}}
}
