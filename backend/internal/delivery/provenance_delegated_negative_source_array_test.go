package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
)

func TestDelegatedNegativeSourceArrayCatalogRefusesMissingScopeAndCancellation(t *testing.T) {
 reader:=DelegatedNegativeSourceArrayCatalog{}
 if _,err:=reader.LookupDelegatedNegativeSourceArray(context.Background(),
  provenance.CanonicalSigningTask{},provenance.SigningNegativePixelEvidence{},"pixels");err==nil{
  t.Fatal("missing delegated scope accepted")
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=reader.LookupDelegatedNegativeSourceArray(ctx,
  provenance.CanonicalSigningTask{},provenance.SigningNegativePixelEvidence{},"pixels");
  !errors.Is(err,context.Canceled){t.Fatal("cancellation ignored",err)}
}
