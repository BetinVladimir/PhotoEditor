package provenance

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderPixelEvidenceRequiresPhysicalCounter(t *testing.T){
 if _,err:=ReadDelegatedRenderPixelEvidence(context.Background(),nil,nil,nil,
  CanonicalSigningTask{});err==nil{t.Fatal("unconfigured render pixels accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ReadDelegatedRenderPixelEvidence(ctx,nil,nil,nil,
  CanonicalSigningTask{});!errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}

func TestDelegatedRenderMaskRequiresSelectorAndAuthority(t *testing.T){
 if _,err:=ReadDelegatedRenderMask(context.Background(),nil,
  CanonicalSigningTask{},review.WorkingImage{},v2input.Ref{},"generated");err==nil{
  t.Fatal("unconfigured generated mask accepted")
 }
}
