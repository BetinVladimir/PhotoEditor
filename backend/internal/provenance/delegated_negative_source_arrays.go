package provenance

import (
 "context"
 "strconv"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type DelegatedNegativeArraySpec struct {
 Ref v2input.Ref
 Width, Height int64
 Channels int
}

func ResolveDelegatedNegativeSourceArraySpec(task CanonicalSigningTask,
 evidence SigningNegativePixelEvidence, role string) (DelegatedNegativeArraySpec, error) {
 var zero DelegatedNegativeArraySpec
 sha, err := canonical.Hash(task)
 if err != nil || sha != evidence.taskDigest || task.Operation != "optical.negative" ||
  task.Version != "1" { return zero, ErrInvalid }
 spec, err := signingNegativeSourceArraySpec(evidence, role)
 if err != nil || !ref(spec.pin) || spec.width < 1 || spec.height < 1 ||
  spec.width > 200000000 || spec.height > 200000000/spec.width ||
  spec.channels < 1 || spec.channels > 3 { return zero, ErrInvalid }
 return DelegatedNegativeArraySpec{Ref:spec.pin,Width:spec.width,
  Height:spec.height,Channels:spec.channels},nil
}

func DelegatedNegativeSourceDocument(evidence SigningNegativePixelEvidence) ([]byte,error) {
 return canonical.Marshal(evidence.source)
}

type DelegatedNegativeSourceArrayCatalog interface {
 LookupDelegatedNegativeSourceArray(context.Context, CanonicalSigningTask,
  SigningNegativePixelEvidence, string) (objectstore.Object, error)
}

type BoundDelegatedNegativeSourceArraysVerifier struct {
 Catalog DelegatedNegativeSourceArrayCatalog
 Objects SigningObjects
}

// Verify streams every source RGB, alpha and dense-warp inverse-field array.
// Shared artifacts are checked for each occurrence and its expected shape.
func (s BoundDelegatedNegativeSourceArraysVerifier) Verify(ctx context.Context,
 task CanonicalSigningTask, evidence SigningNegativePixelEvidence) error {
 if err := ctx.Err(); err != nil { return err }
 sha, err := canonical.Hash(task)
 if err != nil || sha != evidence.taskDigest || s.Catalog == nil ||
  s.Objects == nil || task.Operation != "optical.negative" || task.Version != "1" { return ErrInvalid }
 width, height := evidence.source.Canvas.Width, evidence.source.Canvas.Height
 if width < 1 || height < 1 || width > 200000000 || height > 200000000/width { return ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return err }
 roles := []string{"pixels"}
 if evidence.source.Alpha != nil { roles = append(roles, "alpha") }
 fields, err := signingNegativeInverseFields(evidence); if err != nil { return err }
 for i, field := range fields {
  if field.Inverse != nil { roles = append(roles, "InverseField/"+strconv.Itoa(i)) }
 }
 for _, role := range roles {
  spec, err := signingNegativeSourceArraySpec(evidence, role)
  if err != nil { return err }
  object, err := s.Catalog.LookupDelegatedNegativeSourceArray(ctx, owned, evidence, role)
  if err != nil { return err }
  size := 4*spec.width*spec.height*int64(spec.channels)
  if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
   object.SHA256 != spec.pin.SHA256 || object.ContentType != "application/x-npy" ||
   object.Bytes < size || object.Bytes > size+65536 { return ErrInvalid }
  stream, err := s.Objects.Open(ctx, object)
  if err != nil { if stream != nil { _ = stream.Close() }; return err }
  if stream == nil { return ErrInvalid }
  var verifyErr error
  if role == "alpha" {
   verifyErr = masknpy.VerifyAlphaNPY(ctx, stream, object.Bytes,
    object.SHA256, spec.width, spec.height)
  } else {
   verifyErr = masknpy.VerifyFloat32ArrayNPY(ctx, stream, object.Bytes,
    object.SHA256, spec.width, spec.height, spec.channels, false)
  }
  closeErr := stream.Close()
  if err := ctx.Err(); err != nil { return err }
  if verifyErr != nil { return verifyErr }
  if closeErr != nil { return closeErr }
  current, err := s.Catalog.LookupDelegatedNegativeSourceArray(ctx, owned, evidence, role)
  if err != nil { return err }
  if current != object { return ErrInvalid }
 }
 return ctx.Err()
}
