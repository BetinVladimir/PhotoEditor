package provenance

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type delegatedAdoptedResultFunc func(context.Context, CanonicalSigningTask) (v2input.Ref, v2draft.ImageResultDocument, error)
func (f delegatedAdoptedResultFunc) ReadDelegatedAdoptedResult(ctx context.Context,
 task CanonicalSigningTask) (v2input.Ref, v2draft.ImageResultDocument, error) { return f(ctx, task) }

func TestDelegatedNegativeReportRequiresAdoptedEvidenceAndCancellation(t *testing.T) {
 task := CanonicalSigningTask{Task: uuid.New(), Operation: "optical.negative", Version: "1", ReviewRequired: true}
 calls := 0
 adopted := delegatedAdoptedResultFunc(func(context.Context, CanonicalSigningTask) (v2input.Ref, v2draft.ImageResultDocument, error) {
  calls++
  return v2input.Ref{}, v2draft.ImageResultDocument{}, ErrInvalid
 })
 docs := delegatedDocumentsFunc(func(context.Context, CanonicalSigningTask, v2input.Ref, int) ([]byte, error) {
  t.Fatal("documents read without adopted result")
  return nil, nil
 })
 if _, _, err := ReadDelegatedNegativeReport(context.Background(), nil, docs, task); err == nil || calls != 0 {
  t.Fatal("nil adopted reader accepted", err)
 }
 if _, _, err := ReadDelegatedNegativeReport(context.Background(), adopted, docs, task); !errors.Is(err, ErrInvalid) || calls != 1 {
  t.Fatal("missing adopted result accepted", err, calls)
 }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, _, err := ReadDelegatedNegativeReport(ctx, adopted, docs, task); !errors.Is(err, context.Canceled) || calls != 1 {
  t.Fatal("cancelled read reached authority", err, calls)
 }
}
