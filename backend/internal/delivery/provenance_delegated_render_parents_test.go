package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
)

func TestDelegatedRenderParentCatalogRequiresScope(t *testing.T){
 reader:=DelegatedRenderParentCatalog{}
 if _,err:=reader.ReadDelegatedRenderParents(context.Background(),
  provenance.CanonicalSigningTask{});err==nil{t.Fatal("unconfigured parent reader accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.ReadDelegatedRenderParents(ctx,provenance.CanonicalSigningTask{});
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
