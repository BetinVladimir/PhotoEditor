package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderMaskCatalogRefusesMissingScopeAndCancellation(t *testing.T){
 reader:=DelegatedRenderMaskCatalog{}
 if _,err:=reader.LookupDelegatedRenderMask(context.Background(),
  provenance.CanonicalSigningTask{},v2input.Ref{},review.MaskRef{});err==nil{
  t.Fatal("missing delegated scope accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.LookupDelegatedRenderMask(ctx,provenance.CanonicalSigningTask{},
  v2input.Ref{},review.MaskRef{});!errors.Is(err,context.Canceled){
  t.Fatal("cancellation ignored",err)
 }
}
