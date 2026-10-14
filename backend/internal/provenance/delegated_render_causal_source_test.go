package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedRenderCausalSourceRequiresAuthoritiesAndCancellation(t *testing.T) {
 source:=BoundDelegatedRenderCausalSource{}
 if _,err:=source.ReadDelegatedRenderCausal(context.Background(),ReadySigningExport{},
  CanonicalSigningTask{});err==nil{t.Fatal("unconfigured render causal source accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.ReadDelegatedRenderCausal(ctx,ReadySigningExport{},
  CanonicalSigningTask{});!errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
