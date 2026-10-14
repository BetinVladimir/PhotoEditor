package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedRenderClosureRequiresResolverAndCancellation(t *testing.T){
 source:=BoundDelegatedRenderClosureSource{}
 if _,err:=source.ReadDelegatedRenderClosure(context.Background(),
  CanonicalSigningTask{});err==nil{t.Fatal("unconfigured render closure accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.ReadDelegatedRenderClosure(ctx,CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
 if err:=VerifyDelegatedRenderTransport(context.Background(),nil,
  CanonicalSigningTask{},nil);err==nil{t.Fatal("unconfigured transport accepted")}
}
