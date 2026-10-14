package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2draft"
)

func TestDelegatedRenderQualificationRefusesMissingScopeAndCancellation(t *testing.T){
 reader:=DelegatedRenderQualificationCatalog{}
 if _,err:=reader.ReadDelegatedRenderQualification(context.Background(),
  provenance.CanonicalSigningTask{},v2draft.FrozenRenderRecipe{});err==nil{
  t.Fatal("missing delegated qualification scope accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.ReadDelegatedRenderQualification(ctx,
  provenance.CanonicalSigningTask{},v2draft.FrozenRenderRecipe{});
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
