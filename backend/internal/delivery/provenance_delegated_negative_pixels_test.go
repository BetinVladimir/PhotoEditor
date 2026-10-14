package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedNegativePixelsCatalogRefusesMissingScopeAndCancellation(t *testing.T) {
 reader := DelegatedNegativePixelsCatalog{}
 if _, err := reader.LookupDelegatedNegativePixels(context.Background(),
  provenance.CanonicalSigningTask{}, v2input.Ref{}, review.WorkingImage{}); err == nil {
  t.Fatal("missing delegated scope accepted")
 }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, err := reader.LookupDelegatedNegativePixels(ctx,
  provenance.CanonicalSigningTask{}, v2input.Ref{}, review.WorkingImage{}); !errors.Is(err, context.Canceled) {
  t.Fatal("cancellation ignored", err)
 }
}
