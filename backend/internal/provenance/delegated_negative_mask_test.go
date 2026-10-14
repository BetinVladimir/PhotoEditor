package provenance

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedNegativeMaskRequiresExactGeometryAndPurpose(t *testing.T) {
 fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
 if err != nil { t.Fatal(err) }
 var images []json.RawMessage
 if json.Unmarshal(fixture, &images) != nil || len(images) == 0 { t.Fatal("missing image fixture") }
 rawImage, err := canonical.Bytes(images[0]); if err != nil { t.Fatal(err) }
 image, err := review.ParseWorkingImage(rawImage); if err != nil { t.Fatal(err) }
 for _, mode := range []string{"valid", "selector", "protected", "canvas", "hash", "mutation", "cancelled"} {
  t.Run(mode, func(t *testing.T) {
   mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
    Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "changed", Confidence: 1}
   switch mode {
   case "selector": mask.Selector = "generated"
   case "protected": mask.Protected = true
   case "canvas": mask.Canvas.Width++
   }
   raw, reference := contractBytes(t, mask)
   if mode == "hash" { reference.SHA256 = strings.Repeat("0", 64) }
   task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1,
    Fence: 1, Operation: "optical.negative", Version: "1", InputDigest: strings.Repeat("c", 64),
    ResultDigest: strings.Repeat("d", 64), Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
    Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
   ctx, cancel := context.WithCancel(context.Background()); defer cancel()
   if mode == "cancelled" { cancel() }
   reads := 0
   documents := delegatedDocumentsFunc(func(context.Context, CanonicalSigningTask, v2input.Ref, int) ([]byte, error) {
    reads++
    if mode == "mutation" && reads == 2 { return []byte(`{}`), nil }
    return raw, nil
   })
   actual, err := ReadDelegatedNegativeMask(ctx, documents, task, image, reference)
   if mode == "valid" {
    if err != nil || reads != 2 || actual.Selector != "changed" { t.Fatal("valid mask refused", err) }
   } else if err == nil { t.Fatal("invalid mask accepted", mode) }
   if mode == "cancelled" && reads != 0 { t.Fatal("cancelled mask performed IO") }
  })
 }
}
