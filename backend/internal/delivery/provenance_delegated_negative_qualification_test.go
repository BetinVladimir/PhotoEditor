package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/provenance"
)

func TestDelegatedNegativeQualificationRejectsUnconfiguredAndCancelled(t *testing.T) {
 reader := DelegatedNegativeQualificationCatalog{}
 if _, err := reader.ReadDelegatedNegativeQualification(context.Background(), provenance.CanonicalSigningTask{}); err == nil {
  t.Fatal("unconfigured qualification accepted")
 }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, err := reader.ReadDelegatedNegativeQualification(ctx, provenance.CanonicalSigningTask{}); !errors.Is(err, context.Canceled) {
  t.Fatal("cancelled qualification reached authority", err)
 }
}
