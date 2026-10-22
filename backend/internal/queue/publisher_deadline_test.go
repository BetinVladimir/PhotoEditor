package queue

import (
 "context"
 "errors"
 "testing"
 "time"
)
type deadlinePublisher struct{calls int;run func(context.Context)error}
func(p *deadlinePublisher)Publish(ctx context.Context,_ Envelope)error{p.calls++;return p.run(ctx)}
func TestPublishWithinLeaseRefusesExpiredClaimBeforeIO(t *testing.T){
 p:=&deadlinePublisher{run:func(context.Context)error{return nil}}
 err:=publishWithinLease(context.Background(),p,Envelope{Lease:time.Now().Add(-time.Second)})
 if !errors.Is(err,context.DeadlineExceeded)||p.calls!=0{t.Fatal("expired claim published",p.calls,err)}
}
func TestPublishWithinLeaseRefusesLateSuccessAfterCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 p:=&deadlinePublisher{run:func(context.Context)error{cancel();return nil}}
 err:=publishWithinLease(ctx,p,Envelope{Lease:time.Now().Add(time.Minute)})
 if !errors.Is(err,context.Canceled)||p.calls!=1{t.Fatal("late nil authorized mark",p.calls,err)}
}
func TestPublishWithinLeaseBoundsAndPreservesLiveConfirmation(t *testing.T){
 p:=&deadlinePublisher{run:func(ctx context.Context)error{deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>10*time.Second{t.Fatal("unbounded publish")};return nil}}
 if err:=publishWithinLease(context.Background(),p,Envelope{Lease:time.Now().Add(time.Minute)});err!=nil||p.calls!=1{t.Fatal("live confirmation lost",err)}
}
