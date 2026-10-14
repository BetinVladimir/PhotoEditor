package provenance

import (
 "bytes"
 "context"
 "encoding/binary"
 "encoding/json"
 "io"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type delegatedMaskCatalogFunc func(context.Context, CanonicalSigningTask, v2input.Ref, review.MaskRef) (objectstore.Object, error)
func (f delegatedMaskCatalogFunc) LookupDelegatedNegativeMask(ctx context.Context, task CanonicalSigningTask,
 ref v2input.Ref, mask review.MaskRef) (objectstore.Object, error) { return f(ctx, task, ref, mask) }

func TestDelegatedNegativeMaskCounterPinsPhysicalVersion(t *testing.T) {
 header := "{'descr': '<f4', 'fortran_order': False, 'shape': (1, 2), }"
 header += strings.Repeat(" ", (64-(10+len(header)+1)%64)%64) + "\n"
 raw := append([]byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0}, byte(len(header)), byte(len(header)>>8))
 raw = append(raw, []byte(header)...); raw = append(raw, make([]byte, 8)...)
 binary.LittleEndian.PutUint32(raw[len(raw)-4:], 0x3f800000)
 for _, mode := range []string{"valid", "changed_version", "hash", "truncated", "cancelled"} {
  t.Run(mode, func(t *testing.T) {
   sha := bodyDigest(raw)
   mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: sha},
    Canvas: review.Canvas{Width: 2, Height: 1}, Selector: "changed", Confidence: 1}
   task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(),
    Attempt: 1, Fence: 1, Operation: "optical.negative", Version: "1",
    Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha},
    InputDigest: sha, ResultDigest: sha, Inputs: json.RawMessage(`[]`),
    Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
   object := objectstore.Object{Key: "mask", VersionID: "v1", SHA256: sha,
    Bytes: int64(len(raw)), ContentType: "application/x-npy"}
   calls, opens := 0, 0
   catalog := delegatedMaskCatalogFunc(func(context.Context, CanonicalSigningTask,
    v2input.Ref, review.MaskRef) (objectstore.Object, error) {
    calls++
    value := object
    if mode == "changed_version" && calls == 2 { value.VersionID = "v2" }
    return value, nil
   })
   objects := signingDocumentObjectsFunc(func(_ context.Context, expected objectstore.Object) (io.ReadCloser, error) {
    opens++
    if expected != object { t.Fatal("unexpected object version") }
    body := append([]byte(nil), raw...)
    if mode == "hash" { body[len(body)-1] ^= 1 }
    if mode == "truncated" { body = body[:len(body)-1] }
    return io.NopCloser(bytes.NewReader(body)), nil
   })
   ctx, cancel := context.WithCancel(context.Background()); defer cancel()
   if mode == "cancelled" { cancel() }
   count, err := (BoundDelegatedNegativeMaskCounter{Catalog: catalog, Objects: objects}).CountDelegatedNegativeMask(
    ctx, task, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, mask)
   if mode == "valid" {
    if err != nil || count != 1 || calls != 2 || opens != 1 { t.Fatal("valid mask refused", count, err) }
   } else if err == nil || count != 0 { t.Fatal("invalid mask accepted", mode, count) }
   if mode == "cancelled" && (calls != 0 || opens != 0) { t.Fatal("cancelled mask performed IO") }
  })
 }
}
