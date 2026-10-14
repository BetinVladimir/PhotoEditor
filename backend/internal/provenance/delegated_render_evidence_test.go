package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedRenderEvidenceRequiresCompleteAuthorities(t *testing.T){
 source:=BoundDelegatedRenderEvidenceSource{}
 if _,err:=source.ReadDelegatedRenderEvidence(context.Background(),
  CanonicalSigningTask{});err==nil{t.Fatal("unconfigured evidence accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.ReadDelegatedRenderEvidence(ctx,CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
