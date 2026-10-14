package provenance

import (
 "bytes"
 "context"
 "encoding/binary"
 "io"
 "math"
 "testing"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
)

type delegatedSourceArrayCatalogFunc func(context.Context, CanonicalSigningTask,
 SigningNegativePixelEvidence, string) (objectstore.Object, error)
func (f delegatedSourceArrayCatalogFunc) LookupDelegatedNegativeSourceArray(ctx context.Context,
 task CanonicalSigningTask, evidence SigningNegativePixelEvidence, role string) (objectstore.Object, error) {
 return f(ctx, task, evidence, role)
}

func TestDelegatedNegativeSourceArraysVerifyExactPhysicalVersions(t *testing.T) {
 npy := func(values ...float32) []byte {
  header := "{'descr': '<f4', 'fortran_order': False, 'shape': (1, 1, 3), }\n"
  raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
  binary.LittleEndian.PutUint16(raw[8:], uint16(len(header)))
  raw = append(raw, []byte(header)...)
  for _, value := range values {
   var encoded [4]byte
   binary.LittleEndian.PutUint32(encoded[:], math.Float32bits(value))
   raw = append(raw, encoded[:]...)
  }
  return raw
 }
 for _, mode := range []string{"valid_hdr", "nan", "version", "cancelled"} {
  t.Run(mode, func(t *testing.T) {
   raw := npy(2, .5, .1)
   if mode == "nan" { raw = npy(2, .5, float32(math.NaN())) }
   task := CanonicalSigningTask{Operation: "optical.negative", Version: "1"}
   sha, _ := canonical.Hash(task)
   evidence := SigningNegativePixelEvidence{taskDigest: sha, source: review.WorkingImage{
    Canvas: review.Canvas{Width: 1, Height: 1},
    Pixels: review.ArtifactRef{SHA256: bodyDigest(raw)},
   }}
   calls, opens := 0, 0
   catalog := delegatedSourceArrayCatalogFunc(func(context.Context, CanonicalSigningTask,
    SigningNegativePixelEvidence, string) (objectstore.Object, error) {
    calls++
    version := "v1"
    if mode == "version" && calls > 1 { version = "v2" }
    return objectstore.Object{Key: "pixels", VersionID: version, SHA256: bodyDigest(raw),
     Bytes: int64(len(raw)), ContentType: "application/x-npy"}, nil
   })
   objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
    opens++
    return io.NopCloser(bytes.NewReader(raw)), nil
   })
   ctx, cancel := context.WithCancel(context.Background()); defer cancel()
   if mode == "cancelled" { cancel() }
   err := (BoundDelegatedNegativeSourceArraysVerifier{Catalog: catalog, Objects: objects}).Verify(ctx, task, evidence)
   if mode == "valid_hdr" {
    if err != nil || calls != 2 || opens != 1 { t.Fatal("HDR source refused", err) }
   } else if err == nil { t.Fatal("invalid source accepted", mode) }
   if mode == "cancelled" && (calls != 0 || opens != 0) { t.Fatal("cancelled source performed IO") }
  })
 }
}
