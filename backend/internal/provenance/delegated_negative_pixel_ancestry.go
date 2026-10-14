package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// ResolveDelegatedNegativePixelAncestry proves the exact negative source has
// one stable delegated producer across two authority reads.
func ResolveDelegatedNegativePixelAncestry(ctx context.Context, producers DelegatedImageProducer,
 task CanonicalSigningTask, evidence SigningNegativeEvidence) (signingNegativePixelAncestry, error) {
 var zero signingNegativePixelAncestry
 if err := ctx.Err(); err != nil { return zero, err }
 taskSHA, err := canonical.Hash(task)
 input := evidence.pixels.params.Base.Source
 if err != nil || producers == nil || task.Task == uuid.Nil ||
  task.Operation != "optical.negative" || task.Version != "1" ||
  taskSHA != evidence.taskDigest || taskSHA != evidence.pixels.taskDigest || !ref(input) {
  return zero, ErrInvalid
 }
 producer, err := producers.ResolveDelegatedImageProducer(ctx, input)
 if err != nil { return zero, err }
 if producer == uuid.Nil || producer == task.Task { return zero, ErrInvalid }
 if err := ctx.Err(); err != nil { return zero, err }
 current, err := producers.ResolveDelegatedImageProducer(ctx, input)
 if err != nil { return zero, err }
 if current != producer { return zero, ErrInvalid }
 if err := ctx.Err(); err != nil { return zero, err }
 return signingNegativePixelAncestry{input: input, producer: producer}, nil
}
