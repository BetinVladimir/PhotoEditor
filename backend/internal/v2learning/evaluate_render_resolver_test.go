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
	"photoeditor/backend/internal/v2draft"
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

type frozenImageFixture struct{ cancel context.CancelFunc }

func (f frozenImageFixture) Resolve(_ context.Context, _ LearningEvaluateLease,
	_ v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) (FrozenEvaluationImage, error) {
	image, err := ParseFrozenEvaluationImage(raw, binding)
	if err != nil {
		return FrozenEvaluationImage{}, err
	}
	if f.cancel != nil {
		f.cancel()
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

type frozenStyleFixture struct {
	byRef  map[uuid.UUID][]byte
	body   []byte
	cancel context.CancelFunc
}

type frozenConsentFixture struct{}

func (frozenConsentFixture) Pin(_ context.Context,
	_ LearningEvaluateLease, _ []byte) error {
	return nil
}

func (f frozenStyleFixture) Load(_ context.Context, _ LearningEvaluateLease,
	ref v2input.Ref) ([]byte, error) {
	if f.cancel != nil {
		f.cancel()
	}
	if raw, ok := f.byRef[ref.ArtifactID]; ok {
		return raw, nil
	}
	if f.body != nil {
		return f.body, nil
	}
	return []byte(`{}`), nil
}

type frozenBackgroundStyleFixture struct {
	frozenStyleFixture
	verify func(context.Context, LearningEvaluateLease, v2draft.FrozenBackgroundAsset) error
}

func (f frozenBackgroundStyleFixture) VerifyBackground(ctx context.Context, lease LearningEvaluateLease, pin v2draft.FrozenBackgroundAsset) error {
	return f.verify(ctx, lease, pin)
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
			BaseStyle: v2input.Ref{ArtifactID: uuid.New(), SHA256: style.SHA256}, Renderer: manifest.RendererVersion,
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
	// Authorize the encoded background before admitting the rest of the case.
	for index := range manifest.Examples {
		item := &manifest.Examples[index]
		if item.Split != "holdout" {
			continue
		}
		var frozenRecipe FrozenRecipe
		if err := json.Unmarshal(bodies[item.RenderCase.BaseRecipe.ArtifactID], &frozenRecipe); err != nil {
			t.Fatal(err)
		}
		style := frozenRecipe.BaseStyle
		pin := v2draft.FrozenBackgroundAsset{SchemaVersion: 1, PortableAssetID: "installed", StyleVersionID: uuid.New(), Style: style, Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, ExpectedBytes: 10, MediaType: "image/png", Suffix: ".png", WorkICCSHA: manifest.ICCLockSHA}
		item.RenderCase.BackgroundAsset = &pin
		for _, mode := range []string{"valid", "missing_verifier", "denied", "cancelled"} {
			t.Run("background_"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				candidate := resolver
				calls := 0
				assetStyle := []byte(`{"extensions":{"background":{"mode":"asset","asset_id":"installed"}}}`)
				candidate.Styles = frozenStyleFixture{byRef: map[uuid.UUID][]byte{style.ArtifactID: assetStyle}}
				if mode != "missing_verifier" {
					candidate.Styles = frozenBackgroundStyleFixture{frozenStyleFixture: frozenStyleFixture{byRef: map[uuid.UUID][]byte{style.ArtifactID: assetStyle}}, verify: func(_ context.Context, gotLease LearningEvaluateLease, got v2draft.FrozenBackgroundAsset) error {
						calls++
						if gotLease != lease || got.Source != pin.Source || got.Style != style {
							t.Fatal("background authority scope changed")
						}
						if mode == "denied" {
							return ErrTrainingDraftConflict
						}
						if mode == "cancelled" {
							cancel()
						}
						return nil
					}}
				}
				got, err := candidate.Resolve(ctx, lease, manifest)
				if mode == "valid" {
					if err != nil || len(got) != holdout || calls != 1 {
						t.Fatalf("authorized background refused: %v calls=%d", err, calls)
					}
				} else {
					expected := ErrInvalidTrainingDraft
					if mode == "denied" {
						expected = ErrTrainingDraftConflict
					}
					if mode == "cancelled" {
						expected = context.Canceled
					}
					if got != nil || !errors.Is(err, expected) {
						t.Fatalf("unauthorized background yielded closure: %v %v", got, err)
					}
					if mode != "missing_verifier" && calls != 1 {
						t.Fatal("verifier was not invoked exactly once")
					}
				}
			})
		}
		item.RenderCase.BackgroundAsset = nil
		break
	}
	for _, phase := range []string{"image", "style"} {
		ctx, cancel := context.WithCancel(context.Background())
		candidate := resolver
		if phase == "image" {
			candidate.Images = frozenImageFixture{cancel: cancel}
		}
		if phase == "style" {
			candidate.Styles = frozenStyleFixture{cancel: cancel}
		}
		got, err := candidate.Resolve(ctx, lease, manifest)
		cancel()
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("successful-but-cancelled %s escaped: %v %v", phase, got, err)
		}
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

type reusedEvaluateRootBuffer struct {
	buffer []byte
	calls  int
	cancel context.CancelFunc
}

func (s *reusedEvaluateRootBuffer) Load(context.Context, LearningEvaluateLease,
	v2input.Ref, string) ([]byte, FrozenRootProvenance, error) {
	s.calls++
	for index := range s.buffer {
		s.buffer[index] = byte('a' + s.calls - 1)
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.buffer, FrozenRootProvenance{}, nil
}

func TestEvaluateRootBytesAreOwnedAcrossReaderReuse(t *testing.T) {
	reader := &reusedEvaluateRootBuffer{buffer: make([]byte, 4)}
	resolver := FrozenLearningRenderResolver{Roots: reader}
	first, _, err := resolver.loadRoot(context.Background(), LearningEvaluateLease{}, v2input.Ref{}, "FrozenRecipe")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := resolver.loadRoot(context.Background(), LearningEvaluateLease{}, v2input.Ref{}, "FrozenRecipe")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "aaaa" || string(second) != "bbbb" {
		t.Fatal("reader reuse changed frozen recipe bytes")
	}
	reader.buffer[0] = 'z'
	if string(first) != "aaaa" || string(second) != "bbbb" {
		t.Fatal("returned root retained borrowed backing storage")
	}
}

func TestEvaluateRootReadCancellationRefusesBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &reusedEvaluateRootBuffer{buffer: make([]byte, 4), cancel: cancel}
	resolver := FrozenLearningRenderResolver{Roots: reader}
	for attempt := 0; attempt < 2; attempt++ {
		body, _, err := resolver.loadRoot(ctx, LearningEvaluateLease{}, v2input.Ref{}, "WorkingImage")
		if body != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled bytes escaped: %q %v", body, err)
		}
	}
	if reader.calls != 1 {
		t.Fatal("pre-cancelled read performed IO")
	}
}

func TestEvaluationManifestOwnsNestedAuthorityBeforeIO(t *testing.T) {
	_, _, _, manifest := compiledTrainingDraftFixture(t)
	mask := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	consent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	manifest.Examples[0].RenderCase.EffectiveMask = &mask
	manifest.Examples[0].RenderCase.ConsentSnapshot = &consent
	manifest.Examples[0].RenderCase.RegionMasks = []TrainingRegionMask{{RegionID: "sky", Mask: mask}}
	owned, err := ownEvaluationManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	before, err := canonical.Marshal(owned)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Consents[0].Epoch = "2"
	manifest.Examples[0].Features[0] = 99
	manifest.Examples[0].RenderCase.Binding.CanvasHash = strings.Repeat("f", 64)
	manifest.Examples[0].RenderCase.RegionMasks[0].RegionID = "changed"
	mask.ArtifactID = uuid.Nil
	consent.ArtifactID = uuid.Nil
	after, err := canonical.Marshal(owned)
	if err != nil || string(before) != string(after) {
		t.Fatal("borrowed manifest authority changed after caller mutation")
	}
	if _, err := ownEvaluationManifest(manifest); err == nil {
		t.Fatal("accepted malformed source manifest")
	}
}
