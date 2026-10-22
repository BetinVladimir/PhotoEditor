package main

import (
 "context"
 "errors"
 "fmt"
 "io"
 "testing"
)
func TestReportOptionsRejectUnboundedOrUnscopedReads(t *testing.T){
 tenant:="00000000-0000-4000-8000-000000000001"
 for _,args:=range [][]string{nil,{"--tenant","00000000-0000-0000-0000-000000000000"},{"--tenant",tenant,"--limit","101"},{"--tenant",tenant,"--limit","0"},{"--tenant",tenant,"--after","bad"},{"--tenant",tenant,"extra"}} {
  if _,err:=parseOptions(args);err==nil{t.Fatalf("accepted invalid scope/batch %v",args)}
 }
 options,err:=parseOptions([]string{"--tenant",tenant,"--limit","1","--after",tenant})
 if err!=nil||options.limit!=1||options.tenant!=tenant||options.after!=tenant{t.Fatalf("valid bounded scope rejected: %v",err)}
}
func TestCancelledReportDoesNotOpenOperatorConnection(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=run(ctx,[]string{"--tenant","00000000-0000-4000-8000-000000000001"},"invalid-secret-dsn",io.Discard);err!=context.Canceled{t.Fatalf("expected cancellation before database parse: %v",err)}
}

func TestReportStageErrorPreservesCancellationAndRedactsDatabaseErrors(t *testing.T){
 for _,cause:=range []error{context.Canceled,context.DeadlineExceeded}{
  got:=reportStageError(context.Background(),fmt.Errorf("database: %w",cause),"unavailable")
  if !errors.Is(got,cause){t.Fatal("stage cancellation identity lost")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if got:=reportStageError(ctx,errors.New("secret database error"),"unavailable");got!=context.Canceled{t.Fatal("current cancellation hidden")}
 if got:=reportStageError(context.Background(),errors.New("secret database error"),"unavailable");got.Error()!="unavailable"{t.Fatal("database detail exposed")}
}
