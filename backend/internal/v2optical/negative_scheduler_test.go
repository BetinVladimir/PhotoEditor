package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

type negativeSchedulerFake struct { cursors []uuid.UUID; calls int; fail bool }
func(f *negativeSchedulerFake)Next(ctx context.Context,after uuid.UUID)(*NegativeSchedulerDecision,error){
 f.cursors=append(f.cursors,after)
 if after!=uuid.Nil {return nil,nil}
 return &NegativeSchedulerDecision{Tenant:uuid.New(),Project:uuid.New(),Decision:NegativeDeclinedDecision{Review:uuid.MustParse("00000000-0000-0000-0000-000000000001")}},nil
}
func(f *negativeSchedulerFake)Reconcile(context.Context,NegativeSchedulerDecision)error{f.calls++;if f.fail {return errors.New("deferred")};return nil}
func TestNegativeSchedulerRetriesFailedDecisionsOnNextSweep(t *testing.T){
 f:=&negativeSchedulerFake{fail:true};s:=&NegativeScheduler{Backend:f}
 if done,err:=s.ProcessOne(context.Background());err==nil||done {t.Fatal("failure hidden")}
 if done,err:=s.ProcessOne(context.Background());err!=nil||done {t.Fatal("sweep completion",err)}
 f.fail=false
 if done,err:=s.ProcessOne(context.Background());err!=nil||!done {t.Fatal("retry",err)}
 if f.calls!=2||len(f.cursors)!=3||f.cursors[0]!=uuid.Nil||f.cursors[1]==uuid.Nil||f.cursors[2]!=uuid.Nil {t.Fatal("cursor/retry order",f.cursors)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.ProcessOne(ctx);err==nil||f.calls!=2 {t.Fatal("cancelled call performed work")}
}
func TestNegativeSchedulerWireRequiresScopedClosedDecision(t *testing.T){
 decision:=NegativeSchedulerDecision{Tenant:uuid.New(),Project:uuid.New(),Decision:NegativeDeclinedDecision{Review:uuid.New(),OriginTask:uuid.New(),OriginRun:uuid.New(),DecisionHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Route:"skip"}}
 raw,_:=canonical.Marshal([]NegativeSchedulerDecision{decision})
 if d,err:=decodeNegativeSchedulerDecision(raw,uuid.Nil);err!=nil||d==nil||*d!=decision {t.Fatal("valid scheduler decision",err)}
 if d,err:=decodeNegativeSchedulerDecision([]byte("[]"),uuid.Nil);err!=nil||d!=nil {t.Fatal("empty page",err)}
 if _,err:=decodeNegativeSchedulerDecision(raw,decision.Decision.Review);err==nil {t.Fatal("cursor replay accepted")}
 decision.Tenant=uuid.Nil;raw,_=canonical.Marshal([]NegativeSchedulerDecision{decision})
 if _,err:=decodeNegativeSchedulerDecision(raw,uuid.Nil);err==nil {t.Fatal("unscoped decision accepted")}
 for _,body:=range []string{"null","[null]","{}",`[{"unknown":true}]`} {if _,err:=decodeNegativeSchedulerDecision([]byte(body),uuid.Nil);err==nil {t.Fatal("invalid page accepted",body)}}
}
