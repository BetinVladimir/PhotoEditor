package v2learning

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type frozenRootFixture struct{ bodies map[uuid.UUID][]byte }

func (s frozenRootFixture) Load(_ context.Context, _ LearningEvaluateLease,
	ref v2input.Ref, _ string) ([]byte, FrozenRootProvenance, error) {
	body, okay := s.bodies[ref.ArtifactID]
	if !okay {
		return nil, FrozenRootProvenance{}, ErrInvalidTrainingDraft
	}
	return body, FrozenRootProvenance{}, nil
}

type frozenImageFixture struct{}

func (frozenImageFixture) Resolve(_ context.Context, _ LearningEvaluateLease,
	_ v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) (FrozenEvaluationImage, error) {
	image, err := ParseFrozenEvaluationImage(raw, binding)
	if err != nil {
		return FrozenEvaluationImage{}, err
	}
	return FrozenEvaluationImage{Pixels: v2input.Ref{ArtifactID: image.Pixels.ID,
		SHA256: image.Pixels.SHA256}, PixelCount: image.Canvas.Width * image.Canvas.Height}, nil
}

type frozenMaskFixture struct{}

func (frozenMaskFixture) Resolve(_ context.Context, _ LearningEvaluateLease,
	_ v2input.Ref, raw []byte, image review.WorkingImage) (v2input.Ref, error) {
	return ParseFrozenProtectedMask(raw, image)
}

func (frozenMaskFixture) ResolveEffective(_ context.Context, _ LearningEvaluateLease,
	_ v2input.Ref, raw []byte, image review.WorkingImage) (v2input.Ref, error) {
	return ParseFrozenEffectiveMask(raw, image)
}

type frozenStyleFixture struct{ body []byte }

type frozenConsentFixture struct{}

func (frozenConsentFixture) Pin(_ context.Context,
	_ LearningEvaluateLease, _ []byte) error {
	return nil
}

func (f frozenStyleFixture) Load(_ context.Context, _ LearningEvaluateLease,
	_ v2input.Ref) ([]byte, error) {
	if f.body != nil {
		return f.body, nil
	}
	return []byte(`{}`), nil
}

func TestFrozenRenderResolverBuildsEveryHoldoutAndRejectsMissingRoot(t *testing.T) {
	_, _, _, manifest := compiledTrainingDraftFixture(t)
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	maskDoc := map[string]any{"artifact": map[string]any{
		"artifact_id": uuid.New().String(), "sha256": strings.Repeat("f", 64)},
		"canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain),
		"object_id": nil, "selector": "manual", "confidence": 1.0,
		"model_lock_id": nil, "protected": true}
	maskBody, err := canonical.Marshal(maskDoc)
	if err != nil {
		t.Fatal(err)
	}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
	bodies := map[uuid.UUID][]byte{}
	holdout := 0
	for index := range manifest.Examples {
		item := &manifest.Examples[index]
		if item.Split != "holdout" {
			continue
		}
		holdout++
		item.RenderCase.Binding.CanvasHash = canvasSHA
		caseRef := item.RenderCase
		recipe := FrozenRecipe{SchemaVersion: 1, Source: caseRef.Binding,
			BaseStyle: style, Renderer: manifest.RendererVersion,
			ICCLockSHA: manifest.ICCLockSHA, ModelLockSHA: manifest.ModelLockSHA}
		recipeBody, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		bodies[caseRef.WorkingImage.ArtifactID] = images[0]
		bodies[caseRef.BaseRecipe.ArtifactID] = recipeBody
		bodies[caseRef.ApprovedRecipe.ArtifactID] = recipeBody
		bodies[caseRef.Protected.ArtifactID] = maskBody
		faceBody, err := canonical.Marshal(map[string]any{
			"schema_version": 1, "binding": caseRef.Binding,
			"faces": []any{}})
		if err != nil {
			t.Fatal(err)
		}
		bodies[caseRef.Faces.ArtifactID] = faceBody
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), TenantID: uuid.New(), RunID: uuid.New(),
		WorkflowID: uuid.New(), Candidate: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)}}
	resolver := FrozenLearningRenderResolver{Roots: frozenRootFixture{bodies},
		Images: frozenImageFixture{}, Masks: frozenMaskFixture{},
		Styles: frozenStyleFixture{}, Consents: frozenConsentFixture{}}
	resolved, err := resolver.Resolve(context.Background(), lease, manifest)
	if err != nil || len(resolved) != holdout {
		t.Fatalf("holdout closure incomplete: %v, %d/%d", err, len(resolved), holdout)
	}
	for index := range manifest.Examples {
		item := &manifest.Examples[index]
		if item.Split != "holdout" {
			continue
		}
		effective := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
		consent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
		item.RenderCase.EffectiveMask = &effective
		item.RenderCase.ConsentSnapshot = &consent
		maskDoc["protected"] = false
		body, marshalErr := canonical.Marshal(maskDoc)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		bodies[effective.ArtifactID] = body
		bodies[consent.ArtifactID] = []byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`)
		resolved, err = resolver.Resolve(context.Background(), lease, manifest)
		if err != nil || len(resolved) != holdout {
			t.Fatalf("effective mask closure incomplete: %v", err)
		}
		matched := false
		for _, input := range resolved {
			if input.EventID == item.EventID {
				matched = input.EffectiveMask != nil && *input.EffectiveMask == effective &&
					input.EffectivePixels != nil && input.ConsentSnapshot != nil
			}
		}
		if !matched {
			t.Fatal("effective mask refs dropped")
		}
		region := TrainingRegionMask{RegionID: "sky",
			Mask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
		for other := range manifest.Examples {
			if manifest.Examples[other].Split == "holdout" {
				manifest.Examples[other].RenderCase.RegionMasks = []TrainingRegionMask{region}
			}
		}
		regionDoc := map[string]any{}
		for key, value := range maskDoc {
			regionDoc[key] = value
		}
		regionDoc["selector"] = "sky"
		bodies[region.Mask.ArtifactID], err = canonical.Marshal(regionDoc)
		if err != nil {
			t.Fatal(err)
		}
		resolver.Styles = frozenStyleFixture{body: []byte(`{"extensions":{"region_adjustments":[{"region_id":"sky","selector":"sky"}]}}`)}
		resolved, err = resolver.Resolve(context.Background(), lease, manifest)
		if err != nil || len(resolved) != holdout {
			t.Fatalf("region mask closure incomplete: %v", err)
		}
		matched = false
		for _, input := range resolved {
			if input.EventID == item.EventID {
				matched = len(input.RegionMasks) == 1 &&
					input.RegionMasks[0].RegionID == "sky" &&
					input.RegionMasks[0].Mask == region.Mask
			}
		}
		if !matched {
			t.Fatal("region mask refs dropped")
		}
		break
	}
	for _, item := range manifest.Examples {
		if item.Split == "holdout" {
			delete(bodies, item.RenderCase.WorkingImage.ArtifactID)
			break
		}
	}
	if _, err := resolver.Resolve(context.Background(), lease, manifest); !errors.Is(err, ErrInvalidTrainingDraft) {
		t.Fatalf("missing frozen root accepted: %v", err)
	}
}
