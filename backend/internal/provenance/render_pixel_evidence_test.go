package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type signingMaskCounterFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, review.MaskRef) (int64, error)

func (f signingMaskCounterFunc) CountSigningRenderMask(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask, d v2input.Ref, m review.MaskRef) (int64, error) {
	return f(c, w, r, t, d, m)
}

func TestSigningRenderPixelEvidenceCombinesPinnedRecipeResultAndMaskCounts(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("image fixture unavailable")
	}
	imageRaw, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	source, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "generated", "counter_failure", "negative_count", "excess_count", "changed_image", "changed_adoption", "cancel_counter"} {
		t.Run(mode, func(t *testing.T) {
			documents := map[v2input.Ref][]byte{}
			put := func(value any) v2input.Ref {
				raw, reference := contractBytes(t, value)
				documents[reference] = raw
				return reference
			}
			sourceRef := put(source)
			canvasHash, err := canonical.Hash(source.Canvas)
			if err != nil {
				t.Fatal(err)
			}
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: canvasHash}
			style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
			recipeRaw, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := v2draft.BuildFrozenRenderRecipe(recipeRaw, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("d", 64), ModelLockSHA: strings.Repeat("e", 64)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			params := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(frozen)}
			documents[params] = frozen
			output := source
			output.Pixels.ID = uuid.New()
			outputRef := put(output)
			changed := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Canvas: output.Canvas, TransformChain: output.TransformChain, Selector: "changed", Confidence: 1}
			changedRef := put(changed)
			result := v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: outputRef, ChangedMask: changedRef, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
			if mode == "generated" {
				generated := changed
				generated.Selector = "generated"
				generated.Artifact.ID = uuid.New()
				ref := put(generated)
				result.GeneratedMask = &ref
			}
			root := put(result)
			outputs, err := canonical.Marshal([]v2input.Ref{root})
			if err != nil {
				t.Fatal(err)
			}
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: strings.Repeat("a", 64), ResultDigest: strings.Repeat("b", 64), Params: params, Inputs: json.RawMessage(`[]`), Outputs: outputs, Metrics: json.RawMessage(`{}`)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}
			adoptions := 0
			adoption := signingRenderAdoptionFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (v2input.Ref, error) {
				adoptions++
				reference := root
				if mode == "changed_adoption" && adoptions == 3 {
					reference.ArtifactID = uuid.New()
				}
				return reference, nil
			})
			counts := 0
			cause := errors.New("physical mask invalid")
			counter := signingMaskCounterFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, _ v2input.Ref, mask review.MaskRef) (int64, error) {
				counts++
				switch mode {
				case "counter_failure":
					return 0, cause
				case "negative_count":
					return -1, nil
				case "excess_count":
					return output.Canvas.Width*output.Canvas.Height + 1, nil
				case "changed_image":
					documents[outputRef] = []byte(`{}`)
				case "cancel_counter":
					cancel()
				}
				if mask.Selector == "generated" {
					return 2, nil
				}
				return 3, nil
			})
			reader := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, reference v2input.Ref, _ int) ([]byte, error) {
				raw, ok := documents[reference]
				if !ok {
					return nil, ErrInvalid
				}
				return raw, nil
			})
			evidence, err := ReadSigningRenderPixelEvidence(ctx, adoption, reader, counter, who, ready, task)
			if mode == "valid" || mode == "generated" {
				want := int64(0)
				if mode == "generated" {
					want = 2
				}
				if err != nil || evidence.Root() != root || evidence.ChangedPixels() != 3 || evidence.GeneratedPixels() != want {
					t.Fatal("valid bundle rejected", err)
				}
				return
			}
			if err == nil || evidence.Root().ArtifactID != uuid.Nil {
				t.Fatal("invalid bundle exposed evidence")
			}
			if mode == "counter_failure" && !errors.Is(err, cause) {
				t.Fatal("counter cause lost", err)
			}
		})
	}
}
