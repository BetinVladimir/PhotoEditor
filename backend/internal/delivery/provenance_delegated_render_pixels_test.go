package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderPixelsCatalogRefusesMissingScopeAndCancellation(t *testing.T){
 reader:=DelegatedRenderPixelsCatalog{}
 if _,err:=reader.LookupDelegatedRenderPixels(context.Background(),
  provenance.CanonicalSigningTask{},v2input.Ref{},review.WorkingImage{});err==nil{
  t.Fatal("missing delegated scope accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.LookupDelegatedRenderPixels(ctx,provenance.CanonicalSigningTask{},
  v2input.Ref{},review.WorkingImage{});!errors.Is(err,context.Canceled){
  t.Fatal("cancellation ignored",err)
 }
}
