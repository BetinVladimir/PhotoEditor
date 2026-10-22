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
	workflow "photoeditor/backend/internal/encodecontract"
)

func TestResizeClaimsBindExactGeometryAndTransform(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("invalid fixture")
	}
	for _, mode := range []string{"valid", "no_resize", "alpha", "input", "canvas", "color", "pixels", "missing_transform", "report", "warnings", "changed_document", "changed_parent", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			source, err := review.ParseWorkingImage(images[0])
			if err != nil {
				t.Fatal(err)
			}
			if mode == "alpha" {
				source.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			}
			sourceRaw, sourceRef := contractBytes(t, source)
			edge := int64(200)
			width, height := int64(200), int64(150)
			if mode == "no_resize" {
				edge = 1000
				width = 400
				height = 300
			}
			p := signingResizeParams{Image: sourceRef, ExportSize: workflow.ExportSize{LongEdge: &edge, Fit: "contain"}}
			paramsRaw, paramsRef := contractBytes(t, p)
			pixel := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			outputs := []v2input.Ref{pixel}
			after := source
			after.Pixels = review.ArtifactRef{ID: pixel.ArtifactID, SHA256: pixel.SHA256}
			if source.Alpha != nil {
				alpha := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
				outputs = append(outputs, alpha)
				after.Alpha = &review.ArtifactRef{ID: alpha.ArtifactID, SHA256: alpha.SHA256}
			}
			space, err := canonical.Hash(map[string]any{"image": sourceRef, "width": width, "height": height})
			if err != nil {
				t.Fatal(err)
			}
			after.Canvas = review.Canvas{Width: width, Height: height, Orientation: 1, SpaceID: "resize:" + space[:32], SourceSHA256: source.Canvas.SourceSHA256}
			steps := []any{map[string]any{"kind": "resize", "from_canvas": source.Canvas, "to_canvas": after.Canvas, "invertibility": "invertible", "valid_region": map[string]int64{"x": 0, "y": 0, "width": width, "height": height}, "pixel_centers": "half_pixel"}}
			chainHash, err := canonical.Hash(map[string]any{"steps": steps})
			if err != nil {
				t.Fatal(err)
			}
			after.TransformChain, _ = canonical.Marshal(map[string]any{"steps": steps, "hash": chainHash})
			switch mode {
			case "canvas":
				after.Canvas.SpaceID = "foreign"
			case "color":
				after.Color.ICCSHA256 = strings.Repeat("d", 64)
			case "pixels":
				after.Pixels.ID = uuid.New()
			case "missing_transform":
				after.TransformChain = source.TransformChain
			}
			resultRaw, resultRef := contractBytes(t, after)
			report := signingDerivativeReport{Image: sourceRef, Operation: "resize", Applied: true, Width: width, Height: height, Warnings: []string{}}
			if mode == "no_resize" {
				report.Applied = false
				report.Warnings = []string{"no_resize_required"}
			}
			if mode == "report" {
				report.Width++
			}
			if mode == "warnings" {
				report.Warnings = []string{"no_resize_required"}
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
			e := CanonicalSigningTask{Task: uuid.New(), Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "derivative.resize", Version: "1", Params: paramsRef, Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`), InputDigest: strings.Repeat("e", 64), ResultDigest: strings.Repeat("f", 64)}
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
			if mode == "valid" || mode == "no_resize" || mode == "alpha" {
				if err != nil || len(result.Node.Parents) != 1 || result.Node.Parents[0] != parent || result.Node.Action.Generated || producerCalls != 2 {
					t.Fatal("resize claims lost", err)
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid resize claims exposed")
			}
		})
	}
}

func TestSigningResizeParamsAcceptOnlyDocumentedDefaultsAndNullableSelectors(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, mode := range []string{"defaults", "nullable", "box", "unknown", "null_bool", "null_fit", "both", "missing", "range"} {
		t.Run(mode, func(t *testing.T) {
			fields := map[string]any{"image": image, "long_edge": 200}
			switch mode {
			case "nullable":
				fields["width"] = nil
				fields["height"] = nil
			case "box":
				fields["long_edge"] = nil
				fields["width"] = 100
				fields["height"] = 200
			case "unknown":
				fields["algorithm"] = "foreign"
			case "null_bool":
				fields["allow_upscale"] = nil
			case "null_fit":
				fields["fit"] = nil
			case "both":
				fields["width"] = 100
				fields["height"] = 200
			case "missing":
				delete(fields, "long_edge")
			case "range":
				fields["long_edge"] = 100001
			}
			raw, _ := canonical.Marshal(fields)
			p, err := decodeSigningResizeParams(raw)
			if mode == "defaults" || mode == "nullable" || mode == "box" {
				if err != nil || p.Fit != "contain" || p.AllowUpscale {
					t.Fatal("documented defaults refused", err)
				}
			} else if err == nil {
				t.Fatal("invalid Params accepted")
			}
		})
	}
}
