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

func TestWatermarkClaimsPreserveBothContentAncestries(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("invalid fixture")
	}
	for _, mode := range []string{"valid", "zero", "alpha", "shared_parent", "self_mark", "mark_alpha", "color", "placement", "input", "result", "report", "mark_parent", "changed_document", "changed_parent", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			source, err := review.ParseWorkingImage(images[0])
			if err != nil {
				t.Fatal(err)
			}
			if mode == "alpha" || mode == "self_mark" {
				source.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			}
			sourceRaw, sourceRef := contractBytes(t, source)
			mark := source
			mark.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			mark.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
			mark.Canvas.Width = 80
			mark.Canvas.Height = 40
			if mode == "mark_alpha" {
				mark.Alpha = nil
			}
			if mode == "color" {
				mark.Color.ICCSHA256 = strings.Repeat("d", 64)
			}
			markRaw, markRef := contractBytes(t, mark)
			if mode == "self_mark" {
				mark = source
				markRaw = sourceRaw
				markRef = sourceRef
			}
			p := signingWatermarkParams{Image: sourceRef, Watermark: markRef, Opacity: .5, Anchor: "bottom_right", MarginX: .03, MarginY: .03, MaxWidth: .25, Algorithm: "linear_alpha_v1"}
			if mode == "zero" {
				p.Opacity = 0
			}
			if mode == "placement" {
				p.MarginX = 1
			}
			paramsRaw, paramsRef := contractBytes(t, p)
			pixel := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
			outputs := []v2input.Ref{pixel}
			after := source
			after.Pixels = review.ArtifactRef{ID: pixel.ArtifactID, SHA256: pixel.SHA256}
			if source.Alpha != nil {
				alpha := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
				outputs = append(outputs, alpha)
				after.Alpha = &review.ArtifactRef{ID: alpha.ArtifactID, SHA256: alpha.SHA256}
			}
			if mode == "result" {
				after.Color.ICCSHA256 = strings.Repeat("d", 64)
			}
			resultRaw, resultRef := contractBytes(t, after)
			report := signingDerivativeReport{Image: sourceRef, Operation: "watermark", Applied: p.Opacity > 0, Width: source.Canvas.Width, Height: source.Canvas.Height, MaxDelta: .2, Warnings: []string{}}
			if mode == "zero" {
				report.MaxDelta = 0
				report.Warnings = []string{"zero_opacity"}
			}
			if mode == "report" {
				report.Operation = "sharpen"
			}
			reportRaw, reportRef := contractBytes(t, report)
			outputs = append(outputs, resultRef, reportRef)
			inputs := []v2input.Ref{sourceRef, {ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}}
			if source.Alpha != nil {
				inputs = append(inputs, v2input.Ref{ArtifactID: source.Alpha.ID, SHA256: source.Alpha.SHA256})
			}
			inputs = append(inputs, markRef, v2input.Ref{ArtifactID: mark.Pixels.ID, SHA256: mark.Pixels.SHA256})
			if mark.Alpha != nil {
				inputs = append(inputs, v2input.Ref{ArtifactID: mark.Alpha.ID, SHA256: mark.Alpha.SHA256})
			}
			if mode == "input" {
				inputs[1] = pixel
			}
			inputRaw, _ := canonical.Marshal(inputs)
			outputRaw, _ := canonical.Marshal(outputs)
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New()}
			e := CanonicalSigningTask{Task: uuid.New(), Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "derivative.watermark", Version: "1", Params: paramsRef, Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`), InputDigest: strings.Repeat("a", 64), ResultDigest: strings.Repeat("b", 64)}
			documents := map[v2input.Ref][]byte{sourceRef: sourceRaw, paramsRef: paramsRaw, resultRef: resultRaw, reportRef: reportRaw}
			documents[markRef] = markRaw
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
			sourceParent, markParent := uuid.New(), uuid.New()
			if mode == "shared_parent" || mode == "self_mark" {
				markParent = sourceParent
			}
			originCalls := map[v2input.Ref]int{}
			producer := signingProducerFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, r v2input.Ref) (uuid.UUID, error) {
				originCalls[r]++
				if mode == "changed_parent" && originCalls[r] > 1 {
					return uuid.New(), nil
				}
				if r == sourceRef {
					return sourceParent, nil
				}
				if r == markRef {
					if mode == "mark_parent" {
						return uuid.Nil, ErrInvalid
					}
					return markParent, nil
				}
				return uuid.Nil, ErrInvalid
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (PresentationSigningTaskResolver{Documents: reader, Producers: producer}).ResolveSigningTaskLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, e)
			if mode == "valid" || mode == "zero" || mode == "alpha" || mode == "shared_parent" || mode == "self_mark" {
				parents, content := 2, 2
				if mode == "shared_parent" || mode == "self_mark" {
					parents = 1
				}
				if mode == "self_mark" {
					content = 1
				}
				if err != nil || len(result.Node.Parents) != parents || len(result.Node.Inputs) != content || result.Node.Action.Generated || originCalls[sourceRef] != 2 || originCalls[markRef] != 2 {
					t.Fatal("watermark ancestry lost", err)
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid watermark claims exposed")
			}
		})
	}
}

func TestWatermarkClaimsAcceptDocumentedDefaultsOnly(t *testing.T) {
	image, mark := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	for _, mode := range []string{"valid", "null", "unknown", "zero_width", "anchor", "opacity"} {
		t.Run(mode, func(t *testing.T) {
			fields := map[string]any{"image": image, "watermark": mark, "opacity": .5, "anchor": "center"}
			switch mode {
			case "null":
				fields["margin_x"] = nil
			case "unknown":
				fields["generated"] = true
			case "zero_width":
				fields["max_width_fraction"] = 0
			case "anchor":
				fields["anchor"] = "foreign"
			case "opacity":
				fields["opacity"] = 2
			}
			raw, _ := canonical.Marshal(fields)
			p, err := decodeSigningWatermarkParams(raw)
			if mode == "valid" {
				if err != nil || p.MarginX != .03 || p.MarginY != .03 || p.MaxWidth != .25 || p.Algorithm != "linear_alpha_v1" {
					t.Fatal("defaults refused", err)
				}
			} else if err == nil {
				t.Fatal("invalid Params accepted")
			}
		})
	}
}
