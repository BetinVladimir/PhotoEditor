package provenance

import (
 "context"
 "encoding/json"
 "os"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type delegatedNegativeQualificationFunc func(context.Context, CanonicalSigningTask) (v2optical.NegativeQualificationPin, error)
func (f delegatedNegativeQualificationFunc) ReadDelegatedNegativeQualification(ctx context.Context,
 task CanonicalSigningTask) (v2optical.NegativeQualificationPin, error) { return f(ctx, task) }

func TestDelegatedNegativeQualificationBindsPhysicalEvidence(t *testing.T) {
 raw, err := os.ReadFile("../v2optical/testdata/negative-qualification.json"); if err != nil { t.Fatal(err) }
 var fixture struct { Params v2optical.NegativeParams; Calibration, Report string; Pin v2optical.NegativeQualificationPin; Locks v2qualification.DeploymentLocks }
 if json.Unmarshal(raw, &fixture) != nil { t.Fatal("invalid qualification fixture") }
 source, err := os.ReadFile("../v2optical/testdata/negative-source.json"); if err != nil { t.Fatal(err) }
 image, err := review.ParseWorkingImage(source); if err != nil { t.Fatal(err) }
 fixture.Params.Base.Source.SHA256 = bodyDigest(source)
 fixture.Params.Base.CanvasHash, err = canonical.Hash(image.Canvas); if err != nil { t.Fatal(err) }
 params, paramsSHA, err := fixture.Params.Compile(); if err != nil { t.Fatal(err) }
 paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: paramsSHA}
 outputs, _ := canonical.Marshal([]v2input.Ref{{ArtifactID: uuid.New(), SHA256: paramsSHA}})
 task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1,
  Operation: "optical.negative", Version: "1", Params: paramsRef, InputDigest: paramsSHA,
  ResultDigest: paramsSHA, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
 for _, mode := range []string{"valid", "corrupt_report", "changed_source", "changed_pin", "unlicensed"} {
  t.Run(mode, func(t *testing.T) {
   calls := 0
   historical := delegatedNegativeQualificationFunc(func(context.Context, CanonicalSigningTask) (v2optical.NegativeQualificationPin, error) {
    calls++
    pin := fixture.Pin
    if mode == "changed_pin" && calls > 1 { pin.QualificationID = uuid.New() }
    return pin, nil
   })
   bodies := map[v2input.Ref][]byte{paramsRef: params, fixture.Params.Base.Source: source,
    fixture.Pin.Report: []byte(fixture.Report), fixture.Pin.Calibration: []byte(fixture.Calibration)}
   counts := map[v2input.Ref]int{}
   documents := delegatedDocumentsFunc(func(_ context.Context, _ CanonicalSigningTask, pin v2input.Ref, _ int) ([]byte, error) {
    counts[pin]++
    if mode == "corrupt_report" && pin == fixture.Pin.Report { return []byte(`{}`), nil }
    if mode == "changed_source" && pin == fixture.Params.Base.Source && counts[pin] > 1 { return []byte(`{}`), nil }
    body, ok := bodies[pin]; if !ok { t.Fatal("unexpected document") }
    return body, nil
   })
   locks := fixture.Locks
   if mode == "unlicensed" { locks.LicenseCleared = false }
   verified, err := ReadDelegatedNegativeQualificationContent(context.Background(), historical, documents, task, locks)
   if mode == "valid" {
    if err != nil { t.Fatal(err) }
    pin, err := verified.Pin(); if err != nil || pin != fixture.Pin { t.Fatal("wrong qualification", err) }
   } else if err == nil { t.Fatal("invalid qualification accepted", mode) }
  })
 }
}
