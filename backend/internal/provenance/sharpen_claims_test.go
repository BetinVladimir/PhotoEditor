package provenance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestSharpenClaimsBindSourceResultAndReport(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("invalid fixture")
	}
	for _, mode := range []string{"valid", "zero", "alpha", "radius", "algorithm", "input", "color", "pixels", "report_delta", "report_source", "warnings", "changed_document", "changed_parent", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			source, err := review.ParseWorkingImage(images[0])
			if err != nil {
				t.Fatal(err)
			}
			if mode == "alpha" {
				source.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			}
			sourceRaw, sourceRef := contractBytes(t, source)
			p := signingSharpenParams{Image: sourceRef, Radius: 1, Strength: .5, Threshold: .01, MaxDelta: .03, Algorithm: "luma_unsharp_v1"}
			if mode == "zero" {
				p.Strength = 0
			}
			if mode == "radius" {
				p.Radius = 6
			}
			if mode == "algorithm" {
				p.Algorithm = "other"
			}
			paramsRaw, paramsRef := contractBytes(t, p)
			pixel := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			after := source
			after.Pixels = review.ArtifactRef{ID: pixel.ArtifactID, SHA256: pixel.SHA256}
			outputs := []v2input.Ref{pixel}
			if source.Alpha != nil {
				alpha := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
				outputs = append(outputs, alpha)
				after.Alpha = &review.ArtifactRef{ID: alpha.ArtifactID, SHA256: alpha.SHA256}
			}
			if mode == "color" {
				after.Color.ICCSHA256 = strings.Repeat("d", 64)
			}
			if mode == "pixels" {
				after.Pixels.ID = uuid.New()
			}
			resultRaw, resultRef := contractBytes(t, after)
			report := signingDerivativeReport{Image: sourceRef, Operation: "sharpen", Applied: p.Strength > 0, Width: source.Canvas.Width, Height: source.Canvas.Height, MaxDelta: .02, Warnings: []string{}}
			if mode == "zero" {
				report.MaxDelta = 0
				report.Warnings = []string{"zero_strength"}
			}
			if mode == "report_delta" {
				report.MaxDelta = .5
			}
			if mode == "report_source" {
				report.Image.ArtifactID = uuid.New()
			}
			if mode == "warnings" {
				report.Warnings = []string{"zero_strength"}
			}
			reportRaw, reportRef := contractBytes(t, report)
			outputs = append(outputs, resultRef, reportRef)
			inputs := []v2input.Ref{sourceRef, {ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}}
			if source.Alpha != nil {
				inputs = append(inputs, v2input.Ref{ArtifactID: source.Alpha.ID, SHA256: source.Alpha.SHA256})
			}
			if mode == "input" {
				inputs[1] = pixel
			}
			inputRaw, _ := canonical.Marshal(inputs)
			outputRaw, _ := canonical.Marshal(outputs)
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New()}
			e := CanonicalSigningTask{Task: uuid.New(), Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "derivative.sharpen", Version: "1", Params: paramsRef, Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`), InputDigest: strings.Repeat("e", 64), ResultDigest: strings.Repeat("f", 64)}
			documents := map[v2input.Ref][]byte{sourceRef: sourceRaw, paramsRef: paramsRaw, resultRef: resultRaw, reportRef: reportRaw}
			calls := map[v2input.Ref]int{}
			reader := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
				calls[r]++
				raw, ok := documents[r]
				if !ok {
					return nil, ErrInvalid
				}
				if mode == "changed_document" && calls[r] > 1 {
					return []byte(`{}`), nil
				}
				return append([]byte(nil), raw...), nil
			})
			parent := uuid.New()
			producerCalls := 0
			producer := signingProducerFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, r v2input.Ref) (uuid.UUID, error) {
				producerCalls++
				if r != sourceRef {
					return uuid.Nil, ErrInvalid
				}
				if mode == "changed_parent" && producerCalls > 1 {
					return uuid.New(), nil
				}
				return parent, nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (PresentationSigningTaskResolver{Documents: reader, Producers: producer}).ResolveSigningTaskLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, e)
			if mode == "valid" || mode == "zero" || mode == "alpha" {
				if err != nil || len(result.Node.Parents) != 1 || result.Node.Parents[0] != parent || result.Node.Action.Generated || result.Node.Action.ModelLockSHA != nil || result.Node.Action.ReviewID != nil || producerCalls != 2 {
					t.Fatal("sharpen claims lost", err)
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid sharpen claims exposed")
			}
		})
	}
}
