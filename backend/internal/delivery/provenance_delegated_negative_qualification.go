package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type DelegatedNegativeQualificationCatalog struct {
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
 Adopted DelegatedAdoptedResultReader
}

func (s DelegatedNegativeQualificationCatalog) ReadDelegatedNegativeQualification(
 ctx context.Context, task provenance.CanonicalSigningTask) (v2optical.NegativeQualificationPin, error) {
 var zero v2optical.NegativeQualificationPin
 if err := ctx.Err(); err != nil { return zero, err }
 if s.Tasks.DB == nil || s.Documents.Objects == nil ||
  s.Documents.Tasks.DB != s.Tasks.DB || s.Adopted.Tasks.DB != s.Tasks.DB ||
  s.Documents.Tasks.Lease != s.Tasks.Lease || s.Adopted.Tasks.Lease != s.Tasks.Lease ||
  s.Documents.Tasks.Scope != s.Tasks.Scope || s.Adopted.Tasks.Scope != s.Tasks.Scope ||
  s.Documents.Tasks.Ready != s.Tasks.Ready || s.Adopted.Tasks.Ready != s.Tasks.Ready ||
  s.Documents.Tasks.Slot.Ordinal != s.Tasks.Slot.Ordinal ||
  s.Adopted.Tasks.Slot.Ordinal != s.Tasks.Slot.Ordinal ||
  s.Documents.Tasks.Slot.PresetID != s.Tasks.Slot.PresetID ||
  s.Adopted.Tasks.Slot.PresetID != s.Tasks.Slot.PresetID ||
  s.Documents.Tasks.Bucket != s.Tasks.Bucket ||
  s.Adopted.Tasks.Bucket != s.Tasks.Bucket ||
  task.Task == uuid.Nil || task.Operation != "optical.negative" || task.Version != "1" {
  return zero, v2input.ErrInvalid
 }
 var roots []v2input.Ref
 if json.Unmarshal(task.Outputs, &roots) != nil || len(roots) != 1 ||
  roots[0].ArtifactID == uuid.Nil || !shaPattern.MatchString(roots[0].SHA256) {
  return zero, v2input.ErrInvalid
 }
 check := func() error {
  root, _, err := s.Adopted.ReadDelegatedAdoptedResult(ctx, task)
  if err != nil { return err }
  if root != roots[0] { return v2input.ErrInvalid }
  return ctx.Err()
 }
 if err := check(); err != nil { return zero, err }
 params, err := s.Documents.ReadDelegatedTaskDocument(ctx, task, task.Params, 65536)
 if err != nil { return zero, err }
 params = append([]byte(nil), params...)
 var profile []byte
 err = s.Tasks.DB.QueryRow(ctx, `SELECT photo.v31_delivery_negative_qualification_profile($1,$2,$3,$4,$5)`,
  s.Tasks.Lease.Expansion, s.Tasks.Lease.Token, s.Tasks.Slot.Ordinal,
  s.Tasks.Slot.PresetID, task.Task).Scan(&profile)
 if err != nil { return zero, err }
 pin, err := provenance.DecodeSigningNegativeQualification(profile, params)
 if err != nil { return zero, err }
 current, err := s.Documents.ReadDelegatedTaskDocument(ctx, task, task.Params, 65536)
 if err != nil { return zero, err }
 if !bytes.Equal(current, params) { return zero, v2input.ErrInvalid }
 if err := check(); err != nil { return zero, err }
 return pin, nil
}

var _ provenance.DelegatedNegativeQualificationReader = DelegatedNegativeQualificationCatalog{}
