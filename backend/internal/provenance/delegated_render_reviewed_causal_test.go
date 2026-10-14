package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedRenderReviewedCausalRequiresAuthorities(t *testing.T) {
 source:=BoundDelegatedRenderReviewedCausalSource{}
 if _,err:=source.ReadDelegatedRenderReviewed(context.Background(),ReadySigningExport{},
  CanonicalSigningTask{});err==nil{t.Fatal("missing review authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.ReadDelegatedRenderReviewed(ctx,ReadySigningExport{},
  CanonicalSigningTask{});!errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
 if _,err:=ResolveDelegatedRenderOutputs(context.Background(),nil,nil,
  CanonicalSigningTask{});err==nil{t.Fatal("missing output authority accepted")}
}
