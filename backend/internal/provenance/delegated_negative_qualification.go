package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type DelegatedNegativeQualificationReader interface {
 ReadDelegatedNegativeQualification(context.Context, CanonicalSigningTask) (v2optical.NegativeQualificationPin, error)
}

// ReadDelegatedNegativeQualificationContent binds a retained qualification pin
// to independently published locks and exact task-scoped document bytes.
func ReadDelegatedNegativeQualificationContent(ctx context.Context,
 historical DelegatedNegativeQualificationReader, documents DelegatedTaskDocuments,
 task CanonicalSigningTask, locks v2qualification.DeploymentLocks) (v2optical.VerifiedNegativeQualification, error) {
 var zero v2optical.VerifiedNegativeQualification
 if err := ctx.Err(); err != nil { return zero, err }
 if historical == nil || documents == nil || task.Operation != "optical.negative" ||
  task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil { return zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return zero, err }
 locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
 locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
 locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
 locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
 if _, err := locks.Expected(); err != nil { return zero, err }
 pin, err := historical.ReadDelegatedNegativeQualification(ctx, owned)
 if err != nil { return zero, err }
 if _, err := pin.Compile(); err != nil { return zero, err }
 type witness struct { pin v2input.Ref; raw []byte; maximum int }
 witnesses := []witness{}
 read := func(reference v2input.Ref, maximum int) ([]byte, error) {
  isolated, err := ownSigningProducerTask(owned)
  if err != nil { return nil, err }
  raw, err := documents.ReadDelegatedTaskDocument(ctx, isolated, reference, maximum)
  if err != nil { return nil, err }
  raw = append([]byte(nil), raw...)
  if !pinned(raw, reference, maximum) { return nil, ErrInvalid }
  witnesses = append(witnesses, witness{reference, raw, maximum})
  return raw, nil
 }
 paramsRaw, err := read(owned.Params, 65536); if err != nil { return zero, err }
 params, err := v2optical.ParseNegativeParams(paramsRaw); if err != nil { return zero, err }
 if params.Calibration != pin.Calibration { return zero, ErrInvalid }
 source, err := read(params.Base.Source, 65536); if err != nil { return zero, err }
 image, err := v2optical.VerifyNegativeWorkingImage(params, source); if err != nil { return zero, err }
 report, err := read(pin.Report, 8<<20); if err != nil { return zero, err }
 calibration, err := read(pin.Calibration, 65536); if err != nil { return zero, err }
 verified, err := v2optical.VerifyNegativeQualificationEvidence(pin, locks,
  report, calibration, params, image.Color.ICCSHA256)
 if err != nil { return zero, err }
 for _, witness := range witnesses {
  isolated, err := ownSigningProducerTask(owned)
  if err != nil { return zero, err }
  current, err := documents.ReadDelegatedTaskDocument(ctx, isolated, witness.pin, witness.maximum)
  if err != nil { return zero, err }
  if !bytes.Equal(current, witness.raw) { return zero, ErrInvalid }
 }
 current, err := historical.ReadDelegatedNegativeQualification(ctx, owned)
 if err != nil { return zero, err }
 if current != pin { return zero, ErrInvalid }
 return verified, ctx.Err()
}
