package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

type DelegatedNegativeEvidenceReader interface {
 ReadDelegatedNegativeEvidence(context.Context, CanonicalSigningTask) (SigningNegativeEvidence, error)
}

type BoundDelegatedNegativeCausalSource struct {
 Evidence DelegatedNegativeEvidenceReader
 Producers DelegatedImageProducer
 Documents DelegatedTaskDocuments
}

func (s BoundDelegatedNegativeCausalSource) ReadDelegatedNegativeCausal(ctx context.Context,
 ready ReadySigningExport, task CanonicalSigningTask) (signingNegativeCausalReceipt, error) {
 var zero signingNegativeCausalReceipt
 if err := ctx.Err(); err != nil { return zero, err }
 if s.Evidence == nil || s.Producers == nil || s.Documents == nil ||
  ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil { return zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return zero, err }
 read := func() (SigningNegativeEvidence, error) {
  isolated, err := ownSigningProducerTask(owned)
  if err != nil { return SigningNegativeEvidence{}, err }
  evidence, err := s.Evidence.ReadDelegatedNegativeEvidence(ctx, isolated)
  if err != nil { return SigningNegativeEvidence{}, err }
  stamp, err := compileSigningNegativeEvidenceStamp(owned, evidence)
  if err != nil || stamp != evidence.stamp { return SigningNegativeEvidence{}, ErrInvalid }
  return evidence, nil
 }
 evidence, err := read(); if err != nil { return zero, err }
 stamp := evidence.stamp
 entries, err := CompileSigningNegativeInputInventory(owned, evidence)
 if err != nil { return zero, err }
 if err := VerifyDelegatedNegativeTransport(ctx, s.Documents, owned, entries); err != nil { return zero, err }
 pixels, err := ResolveDelegatedNegativePixelAncestry(ctx, s.Producers, owned, evidence)
 if err != nil { return zero, err }
 currentStamp, err := compileSigningNegativeEvidenceStamp(owned, evidence)
 if err != nil || currentStamp != stamp { return zero, ErrInvalid }
 current, err := read(); if err != nil { return zero, err }
 if current.stamp != stamp { return zero, ErrInvalid }
 finalPixels, err := ResolveDelegatedNegativePixelAncestry(ctx, s.Producers, owned, current)
 if err != nil { return zero, err }
 if finalPixels != pixels { return zero, ErrInvalid }
 finalStamp, err := compileSigningNegativeEvidenceStamp(owned, current)
 if err != nil || finalStamp != stamp { return zero, ErrInvalid }
 if err := VerifyDelegatedNegativeTransport(ctx, s.Documents, owned, entries); err != nil { return zero, err }
 finalStamp, err = compileSigningNegativeEvidenceStamp(owned, current)
 if err != nil || finalStamp != stamp { return zero, ErrInvalid }
 causalStamp, err := canonical.Hash([]any{stamp, pixels.input, pixels.producer, entries})
 if err != nil { return zero, err }
 return signingNegativeCausalReceipt{evidence: current, pixels: finalPixels, stamp: causalStamp, entries: entries}, ctx.Err()
}

var _ DelegatedNegativeCausalReader = BoundDelegatedNegativeCausalSource{}
