package provenance

import (
 "context"

 "photoeditor/backend/internal/v2optical"
)

type BoundDelegatedNegativeQualificationContent struct {
 Historical DelegatedNegativeQualificationReader
 Documents DelegatedTaskDocuments
 Policies *SigningNegativePolicyRegistry
}

// Read uses the same authenticated process-provisioned policy registry as the
// interactive signer, while historical task evidence comes from delegation.
func (s BoundDelegatedNegativeQualificationContent) Read(ctx context.Context,
 task CanonicalSigningTask) (v2optical.VerifiedNegativeQualification, error) {
 var zero v2optical.VerifiedNegativeQualification
 if err := ctx.Err(); err != nil { return zero, err }
 if s.Historical == nil || s.Documents == nil || s.Policies == nil ||
  task.Operation != "optical.negative" || task.Version != "1" ||
  normalizeCanonicalSigningTask(&task) != nil { return zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task)
 if err != nil { return zero, err }
 pin, err := s.Historical.ReadDelegatedNegativeQualification(ctx, owned)
 if err != nil { return zero, err }
 locks, err := s.Policies.Resolve(ctx, pin)
 if err != nil { return zero, err }
 result, err := ReadDelegatedNegativeQualificationContent(ctx,
  s.Historical, s.Documents, owned, locks)
 if err != nil { return zero, err }
 verified, err := result.Pin()
 if err != nil || verified != pin { return zero, ErrInvalid }
 current, err := s.Historical.ReadDelegatedNegativeQualification(ctx, owned)
 if err != nil { return zero, err }
 if current != pin { return zero, ErrInvalid }
 return result, ctx.Err()
}
