package httpapi

import (
 "context"
 "errors"
 "testing"
 "time"
)

type readinessDBFunc func(context.Context) error
func (fn readinessDBFunc) Ping(ctx context.Context) error { return fn(ctx) }

func TestReadinessRequiresEveryInstalledDependency(t *testing.T) {
 sentinel := errors.New("unavailable")
 for _, tc := range []struct{ name string; database readinessDatabase; runtime func(context.Context) error; fail bool }{
  {"missing database",nil,func(context.Context)error{return nil},true},
  {"missing runtime",readinessDBFunc(func(context.Context)error{return nil}),nil,true},
  {"database down",readinessDBFunc(func(context.Context)error{return sentinel}),func(context.Context)error{t.Fatal("runtime after database failure");return nil},true},
  {"storage/auth down",readinessDBFunc(func(context.Context)error{return nil}),func(context.Context)error{return sentinel},true},
  {"all dependencies",readinessDBFunc(func(context.Context)error{return nil}),func(context.Context)error{return nil},false},
 } { t.Run(tc.name,func(t *testing.T){ if err:=checkReadiness(context.Background(),tc.database,tc.runtime);(err!=nil)!=tc.fail{t.Fatalf("unexpected readiness: %v",err)} }) }
}

func TestReadinessSharesBoundedDeadlineAndParentCancellation(t *testing.T) {
 parent,cancel:=context.WithCancel(context.Background());defer cancel()
 var first context.Context
 db:=readinessDBFunc(func(ctx context.Context)error{
  deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>3*time.Second{t.Fatal("unbounded readiness")};first=ctx;return nil
 })
 if err:=checkReadiness(parent,db,func(ctx context.Context)error{
  if ctx!=first{t.Fatal("dependency received another deadline")};cancel();return nil
 });!errors.Is(err,context.Canceled){t.Fatalf("late cancellation: %v",err)}
 if err:=checkReadiness(parent,readinessDBFunc(func(context.Context)error{t.Fatal("IO after cancellation");return nil}),func(context.Context)error{return nil});!errors.Is(err,context.Canceled){t.Fatal(err)}
}
