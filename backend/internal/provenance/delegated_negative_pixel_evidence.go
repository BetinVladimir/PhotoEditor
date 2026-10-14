package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type DelegatedNegativeMaskCounter interface {
 CountDelegatedNegativeMask(context.Context, CanonicalSigningTask,
  v2input.Ref, review.MaskRef) (int64, error)
}

func ReadDelegatedNegativePixelEvidence(ctx context.Context,
 adopted DelegatedAdoptedResultPort, documents DelegatedTaskDocuments,
 counter DelegatedNegativeMaskCounter, task CanonicalSigningTask) (SigningNegativePixelEvidence, error) {
 var zero SigningNegativePixelEvidence
 if err := ctx.Err(); err != nil { return zero, err }
 if adopted == nil || documents == nil || counter == nil ||
  task.Operation != "optical.negative" || task.Version != "1" { return zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return zero, err }
 root, result, err := adopted.ReadDelegatedAdoptedResult(ctx, owned)
 if err != nil { return zero, err }
 type witness struct { ref v2input.Ref; raw []byte; maximum int }
 witnesses := []witness{}
 read := func(ref v2input.Ref, maximum int) ([]byte, error) {
  isolated, err := ownSigningProducerTask(owned); if err != nil { return nil, err }
  raw, err := documents.ReadDelegatedTaskDocument(ctx, isolated, ref, maximum)
  if err != nil { return nil, err }
  raw = append([]byte(nil), raw...)
  if !pinned(raw, ref, maximum) { return nil, ErrInvalid }
  witnesses = append(witnesses, witness{ref, raw, maximum})
  return raw, nil
 }
 paramsRaw, err := read(owned.Params, 65536); if err != nil { return zero, err }
 params, err := v2optical.ParseNegativeParams(paramsRaw); if err != nil { return zero, err }
 sourceRaw, err := read(params.Base.Source, 65536); if err != nil { return zero, err }
 source, err := v2optical.VerifyNegativeWorkingImage(params, sourceRaw)
 if err != nil { return zero, err }
 imageRaw, err := read(result.WorkingImage, 1<<20); if err != nil { return zero, err }
 image, err := review.ParseWorkingImage(imageRaw); if err != nil { return zero, err }
 expected := source; expected.Pixels = image.Pixels
 expectedRaw, err := canonical.Marshal(expected)
 if err != nil || !bytes.Equal(expectedRaw, imageRaw) ||
  image.Pixels.ID == result.WorkingImage.ArtifactID || image.Pixels.ID == root.ArtifactID ||
  image.Pixels.ID == params.Calibration.ArtifactID ||
  image.Pixels.ID == params.Base.Source.ArtifactID ||
  source.Alpha != nil && image.Pixels.ID == source.Alpha.ID { return zero, ErrInvalid }
 changed, err := ReadDelegatedNegativeMask(ctx, documents, owned, image, result.ChangedMask)
 if err != nil { return zero, err }
 maskRaw, err := canonical.Marshal(changed); if err != nil { return zero, err }
 witnesses = append(witnesses, witness{result.ChangedMask, maskRaw, 1<<20})
 count, err := counter.CountDelegatedNegativeMask(ctx, owned, result.ChangedMask, changed)
 if err != nil { return zero, err }
 if count < 0 || count > image.Canvas.Width*image.Canvas.Height ||
  params.Strength == 0 && count != 0 { return zero, ErrInvalid }
 for _, witness := range witnesses {
  isolated, err := ownSigningProducerTask(owned); if err != nil { return zero, err }
  current, err := documents.ReadDelegatedTaskDocument(ctx, isolated, witness.ref, witness.maximum)
  if err != nil { return zero, err }
  if !bytes.Equal(current, witness.raw) { return zero, ErrInvalid }
 }
 currentRoot, currentResult, err := adopted.ReadDelegatedAdoptedResult(ctx, owned)
 if err != nil { return zero, err }
 if currentRoot != root || currentResult.WorkingImage != result.WorkingImage ||
  currentResult.ChangedMask != result.ChangedMask || currentResult.Report != result.Report ||
  currentResult.GeneratedMask != nil { return zero, ErrInvalid }
 taskDigest, err := canonical.Hash(owned); if err != nil { return zero, err }
 if err := ctx.Err(); err != nil { return zero, err }
 return SigningNegativePixelEvidence{taskDigest: taskDigest, root: root, params: params,
  result: result, source: source, image: image, changedPixels: count}, nil
}
