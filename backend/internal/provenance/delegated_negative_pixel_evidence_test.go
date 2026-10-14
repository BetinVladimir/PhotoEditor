package provenance

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type delegatedMaskCounterFunc func(context.Context, CanonicalSigningTask, v2input.Ref, review.MaskRef) (int64, error)
func (f delegatedMaskCounterFunc) CountDelegatedNegativeMask(ctx context.Context,
 task CanonicalSigningTask, ref v2input.Ref, mask review.MaskRef) (int64, error) {
 return f(ctx, task, ref, mask)
}

func TestDelegatedNegativePixelEvidenceRequiresAdoptedAndPhysicalCounter(t *testing.T) {
 task := CanonicalSigningTask{Task: uuid.New(), Operation: "optical.negative", Version: "1"}
 if _, err := ReadDelegatedNegativePixelEvidence(context.Background(), nil, nil, nil, task); err == nil {
  t.Fatal("unconfigured evidence accepted")
 }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, err := ReadDelegatedNegativePixelEvidence(ctx, nil, nil, nil, task); !errors.Is(err, context.Canceled) {
  t.Fatal("cancellation ignored", err)
 }
}
