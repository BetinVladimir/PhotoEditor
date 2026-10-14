package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type DelegatedAdoptedResultPort interface {
 ReadDelegatedAdoptedResult(context.Context, CanonicalSigningTask) (v2input.Ref, v2draft.ImageResultDocument, error)
}

// ReadDelegatedNegativeReport accepts only the report child of the current
// adopted negative result. Its status requests review, not human acceptance.
func ReadDelegatedNegativeReport(ctx context.Context, adopted DelegatedAdoptedResultPort,
 documents DelegatedTaskDocuments, task CanonicalSigningTask) (v2input.Ref, v2draft.ImageReportDocument, error) {
 var noRef v2input.Ref
 var zero v2draft.ImageReportDocument
 if err := ctx.Err(); err != nil { return noRef, zero, err }
 if adopted == nil || documents == nil || task.Operation != "optical.negative" ||
  task.Version != "1" || !task.ReviewRequired { return noRef, zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task)
 if err != nil { return noRef, zero, err }
 root, result, err := adopted.ReadDelegatedAdoptedResult(ctx, owned)
 if err != nil { return noRef, zero, err }
 paramsRaw, err := documents.ReadDelegatedTaskDocument(ctx, owned, owned.Params, 65536)
 if err != nil { return noRef, zero, err }
 paramsRaw = append([]byte(nil), paramsRaw...)
 if !pinned(paramsRaw, owned.Params, 65536) { return noRef, zero, ErrInvalid }
 params, err := v2optical.ParseNegativeParams(paramsRaw)
 if err != nil { return noRef, zero, ErrInvalid }
 reportRaw, err := documents.ReadDelegatedTaskDocument(ctx, owned, result.Report, 1<<20)
 if err != nil { return noRef, zero, err }
 reportRaw = append([]byte(nil), reportRaw...)
 if !pinned(reportRaw, result.Report, 1<<20) { return noRef, zero, ErrInvalid }
 report, err := v2draft.ParseImageReport(reportRaw, result.Report)
 if err != nil || report.Operation != "optical.negative" || report.Status != "review" ||
  report.Reason == nil || *report.Reason != "negative_review" ||
  report.SkippedCount != 0 || report.OutsideMaskMaxError != 0 ||
  report.Identity != nil || report.Transform != nil || len(report.Sources) != 1 ||
  report.Sources[0] != params.Base || len(report.Registrations) != 0 ||
  len(report.ParentReports) != 0 { return noRef, zero, ErrInvalid }
 for _, witness := range []struct{ ref v2input.Ref; raw []byte; maximum int }{
  {owned.Params, paramsRaw, 65536}, {result.Report, reportRaw, 1<<20},
 } {
  current, err := documents.ReadDelegatedTaskDocument(ctx, owned, witness.ref, witness.maximum)
  if err != nil { return noRef, zero, err }
  if !bytes.Equal(current, witness.raw) { return noRef, zero, ErrInvalid }
 }
 currentRoot, currentResult, err := adopted.ReadDelegatedAdoptedResult(ctx, owned)
 if err != nil { return noRef, zero, err }
 if currentRoot != root || currentResult.WorkingImage != result.WorkingImage ||
  currentResult.ChangedMask != result.ChangedMask || currentResult.Report != result.Report ||
  currentResult.GeneratedMask != nil { return noRef, zero, ErrInvalid }
 if err := ctx.Err(); err != nil { return noRef, zero, err }
 return result.Report, report, nil
}
