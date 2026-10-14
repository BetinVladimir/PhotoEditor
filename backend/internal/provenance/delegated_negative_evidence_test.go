package provenance

import (
 "context"
 "errors"
 "testing"
)

func TestDelegatedNegativeEvidenceRefusesUnconfiguredAndCancelled(t *testing.T) {
 reader:=BoundDelegatedNegativeEvidenceReader{}
 if _,err:=reader.ReadDelegatedNegativeEvidence(context.Background(),CanonicalSigningTask{});err==nil{
  t.Fatal("unconfigured evidence reader accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.ReadDelegatedNegativeEvidence(ctx,CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
